package websocket

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// fakeClient creates a Client with a send channel but nil conn.
// Good enough for Hub tests — we never call ReadPump/WritePump.
func fakeClient(hub *Hub, userID uuid.UUID) *Client {
	return &Client{
		hub:      hub,
		conn:     nil,
		send:     make(chan []byte, sendBufSize),
		userID:   userID,
		tenantID: uuid.New(),
		logger:   zap.NewNop(),
	}
}

// drainOne reads exactly one message from c.send with a timeout.
func drainOne(t *testing.T, c *Client) OutboundEvent {
	t.Helper()
	select {
	case data := <-c.send:
		var ev OutboundEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return ev
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
		return OutboundEvent{}
	}
}

func startHub(t *testing.T) *Hub {
	t.Helper()
	hub := NewHub(zap.NewNop())
	go hub.Run()
	return hub
}

func TestRegisterAndUnregister(t *testing.T) {
	hub := startHub(t)
	c := fakeClient(hub, uuid.New())

	hub.register <- c
	time.Sleep(50 * time.Millisecond)

	hub.unregister <- c
	time.Sleep(50 * time.Millisecond)

	// send channel should be closed after unregister
	_, open := <-c.send
	if open {
		t.Fatal("expected send channel to be closed")
	}
}

func TestSubscribeAndBroadcast(t *testing.T) {
	hub := startHub(t)
	c := fakeClient(hub, uuid.New())
	chID := uuid.New()

	hub.register <- c
	time.Sleep(50 * time.Millisecond)

	hub.subscribeCh <- &subscription{client: c, channelID: chID}
	time.Sleep(50 * time.Millisecond)

	// Should receive "subscribed" ack
	ev := drainOne(t, c)
	if ev.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %s", ev.Type)
	}

	// Broadcast a message
	payload, _ := json.Marshal(OutboundEvent{Type: "message", ChannelID: chID.String()})
	hub.Broadcast(chID, payload)
	time.Sleep(50 * time.Millisecond)

	ev = drainOne(t, c)
	if ev.Type != "message" {
		t.Fatalf("expected message, got %s", ev.Type)
	}
}

func TestBroadcastToEmptyChannel(t *testing.T) {
	hub := startHub(t)

	// Broadcasting to a channel with no subscribers should not panic.
	payload, _ := json.Marshal(OutboundEvent{Type: "message"})
	hub.Broadcast(uuid.New(), payload)
	time.Sleep(50 * time.Millisecond)
}

func TestUnsubscribe(t *testing.T) {
	hub := startHub(t)
	c := fakeClient(hub, uuid.New())
	chID := uuid.New()

	hub.register <- c
	time.Sleep(50 * time.Millisecond)
	hub.subscribeCh <- &subscription{client: c, channelID: chID}
	time.Sleep(50 * time.Millisecond)
	_ = drainOne(t, c) // consume "subscribed" ack

	hub.unsubscribeCh <- &subscription{client: c, channelID: chID}
	time.Sleep(50 * time.Millisecond)
	ev := drainOne(t, c) // consume "unsubscribed" ack
	if ev.Type != "unsubscribed" {
		t.Fatalf("expected unsubscribed, got %s", ev.Type)
	}

	// After unsubscribe, broadcast should NOT reach this client
	payload, _ := json.Marshal(OutboundEvent{Type: "message"})
	hub.Broadcast(chID, payload)
	time.Sleep(50 * time.Millisecond)

	select {
	case <-c.send:
		t.Fatal("should not receive message after unsubscribe")
	default:
		// good — nothing in send buffer
	}
}

func TestChannelCallbacks(t *testing.T) {
	hub := startHub(t)

	var activatedCh, deactivatedCh uuid.UUID
	hub.SetChannelCallbacks(
		func(id uuid.UUID) { activatedCh = id },
		func(id uuid.UUID) { deactivatedCh = id },
	)

	c := fakeClient(hub, uuid.New())
	chID := uuid.New()

	hub.register <- c
	time.Sleep(50 * time.Millisecond)

	// First subscriber triggers onChannelActive
	hub.subscribeCh <- &subscription{client: c, channelID: chID}
	time.Sleep(50 * time.Millisecond)
	_ = drainOne(t, c) // ack

	if activatedCh != chID {
		t.Fatalf("onChannelActive not called, got %s", activatedCh)
	}

	// Unregister last subscriber triggers onChannelInactive
	hub.unregister <- c
	time.Sleep(50 * time.Millisecond)

	if deactivatedCh != chID {
		t.Fatalf("onChannelInactive not called, got %s", deactivatedCh)
	}
}

