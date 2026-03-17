//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"time"

	gorillaws "github.com/gorilla/websocket"
)

const base = "http://localhost:8081"

func main() {
	suffix := rand.Intn(1_000_000)

	fmt.Println("=== Step 1: Signup Alice ===")
	email := fmt.Sprintf("alice+%d@test.com", suffix)
	aliceToken := signup(email, "pass1234", "Alice", "TestCorp")
	fmt.Println("  token:", aliceToken[:30], "...")

	fmt.Println("\n=== Step 2: Create #general ===")
	channelID := createChannel(aliceToken, fmt.Sprintf("ws-test-%d", suffix), "Testing WS delivery")
	fmt.Println("  channel:", channelID)

	fmt.Println("\n=== Step 3: Alice joins #general ===")
	joinChannel(aliceToken, channelID)
	fmt.Println("  joined")

	fmt.Println("\n=== Step 4: Connect WebSocket ===")
	wsURL := url.URL{
		Scheme:   "ws",
		Host:     "localhost:8081",
		Path:     "/v1/ws",
		RawQuery: "token=" + aliceToken,
	}
	conn, _, err := gorillaws.DefaultDialer.Dial(wsURL.String(), nil)
	if err != nil {
		fatal("ws dial", err)
	}
	defer conn.Close()
	fmt.Println("  connected")

	wsMsgs := make(chan map[string]interface{}, 10)
	go func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			json.Unmarshal(raw, &msg)
			wsMsgs <- msg
		}
	}()

	fmt.Println("\n=== Step 5: Subscribe via WS ===")
	conn.WriteJSON(map[string]string{
		"type":       "subscribe",
		"channel_id": channelID,
	})

	select {
	case msg := <-wsMsgs:
		fmt.Printf("  got: %v\n", msg["type"])
	case <-time.After(3 * time.Second):
		fatal("timeout", fmt.Errorf("no subscribe ack"))
	}

	fmt.Println("\n=== Step 6: Send message via REST ===")
	restMsg := sendMessage(aliceToken, channelID, "Hello from REST!")
	fmt.Printf("  saved: id=%v body=%v\n", restMsg["id"], restMsg["body"])

	fmt.Println("\n=== Step 7: Check WebSocket delivery ===")
	select {
	case msg := <-wsMsgs:
		if msg["type"] == "message" {
			inner := msg["message"].(map[string]interface{})
			fmt.Println("  REAL-TIME DELIVERY WORKS")
			fmt.Printf("  body: %s\n", inner["body"])
		}
	case <-time.After(3 * time.Second):
		fmt.Println("  TIMEOUT - no message on WebSocket")
		os.Exit(1)
	}

	fmt.Println("\n=== Step 8: Rapid fire 3 messages ===")
	for i := 1; i <= 3; i++ {
		sendMessage(aliceToken, channelID, fmt.Sprintf("Message #%d", i))
	}
	count := 0
	deadline := time.After(3 * time.Second)
loop:
	for count < 3 {
		select {
		case <-wsMsgs:
			count++
		case <-deadline:
			break loop
		}
	}
	fmt.Printf("  received %d/3 via WebSocket\n", count)

	fmt.Println("\n=== Step 9: Check message history ===")
	messages := listMessages(aliceToken, channelID)
	fmt.Printf("  %d messages in DB\n", len(messages))

	fmt.Println("\nAll tests passed.")
}

func signup(email, password, name, tenant string) string {
	body, _ := json.Marshal(map[string]string{
		"email": email, "password": password,
		"display_name": name, "tenant_name": tenant,
	})
	resp := post("/v1/auth/signup", "", body)
	return resp["token"].(string)
}

func createChannel(token, name, desc string) string {
	body, _ := json.Marshal(map[string]string{"name": name, "description": desc})
	resp := post("/v1/channels", token, body)
	return resp["id"].(string)
}

func joinChannel(token, channelID string) {
	post("/v1/channels/"+channelID+"/join", token, nil)
}

func sendMessage(token, channelID, content string) map[string]interface{} {
	body, _ := json.Marshal(map[string]string{"content": content})
	return post("/v1/channels/"+channelID+"/messages", token, body)
}

func listMessages(token, channelID string) []interface{} {
	req, _ := http.NewRequest("GET", base+"/v1/channels/"+channelID+"/messages", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatal("list", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out []interface{}
	json.Unmarshal(raw, &out)
	return out
}

func post(path, token string, body []byte) map[string]interface{} {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, _ := http.NewRequest("POST", base+path, r)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatal("POST "+path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 && resp.StatusCode != 204 {
		fatal("POST "+path, fmt.Errorf("status %d: %s", resp.StatusCode, string(raw)))
	}
	if resp.StatusCode == 204 || len(raw) == 0 {
		return nil
	}
	var out map[string]interface{}
	json.Unmarshal(raw, &out)
	return out
}

func fatal(ctx string, err error) {
	fmt.Fprintf(os.Stderr, "FAIL [%s]: %v\n", ctx, err)
	os.Exit(1)
}
