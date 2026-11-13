package main

// Minimal Discord RPC client over the local IPC socket (same protocol curd uses through go-discordrpc)

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

type discordClient struct {
	clientID string
	conn     net.Conn
}

func discordSocketDirs() []string {
	uid := fmt.Sprint(os.Getuid())
	dirs := []string{}
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = "/run/user/" + uid
	}
	for _, sub := range []string{"", "app/com.discordapp.Discord", ".flatpak/com.discordapp.Discord/xdg-run",
		".flatpak/dev.vencord.Vesktop/xdg-run", "app/dev.vencord.Vesktop", "snap.discord"} {
		dirs = append(dirs, filepath.Join(base, sub))
	}
	for _, v := range []string{"TMPDIR", "TMP", "TEMP"} {
		if d := os.Getenv(v); d != "" {
			dirs = append(dirs, d)
		}
	}
	return append(dirs, "/tmp")
}

func (d *discordClient) connect() error {
	if d.conn != nil {
		return nil
	}
	for _, dir := range discordSocketDirs() {
		for i := 0; i < 10; i++ {
			c, err := net.DialTimeout("unix", filepath.Join(dir, fmt.Sprintf("discord-ipc-%d", i)), time.Second)
			if err != nil {
				continue
			}
			d.conn = c
			if _, err := d.send(0, map[string]string{"v": "1", "client_id": d.clientID}); err != nil {
				d.close()
				continue
			}
			return nil
		}
	}
	return fmt.Errorf("Discord IPC socket not found (is Discord running?)")
}

func (d *discordClient) send(op uint32, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(buf[0:], op)
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(body)))
	copy(buf[8:], body)
	d.conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := d.conn.Write(buf); err != nil {
		return nil, err
	}
	hdr := make([]byte, 8)
	if _, err := io.ReadFull(d.conn, hdr); err != nil {
		return nil, err
	}
	resp := make([]byte, binary.LittleEndian.Uint32(hdr[4:]))
	if _, err := io.ReadFull(d.conn, resp); err != nil {
		return nil, err
	}
	if binary.LittleEndian.Uint32(hdr[0:]) == 2 { // CLOSE
		return resp, fmt.Errorf("discord closed connection: %s", resp)
	}
	return resp, nil
}

func nonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func (d *discordClient) setActivity(activity map[string]any) error {
	if err := d.connect(); err != nil {
		return err
	}
	resp, err := d.send(1, map[string]any{
		"cmd":   "SET_ACTIVITY",
		"args":  map[string]any{"pid": os.Getpid(), "activity": activity},
		"nonce": nonce(),
	})
	if err != nil {
		d.close() // reconnect on next update
		return err
	}
	var r struct {
		Evt  string `json:"evt"`
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if json.Unmarshal(resp, &r) == nil && r.Evt == "ERROR" {
		return fmt.Errorf("discord rejected activity: %s", r.Data.Message)
	}
	return nil
}

func (d *discordClient) close() {
	if d.conn != nil {
		d.conn.Close()
		d.conn = nil
	}
}
