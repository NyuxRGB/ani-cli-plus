package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

type mpvClient struct {
	conn   net.Conn
	reader *bufio.Reader
	reqID  int
}

func dialMpv(path string, wait time.Duration) (*mpvClient, error) {
	deadline := time.Now().Add(wait)
	for {
		c, err := net.Dial("unix", path)
		if err == nil {
			return &mpvClient{conn: c, reader: bufio.NewReader(c)}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("mpv socket %s not available: %w", path, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (m *mpvClient) get(prop string) (val any, ok bool, err error) {
	m.reqID++
	id := m.reqID
	req, _ := json.Marshal(map[string]any{"command": []any{"get_property", prop}, "request_id": id})
	m.conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := m.conn.Write(append(req, '\n')); err != nil {
		return nil, false, err
	}
	for {
		line, err := m.reader.ReadBytes('\n')
		if err != nil {
			return nil, false, err
		}
		var resp struct {
			Error     string `json:"error"`
			Data      any    `json:"data"`
			RequestID *int   `json:"request_id"`
			Event     string `json:"event"`
		}
		if json.Unmarshal(line, &resp) != nil || resp.Event != "" || resp.RequestID == nil || *resp.RequestID != id {
			continue // skip events and unrelated replies
		}
		return resp.Data, resp.Error == "success", nil
	}
}

func (m *mpvClient) getFloat(prop string) (float64, bool, error) {
	v, ok, err := m.get(prop)
	f, isNum := v.(float64)
	return f, ok && isNum, err
}

func (m *mpvClient) getBool(prop string) (bool, bool, error) {
	v, ok, err := m.get(prop)
	b, isBool := v.(bool)
	return b, ok && isBool, err
}
