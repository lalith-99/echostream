package presence

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func TestConcurrentGatewayLeases(t *testing.T) {
	url := os.Getenv("ECHOSTREAM_TEST_REDIS_URL")
	if url == "" {
		t.Skip("ECHOSTREAM_TEST_REDIS_URL is unset")
	}
	opts, err := goredis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	r := goredis.NewClient(opts)
	t.Cleanup(func() { _ = r.Close() })
	first, second := NewTracker(r, zap.NewNop()), NewTracker(r, zap.NewNop())
	user := uuid.New()
	ctx := context.Background()
	t.Cleanup(func() { r.Del(ctx, presenceKey(user)) })
	start := func(tracker *Tracker, want int64) func() {
		t.Helper()
		life, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { defer close(done); tracker.Track(life, user) }()
		stop := func() { cancel(); <-done }
		t.Cleanup(stop)
		deadline := time.Now().Add(3 * time.Second)
		for r.ZCard(ctx, presenceKey(user)).Val() != want {
			if time.Now().After(deadline) {
				t.Fatalf("expected %d presence leases", want)
			}
			time.Sleep(5 * time.Millisecond)
		}
		return stop
	}
	closeFirst := start(first, 1)
	closeSecond := start(second, 2)
	closeFirst()
	if status := first.BulkStatus(ctx, []uuid.UUID{user})[user]; status != Online {
		t.Fatal("disconnect on one gateway hid another live socket", status)
	}
	closeSecond()
	if status := second.BulkStatus(ctx, []uuid.UUID{user})[user]; status != Offline {
		t.Fatal("last disconnect left user online", status)
	}
	if err := r.ZAdd(ctx, presenceKey(user), goredis.Z{Score: 1, Member: "expired"}).Err(); err != nil {
		t.Fatal(err)
	}
	if status := first.BulkStatus(ctx, []uuid.UUID{user})[user]; status != Offline {
		t.Fatal("expired lease counted as live", status)
	}
}
