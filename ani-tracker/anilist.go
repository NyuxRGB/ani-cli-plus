package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const anilistAPI = "https://graphql.anilist.co"

// Same public OAuth client curd uses (implicit grant, token is shown in the redirect URL)
const anilistLoginURL = "https://anilist.co/api/v2/oauth/authorize?client_id=20686&response_type=token&redirect_uri=http://localhost:8000/oauth/callback"

type media struct {
	ID         int `json:"id"`
	IDMal      int `json:"idMal"`
	Episodes   int `json:"episodes"`
	Title      struct{ Romaji, English string }
	CoverImage struct{ Large string }
	ListEntry  *struct {
		Progress int    `json:"progress"`
		Status   string `json:"status"`
	} `json:"mediaListEntry"`
}

func (m *media) title(pref string) string {
	if pref == "english" && m.Title.English != "" {
		return m.Title.English
	}
	if m.Title.Romaji != "" {
		return m.Title.Romaji
	}
	return m.Title.English
}

func anilistQuery(token, query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, _ := http.NewRequest("POST", anilistAPI, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	var resp *http.Response
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err = client.Do(req.Clone(req.Context()))
		if err == nil && resp.StatusCode != http.StatusTooManyRequests {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		req.Body, _ = req.GetBody()
		time.Sleep(time.Duration(attempt+1) * 3 * time.Second)
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var wrapped struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapped); err != nil {
		return fmt.Errorf("anilist: HTTP %d: %w", resp.StatusCode, err)
	}
	if len(wrapped.Errors) > 0 {
		return fmt.Errorf("anilist: %s", wrapped.Errors[0].Message)
	}
	return json.Unmarshal(wrapped.Data, out)
}

func fetchMediaByMal(token string, malID int) (*media, error) {
	listField := ""
	if token != "" {
		listField = "mediaListEntry { progress status }"
	}
	q := `query ($idMal: Int) { Media(idMal: $idMal, type: ANIME) {
		id idMal episodes title { romaji english } coverImage { large } ` + listField + ` } }`
	var out struct{ Media media }
	if err := anilistQuery(token, q, map[string]any{"idMal": malID}, &out); err != nil {
		return nil, err
	}
	return &out.Media, nil
}

func updateProgress(token string, m *media, ep int) (string, error) {
	status := "CURRENT"
	if m.ListEntry != nil {
		switch m.ListEntry.Status {
		case "COMPLETED":
			return "already completed on AniList, not touched", nil
		case "REPEATING":
			status = "REPEATING"
		}
		if ep <= m.ListEntry.Progress && (m.ListEntry.Status == "CURRENT" || m.ListEntry.Status == "REPEATING") {
			return fmt.Sprintf("progress already at %d", m.ListEntry.Progress), nil
		}
	}
	if m.Episodes > 0 && ep >= m.Episodes {
		status = "COMPLETED"
		ep = m.Episodes
	}
	q := `mutation ($mediaId: Int, $progress: Int, $status: MediaListStatus) {
		SaveMediaListEntry(mediaId: $mediaId, progress: $progress, status: $status) { progress status } }`
	var out struct {
		SaveMediaListEntry struct {
			Progress int
			Status   string
		}
	}
	if err := anilistQuery(token, q, map[string]any{"mediaId": m.ID, "progress": ep, "status": status}, &out); err != nil {
		return "", err
	}
	return fmt.Sprintf("progress %d (%s)", out.SaveMediaListEntry.Progress, out.SaveMediaListEntry.Status), nil
}

func dataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

func ownTokenPath() string { return filepath.Join(dataDir(), "ani-cli", "anilist_token") }

// loadToken looks in $ANI_CLI_ANILIST_TOKEN, then ani-tracker's own file, then curd's token file so an existing curd login is reused.
func loadToken() string {
	if t := strings.TrimSpace(os.Getenv("ANI_CLI_ANILIST_TOKEN")); t != "" {
		return t
	}
	if b, err := os.ReadFile(ownTokenPath()); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t
		}
	}
	if b, err := os.ReadFile(filepath.Join(dataDir(), "curd", "anilist_token.json")); err == nil {
		var tok struct {
			AccessToken string    `json:"access_token"`
			ExpiresAt   time.Time `json:"expires_at"`
		}
		if json.Unmarshal(b, &tok) == nil {
			if tok.AccessToken != "" && (tok.ExpiresAt.IsZero() || time.Now().Before(tok.ExpiresAt)) {
				return tok.AccessToken
			}
		} else if t := strings.TrimSpace(string(b)); t != "" {
			return t
		}
	}
	return ""
}

func viewerName(token string) (string, error) {
	var out struct{ Viewer struct{ Name string } }
	err := anilistQuery(token, `query { Viewer { name } }`, nil, &out)
	return out.Viewer.Name, err
}

func login() error {
	fmt.Println("1. Open this URL and authorize the app:")
	fmt.Println("   " + anilistLoginURL)
	fmt.Println("2. You will land on a localhost page that fails to load; copy the")
	fmt.Println("   access_token=... value from the address bar (or the whole URL).")
	fmt.Print("Token: ")
	var input string
	fmt.Scanln(&input)
	token := strings.TrimSpace(input)
	if i := strings.Index(token, "access_token="); i >= 0 {
		token = token[i+len("access_token="):]
		if j := strings.IndexByte(token, '&'); j >= 0 {
			token = token[:j]
		}
	}
	if token == "" {
		return fmt.Errorf("no token given")
	}
	name, err := viewerName(token)
	if err != nil {
		return fmt.Errorf("token rejected: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(ownTokenPath()), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(ownTokenPath(), []byte(token), 0o600); err != nil {
		return err
	}
	fmt.Printf("Logged in as %s, token saved to %s\n", name, ownTokenPath())
	return nil
}
