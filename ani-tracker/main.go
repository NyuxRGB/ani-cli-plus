package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const defaultDiscordClientID = "1287457464148820089"

func notify(msg string) {
	log.Print(msg)
	if os.Getenv("ANI_CLI_NOTIFY") == "0" {
		return
	}
	if p, err := exec.LookPath("notify-send"); err == nil {
		exec.Command(p, "-a", "ani-cli", "ani-cli", msg).Run()
	}
}

func setupLog() {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	dir = filepath.Join(dir, "ani-cli")
	os.MkdirAll(dir, 0o755)
	if f, err := os.OpenFile(filepath.Join(dir, "tracker.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(f)
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "login" {
		if err := login(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "whoami" {
		token := loadToken()
		if token == "" {
			fmt.Println("not logged in (run: ani-tracker login)")
			os.Exit(1)
		}
		name, err := viewerName(token)
		if err != nil {
			fmt.Println("token invalid:", err)
			os.Exit(1)
		}
		fmt.Println("AniList user:", name)
		return
	}

	socket := flag.String("socket", "", "mpv IPC socket path")
	malID := flag.Int("mal-id", 0, "MyAnimeList id of the anime")
	epStr := flag.String("episode", "", "episode number")
	fallbackTitle := flag.String("title", "", "title shown when AniList has no match")
	noDiscord := flag.Bool("no-discord", os.Getenv("ANI_CLI_DISCORD") == "0", "disable Discord Rich Presence")
	noAnilist := flag.Bool("no-anilist", os.Getenv("ANI_CLI_ANILIST") == "0", "disable AniList sync")
	threshold := flag.Float64("threshold", envFloat("ANI_CLI_ANILIST_THRESHOLD", 85), "percent watched before AniList is updated")
	clientID := flag.String("discord-client-id", envStr("ANI_CLI_DISCORD_CLIENT_ID", defaultDiscordClientID), "Discord application id")
	titleLang := flag.String("title-lang", envStr("ANI_CLI_TITLE_LANG", "romaji"), "romaji or english")
	flag.Parse()
	setupLog()
	if *socket == "" {
		log.Fatal("--socket is required")
	}

	ep, epErr := strconv.ParseFloat(*epStr, 64)
	token := ""
	if !*noAnilist {
		token = loadToken()
		if token == "" {
			log.Print("AniList sync off: no token (run `ani-tracker login`)")
			*noAnilist = true
		}
	}

	var m *media
	if *malID > 0 {
		var err error
		if m, err = fetchMediaByMal(token, *malID); err != nil {
			log.Printf("AniList lookup for MAL %d failed: %v", *malID, err)
			m = nil
		}
	}
	title := *fallbackTitle
	if m != nil {
		title = m.title(*titleLang)
	}

	mpv, err := dialMpv(*socket, 60*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	defer os.Remove(*socket)

	var dc *discordClient
	if !*noDiscord {
		dc = &discordClient{clientID: *clientID}
		defer dc.close()
	}

	synced := *noAnilist || m == nil || epErr != nil || ep != math.Trunc(ep) || ep < 1
	if m == nil && !*noAnilist {
		log.Printf("AniList sync skipped for %q ep %s: no AniList entry for MAL id %d", title, *epStr, *malID)
	}

	var lastPaused bool
	var lastPush, lastExpectedEnd time.Time
	first := true
	for {
		pos, _, err := mpv.getFloat("time-pos")
		if err != nil {
			break // mpv closed
		}
		dur, durOK, _ := mpv.getFloat("duration")
		paused, _, err := mpv.getBool("pause")
		if err != nil {
			break
		}

		if !synced && durOK && dur > 60 && pos/dur*100 >= *threshold {
			synced = true
			pending.Add(1)
			go func(ep int) {
				defer pending.Done()
				res, err := updateProgress(token, m, ep)
				if err != nil {
					notify(fmt.Sprintf("AniList update failed for %s ep %d: %v", title, ep, err))
					return
				}
				notify(fmt.Sprintf("AniList: %s — %s", title, res))
			}(int(ep))
		}

		if dc != nil {
			now := time.Now()
			expectedEnd := now.Add(time.Duration((dur - pos) * float64(time.Second)))
			seeked := !lastExpectedEnd.IsZero() && !paused && absDur(expectedEnd.Sub(lastExpectedEnd)) > 5*time.Second
			if first || paused != lastPaused || seeked || now.Sub(lastPush) > 2*time.Minute {
				if err := dc.setActivity(activity(title, *epStr, m, *malID, pos, dur, durOK, paused)); err != nil {
					log.Printf("discord: %v", err)
				} else {
					if first {
						log.Printf("discord: presence set for %q episode %s", title, *epStr)
					}
					first = false
				}
				lastPaused, lastPush, lastExpectedEnd = paused, now, expectedEnd
			} else if !paused {
				lastExpectedEnd = expectedEnd
			}
		}
		time.Sleep(time.Second)
	}

	done := make(chan struct{})
	go func() { pending.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
	}
}

var pending sync.WaitGroup

func activity(title, ep string, m *media, malID int, pos, dur float64, durOK, paused bool) map[string]any {
	now := time.Now()
	start := now.Add(-time.Duration(pos * float64(time.Second)))
	ts := map[string]any{"start": start.UnixMilli()}
	small := map[string]any{}
	if paused {
		small["small_image"] = "pause-button"
		small["small_text"] = "Paused"
	} else if durOK && dur > 60 && dur > pos {
		ts["end"] = now.Add(time.Duration((dur - pos) * float64(time.Second))).UnixMilli()
	}
	assets := map[string]any{"large_image": "https://anilist.co/img/icons/icon.svg", "large_text": title}
	for k, v := range small {
		assets[k] = v
	}
	var buttons []map[string]string
	if m != nil {
		if m.CoverImage.Large != "" {
			assets["large_image"] = m.CoverImage.Large
		}
		buttons = append(buttons, map[string]string{"label": "View on AniList", "url": fmt.Sprintf("https://anilist.co/anime/%d", m.ID)})
	}
	if malID > 0 {
		buttons = append(buttons, map[string]string{"label": "View on MAL", "url": fmt.Sprintf("https://myanimelist.net/anime/%d", malID)})
	}
	act := map[string]any{
		"type":       3, // Watching
		"name":       title,
		"details":    title,
		"state":      "Episode " + ep,
		"assets":     assets,
		"timestamps": ts,
	}
	if len(buttons) > 0 {
		act["buttons"] = buttons
	}
	return act
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func envStr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envFloat(k string, def float64) float64 {
	if f, err := strconv.ParseFloat(os.Getenv(k), 64); err == nil {
		return f
	}
	return def
}
