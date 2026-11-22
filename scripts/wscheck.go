//go:build ignore

// Run go run ./scripts/wscheck.go against the local development API.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	gorillaws "github.com/gorilla/websocket"
)

const base = "http://localhost:8081"

var client = http.Client{Timeout: 5 * time.Second}

type message struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

func request(method, path, token string, body, result any) error {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, base+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(result)
}

func check() error {
	var session struct {
		Token string `json:"token"`
	}
	err := request(http.MethodPost, "/v1/auth/signup", "", map[string]string{
		"email": rand.Text() + "@example.com", "password": rand.Text(), "display_name": "WS check", "tenant_name": "Local demo",
	}, &session)
	if err != nil {
		return err
	}
	var channel struct {
		ID string `json:"id"`
	}
	if err := request(http.MethodPost, "/v1/channels", session.Token, map[string]string{"name": "demo"}, &channel); err != nil {
		return err
	}

	conn, _, err := gorillaws.DefaultDialer.Dial("ws://localhost:8081/v1/ws", http.Header{"Authorization": {"Bearer " + session.Token}})
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"type": "subscribe", "channel_id": channel.ID}); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var ack struct {
		Type    string
		Channel string `json:"channel_id"`
	}
	if err := conn.ReadJSON(&ack); err != nil {
		return err
	}
	if ack.Type != "subscribed" || ack.Channel != channel.ID {
		return fmt.Errorf("channel subscription was not acknowledged")
	}

	path := "/v1/channels/" + channel.ID + "/messages"
	var saved message
	if err := request(http.MethodPost, path, session.Token, map[string]string{"content": "Hello from REST"}, &saved); err != nil {
		return err
	}
	if saved.ID <= 0 {
		return fmt.Errorf("message ID missing")
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var event struct {
			Type    string
			Message message
		}
		if err := conn.ReadJSON(&event); err != nil {
			return err
		}
		if event.Type == "message" {
			if event.Message != saved {
				return fmt.Errorf("live message differed from stored response")
			}
			break
		}
	}
	var history []message
	if err := request(http.MethodGet, path, session.Token, nil, &history); err != nil {
		return err
	}
	if len(history) != 1 || history[0] != saved {
		return fmt.Errorf("unexpected channel history")
	}
	fmt.Printf("Message %d matched the send response, live event and channel history.\n", saved.ID)
	return nil
}
func main() {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
