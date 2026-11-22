package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	gorillaws "github.com/gorilla/websocket"
	"github.com/lalith-99/echostream/internal/auth"
	ws "github.com/lalith-99/echostream/internal/websocket"
	"go.uber.org/zap"
)

type wsMembership struct {
	mockMembershipRepoFull
	allowed atomic.Bool
}

func (m *wsMembership) IsMember(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (bool, error) {
	return m.allowed.Load(), nil
}

func TestWebSocketSessionAuthorization(t *testing.T) {
	hub := ws.NewHub(zap.NewNop())
	go hub.Run()
	t.Cleanup(hub.Shutdown)
	repo := &wsMembership{}
	repo.allowed.Store(true)
	h := NewWSHandler(hub, repo.IsMember, "test-secret", zap.NewNop(), "https://chat.example")
	engine := gin.New()
	engine.GET("/v1/ws", h.HandleWS)
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/ws"
	user := uuid.New()
	token, err := auth.GenerateToken(user, uuid.New(), "a@example.com", "test-secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	browser := gorillaws.Dialer{Subprotocols: []string{wsProtocol, "bearer." + token}}
	for _, tc := range []struct {
		dialer *gorillaws.Dialer
		url    string
		origin string
		want   int
	}{
		{&browser, url, "https://other.example", http.StatusForbidden},
		{gorillaws.DefaultDialer, url, "https://chat.example", http.StatusUnauthorized},
	} {
		conn, resp, err := tc.dialer.Dial(tc.url, http.Header{"Origin": {tc.origin}})
		if err == nil {
			conn.Close()
			t.Fatalf("expected HTTP %d", tc.want)
		}
		if resp == nil || resp.StatusCode != tc.want {
			t.Fatalf("expected HTTP %d, response %v", tc.want, resp)
		}
		resp.Body.Close()
	}
	conn, resp, err := browser.Dial(url, http.Header{"Origin": {"https://chat.example"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if conn.Subprotocol() != wsProtocol || strings.Contains(resp.Header.Get("Sec-WebSocket-Protocol"), token) {
		t.Fatal("wrong negotiated protocol")
	}
	id := uuid.New()
	if err := conn.WriteJSON(ws.InboundMessage{Type: "subscribe", ChannelID: id.String()}); err != nil {
		t.Fatal(err)
	}
	readEvent := func(want string) {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var ev ws.OutboundEvent
		if err := conn.ReadJSON(&ev); err != nil || ev.Type != want {
			t.Fatalf("want %s, got %s err=%v", want, ev.Type, err)
		}
	}
	readEvent("subscribed")
	repo.allowed.Store(false)
	hub.Broadcast(id, []byte(`{"type":"message"}`))
	readEvent("unsubscribed")

	// The admission limit also applies across different sockets for a single user.
	for i := 1; i < maxSocketsPerUser; i++ {
		extra, _, err := gorillaws.DefaultDialer.Dial(url, http.Header{"Authorization": {"Bearer " + token}})
		if err != nil {
			t.Fatal(err)
		}
		defer extra.Close()
	}
	extra, resp, err := gorillaws.DefaultDialer.Dial(url, http.Header{"Authorization": {"Bearer " + token}})
	if err == nil {
		extra.Close()
		t.Fatal("connection limit not enforced")
	}
	if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("limit response %v", resp)
	}
	_ = resp.Body.Close()
}

func TestWebSocketExpiresWhileIdle(t *testing.T) {
	hub := ws.NewHub(zap.NewNop())
	go hub.Run()
	defer hub.Shutdown()
	engine := gin.New()
	h := NewWSHandler(hub, (&wsMembership{}).IsMember, "test-secret", zap.NewNop())
	engine.GET("/v1/ws", h.HandleWS)
	server := httptest.NewServer(engine)
	defer server.Close()
	token, err := auth.GenerateToken(uuid.New(), uuid.New(), "test@example.com", "test-secret", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := gorillaws.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/ws", http.Header{"Authorization": {"Bearer " + token}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	_, _, err = conn.ReadMessage()
	if !gorillaws.IsCloseError(err, gorillaws.ClosePolicyViolation) {
		t.Fatalf("expected expiry close, got %v", err)
	}
}
