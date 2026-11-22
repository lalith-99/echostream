package postgres

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalith-99/echostream/internal/models"
	"github.com/lalith-99/echostream/internal/observ"
	"github.com/lalith-99/echostream/internal/repository"
	"github.com/lalith-99/echostream/migrations"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Use a private schema so tests can pause/dispatch the queue without consuming
// another test's messages. The role must own the disposable test database.
func messageTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := pgx.Identifier{"message_test_" + uuid.NewString()}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if err := migrations.Apply(ctx, conn.Conn()); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestPostgresMessageOutbox(t *testing.T) {
	pool := messageTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store := NewMessageStore(pool)
	tenant, user, channel := uuid.New(), uuid.New(), uuid.New()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	count := func(q string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	exec("INSERT INTO tenants (id,name) VALUES ($1,'test')", tenant)
	exec("INSERT INTO users (id,tenant_id,email,display_name) VALUES ($1,$2,'member@test.invalid','Member')", user, tenant)
	exec("INSERT INTO channels (id,tenant_id,name,is_private) VALUES ($1,$2,'test',true)", channel, tenant)
	exec("INSERT INTO channel_members (channel_id,user_id,role) VALUES ($1,$2,'admin')", channel, user)

	// An authorized empty page is a JSON array. Inaccessible channels are forbidden.
	empty, err := store.ListByChannel(ctx, tenant, channel, user, repository.MessagePage{Limit: 50})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("authorized empty page: %v, %v", empty, err)
	}

	// Force a failure after the message INSERT to prove the API cannot
	// acknowledge a durable message that lacks its publish intent.
	exec("ALTER TABLE message_outbox ADD CONSTRAINT simulated_failure CHECK (message_id < 0)")
	if _, _, err := store.Create(ctx, tenant, channel, user, "rollback", nil); err == nil {
		t.Fatal("expected enqueue to fail")
	}
	if n := count("SELECT count(*) FROM messages"); n != 0 {
		t.Fatalf("failed enqueue left %d messages", n)
	}
	select {
	case <-store.Wakeups():
		t.Fatal("rolled back send woke a worker")
	default:
	}
	exec("ALTER TABLE message_outbox DROP CONSTRAINT simulated_failure")

	const traceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	sendCtx := observ.TraceContext.Extract(ctx, propagation.MapCarrier{"traceparent": traceparent})
	key := uuid.New()
	const writers = 12
	type outcome struct {
		message *models.Message
		created bool
		err     error
	}
	outcomes := make(chan outcome, writers)
	var writersDone sync.WaitGroup
	for i := 0; i < writers; i++ {
		writersDone.Add(1)
		go func() {
			defer writersDone.Done()
			m, created, err := store.Create(sendCtx, tenant, channel, user, "same send", &key)
			outcomes <- outcome{m, created, err}
		}()
	}
	writersDone.Wait()
	close(outcomes)
	var id int64
	createdCount := 0
	for result := range outcomes {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if id != 0 && result.message.ID != id {
			t.Fatalf("replay ID %d != %d", result.message.ID, id)
		}
		id = result.message.ID
		if result.created {
			createdCount++
		}
	}
	select {
	case <-store.Wakeups():
	default:
		t.Fatal("committed send did not wake a worker")
	}
	if createdCount != 1 || count("SELECT count(*) FROM message_outbox") != 1 {
		t.Fatal("retries must enqueue exactly one publish")
	}
	if _, _, err := store.Create(ctx, tenant, channel, user, "changed", &key); !errors.Is(err, repository.ErrMessageKeyConflict) {
		t.Fatal("expected key conflict", err)
	}

	// A Redis error persists the retry time. A restarted worker will find it.
	publishErr := errors.New("publisher unavailable")
	if _, err := store.DispatchNext(ctx, func(context.Context, *models.Message) error { return publishErr }); !errors.Is(err, publishErr) {
		t.Fatal("expected publish error", err)
	}
	if count("SELECT count(*) FROM message_outbox WHERE attempts=1 AND available_at > now()") != 1 {
		t.Fatal("failed publish was not deferred")
	}
	exec("UPDATE message_outbox SET available_at=now()")
	// Redis may have accepted a publish just as the process loses its DB session.
	// Retain the same durable ID for the next delivery attempt.
	lost, stopPublish := context.WithCancel(ctx)
	_, err = store.DispatchNext(lost, func(_ context.Context, m *models.Message) error {
		stopPublish()
		return nil
	})
	stopPublish()
	if err == nil || count("SELECT count(*) FROM message_outbox") != 1 {
		t.Fatal("ambiguous publish lost its retry", err)
	}
	locked, release := make(chan struct{}), make(chan struct{})
	releaseWorker := sync.OnceFunc(func() { close(release) })
	defer releaseWorker()
	done := make(chan error, 1)
	go func() {
		_, err := store.DispatchNext(ctx, func(publishCtx context.Context, m *models.Message) error {
			close(locked)
			if trace.SpanContextFromContext(publishCtx).TraceID() != trace.SpanContextFromContext(sendCtx).TraceID() {
				return errors.New("publish lost the send trace")
			}
			if m.ID != id {
				return errors.New("unexpected claimed message")
			}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		done <- err
	}()
	select {
	case <-locked:
	case <-ctx.Done():
		t.Fatal("worker did not claim row", ctx.Err())
	}
	found, err := store.DispatchNext(ctx, func(context.Context, *models.Message) error { return errors.New("double claim") })
	if err != nil || found {
		t.Fatalf("second worker claimed a locked row: found=%v err=%v", found, err)
	}
	releaseWorker()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if count("SELECT count(*) FROM message_outbox") != 0 {
		t.Fatal("published row still pending")
	}

	// A transaction holding a channel lock may already have obtained a smaller
	// message ID. Later writers must wait before assigning and committing theirs.
	older, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer older.Rollback(ctx)
	if _, err := older.Exec(ctx, "SELECT id FROM channels WHERE id=$1 FOR NO KEY UPDATE", channel); err != nil {
		t.Fatal(err)
	}
	short, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	_, _, err = store.Create(short, tenant, channel, user, "would skip pending commit", nil)
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("writer did not wait for preceding transaction", err)
	}
	if err := older.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	for _, body := range []string{"two", "three", "four"} {
		if _, _, err := store.Create(ctx, tenant, channel, user, body, nil); err != nil {
			t.Fatal(err)
		}
	}
	latest, err := store.ListByChannel(ctx, tenant, channel, user, repository.MessagePage{Limit: 2})
	if err != nil || len(latest) != 2 || latest[0].Body != "four" || latest[1].Body != "three" {
		t.Fatal("latest", latest, err)
	}
	after := id
	next, err := store.ListByChannel(ctx, tenant, channel, user, repository.MessagePage{After: &after, Limit: 2})
	if err != nil || len(next) != 2 || next[0].Body != "two" || next[1].Body != "three" {
		t.Fatal("first catch-up page", next, err)
	}
	after = next[1].ID
	next, err = store.ListByChannel(ctx, tenant, channel, user, repository.MessagePage{After: &after, Limit: 2})
	if err != nil || len(next) != 1 || next[0].Body != "four" {
		t.Fatal("second catch-up page", next, err)
	}
	before, err := store.ListByChannel(ctx, tenant, channel, user, repository.MessagePage{Before: latest[1].ID, Limit: 2})
	if err != nil || len(before) != 2 || before[0].Body != "two" || before[1].ID != id {
		t.Fatal("older page", before, err)
	}

	for _, who := range []struct{ tenant, user uuid.UUID }{{uuid.New(), user}, {tenant, uuid.New()}} {
		if _, err := store.ListByChannel(ctx, who.tenant, channel, who.user, repository.MessagePage{Limit: 1}); !errors.Is(err, repository.ErrMessageForbidden) {
			t.Fatal("unauthorized history", err)
		}
		if _, _, err := store.Create(ctx, who.tenant, channel, who.user, "x", nil); !errors.Is(err, repository.ErrMessageForbidden) {
			t.Fatal("unauthorized send", err)
		}
	}
}
