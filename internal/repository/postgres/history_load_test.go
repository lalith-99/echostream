package postgres

import (
	"context"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalith-99/echostream/internal/repository"
)

// TestHistoryReadLoad runs the same validated requests against both binaries.
// scripts/history/run.sh supplies the isolated database and interleaves trials.
func TestHistoryReadLoad(t *testing.T) {
	dsn := os.Getenv("HISTORY_LOAD_URL")
	if dsn == "" {
		t.Skip("run scripts/history/run.sh for the one-million-row fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	// Use the server's 25-connection maximum, including pool wait in latency.
	cfg.MaxConns = 25
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewMessageStore(pool)
	tenant := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	member := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	const channels, rowsPerChannel, pageSize = 16, 62500, 20
	var ids [channels]uuid.UUID
	for i := range ids {
		ids[i] = uuid.MustParse(fmt.Sprintf("30000000-0000-0000-0000-%012d", i+1))
	}
	padding := strings.Repeat("x", 64)
	read := func(i int) (time.Duration, error) {
		channel := i % channels
		offset := pageSize + (i*7919)%(rowsPerChannel-2*pageSize)
		cursor := int64(offset*channels + channel + 1)
		page := repository.MessagePage{Limit: pageSize}
		first, step := int64((rowsPerChannel-1)*channels+channel+1), int64(-channels)
		switch i % 3 {
		case 1:
			page.Before = cursor
			first = cursor - channels
		case 2:
			page.After = &cursor
			first, step = cursor+channels, channels
		}

		start := time.Now()
		messages, err := store.ListByChannel(ctx, tenant, ids[channel], member, page)
		latency := time.Since(start)
		if err != nil || len(messages) != pageSize {
			return 0, fmt.Errorf("request %d returned %d messages: %v", i, len(messages), err)
		}
		for j, m := range messages {
			want := first + int64(j)*step
			number, hasPrefix := strings.CutPrefix(m.Body, "message-")
			number, hasSuffix := strings.CutSuffix(number, padding)
			bodyID, err := strconv.ParseInt(number, 10, 64)
			if m.ID != want || m.ChannelID != ids[channel] || m.SenderID != member || !hasPrefix || !hasSuffix || err != nil || bodyID != want {
				return 0, fmt.Errorf("request %d item %d: unexpected ID, channel, sender or body", i, j)
			}
		}
		return latency, nil
	}

	const clients = 32
	run := func(requests int) ([]time.Duration, time.Duration) {
		t.Helper()
		samples := make([]time.Duration, requests)
		errs := make(chan error, clients)
		var wg sync.WaitGroup
		start := time.Now()
		for client := 0; client < clients; client++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := client; i < requests; i += clients {
					d, err := read(i)
					if err != nil {
						errs <- err
						return
					}
					samples[i] = d
				}
			}()
		}
		wg.Wait()
		elapsed := time.Since(start)
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		return samples, elapsed
	}
	run(1024)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	samples, duration := run(65536)
	runtime.ReadMemStats(&after)
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	percentile := func(q float64) float64 {
		return float64(samples[int(math.Ceil(q*float64(len(samples))))-1]) / float64(time.Millisecond)
	}
	t.Logf("history_load samples=%d clients=%d duration_s=%.3f pages_s=%.1f p50_ms=%.3f p95_ms=%.3f p99_ms=%.3f allocated_bytes_page=%.0f mallocs_page=%.1f",
		len(samples), clients, duration.Seconds(), float64(len(samples))/duration.Seconds(), percentile(.5), percentile(.95), percentile(.99),
		float64(after.TotalAlloc-before.TotalAlloc)/float64(len(samples)), float64(after.Mallocs-before.Mallocs)/float64(len(samples)))
}