func TestTypingExcludesSender(t *testing.T) {
	hub := startHub(t)
	sender := fakeClient(hub, uuid.New())
	receiver := fakeClient(hub, uuid.New())
	chID := uuid.New()

	hub.register <- sender
	hub.register <- receiver
	time.Sleep(50 * time.Millisecond)

	hub.subscribeCh <- &subscription{client: sender, channelID: chID}
	hub.subscribeCh <- &subscription{client: receiver, channelID: chID}
	time.Sleep(50 * time.Millisecond)
	_ = drainOne(t, sender)   // ack
	_ = drainOne(t, receiver) // ack

	// Send typing event
	hub.typingCh <- &typingEvent{channelID: chID, userID: sender.userID}
	time.Sleep(50 * time.Millisecond)

	// Receiver should get typing notification
	ev := drainOne(t, receiver)
	if ev.Type != "typing" {
		t.Fatalf("expected typing, got %s", ev.Type)
	}

	// Sender should NOT get their own typing event
	select {
	case <-sender.send:
		t.Fatal("sender should not receive own typing event")
	default:
	}
}

func TestDisconnectCleansUpAllChannels(t *testing.T) {
	hub := startHub(t)

	var inactiveCalls int
	hub.SetChannelCallbacks(
		func(uuid.UUID) {},
		func(uuid.UUID) { inactiveCalls++ },
	)

	c := fakeClient(hub, uuid.New())
	ch1, ch2 := uuid.New(), uuid.New()

	hub.register <- c
	time.Sleep(50 * time.Millisecond)
	hub.subscribeCh <- &subscription{client: c, channelID: ch1}
	hub.subscribeCh <- &subscription{client: c, channelID: ch2}
	time.Sleep(50 * time.Millisecond)
	_ = drainOne(t, c) // ack ch1
	_ = drainOne(t, c) // ack ch2

	// Disconnect should clean up both channels
	hub.unregister <- c
	time.Sleep(50 * time.Millisecond)

	if inactiveCalls != 2 {
		t.Fatalf("expected 2 onChannelInactive calls, got %d", inactiveCalls)
	}
}

func TestClient_Send_NonBlocking(t *testing.T) {
	hub := NewHub(zap.NewNop())
	c := fakeClient(hub, uuid.New())

	// Fill the send buffer
	for i := 0; i < sendBufSize; i++ {
		c.Send([]byte("x"))
	}

	// One more Send should NOT block (dropped via select/default)
	done := make(chan struct{})
	go func() {
		c.Send([]byte("overflow"))
		close(done)
	}()

	select {
	case <-done:
		// good — did not block
	case <-time.After(time.Second):
		t.Fatal("Send blocked on full buffer")
	}
}

func TestPresenceBroadcastOnSubscribe(t *testing.T) {
	hub := startHub(t)
	chID := uuid.New()

	// Register two users and subscribe the first to the channel.
	alice := fakeClient(hub, uuid.New())
	bob := fakeClient(hub, uuid.New())

	hub.register <- alice
	hub.register <- bob
	time.Sleep(50 * time.Millisecond)

	hub.subscribeCh <- &subscription{client: alice, channelID: chID}
	time.Sleep(50 * time.Millisecond)
	_ = drainOne(t, alice) // "subscribed" ack

	// Now Bob subscribes — Alice should get a presence_change (online) for Bob.
	hub.subscribeCh <- &subscription{client: bob, channelID: chID}
	time.Sleep(50 * time.Millisecond)
	_ = drainOne(t, bob) // "subscribed" ack

	ev := drainOne(t, alice)
	if ev.Type != "presence_change" {
		t.Fatalf("expected presence_change, got %s", ev.Type)
	}
	if ev.UserID != bob.userID.String() {
		t.Fatalf("expected user_id=%s, got %s", bob.userID, ev.UserID)
	}
	if ev.Status != "online" {
		t.Fatalf("expected status=online, got %s", ev.Status)
	}

	// Bob should NOT receive his own presence event.
	select {
	case data := <-bob.send:
		t.Fatalf("bob should not get his own presence event, got %s", string(data))
	default:
	}
}

func TestPresenceBroadcastOnDisconnect(t *testing.T) {
	hub := startHub(t)
	chID := uuid.New()

	alice := fakeClient(hub, uuid.New())
	bob := fakeClient(hub, uuid.New())

	hub.register <- alice
	hub.register <- bob
	time.Sleep(50 * time.Millisecond)

	hub.subscribeCh <- &subscription{client: alice, channelID: chID}
	time.Sleep(50 * time.Millisecond)
	_ = drainOne(t, alice) // "subscribed" ack

	hub.subscribeCh <- &subscription{client: bob, channelID: chID}
	time.Sleep(50 * time.Millisecond)
	_ = drainOne(t, bob)   // "subscribed" ack
	_ = drainOne(t, alice) // presence_change online for bob

	// Bob disconnects — Alice should get presence_change (offline).
	hub.unregister <- bob
	time.Sleep(100 * time.Millisecond)

	ev := drainOne(t, alice)
	if ev.Type != "presence_change" {
		t.Fatalf("expected presence_change, got %s", ev.Type)
	}
	if ev.Status != "offline" {
		t.Fatalf("expected status=offline, got %s", ev.Status)
	}
	if ev.UserID != bob.userID.String() {
		t.Fatalf("expected user_id=%s, got %s", bob.userID, ev.UserID)
	}
}
