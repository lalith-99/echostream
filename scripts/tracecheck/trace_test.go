// Package tracecheck exercises the released server's telemetry over OTLP/HTTP.
package tracecheck

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	collectorpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

type recordedSpan struct {
	instance, name, trace, id, parent string
}

type recorder struct {
	mu    sync.Mutex
	spans []recordedSpan
}

func (r *recorder) endpoint(t *testing.T, instance string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.URL.Path != "/v1/traces" {
			http.NotFound(w, req)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, 4<<20))
		var batch collectorpb.ExportTraceServiceRequest
		if err != nil || proto.Unmarshal(body, &batch) != nil {
			http.Error(w, "invalid OTLP protobuf", http.StatusBadRequest)
			return
		}
		r.mu.Lock()
		for _, resource := range batch.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				for _, span := range scope.Spans {
					r.spans = append(r.spans, recordedSpan{instance, span.Name, hex.EncodeToString(span.TraceId), hex.EncodeToString(span.SpanId), hex.EncodeToString(span.ParentSpanId)})
				}
			}
		}
		r.mu.Unlock()
		// An empty protobuf ExportTraceServiceResponse denotes full success.
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func (r *recorder) messageTrace() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, send := range r.spans {
		if send.instance != "writer" || send.name != "POST /v1/channels/:id/messages" || send.trace == "" {
			continue
		}
		for _, relay := range r.spans {
			if relay.name != "outbox.publish" || relay.trace != send.trace || relay.parent != send.id {
				continue
			}
			for _, fanout := range r.spans {
				if fanout.instance == "reader" && fanout.name == "redis.fanout" && fanout.trace == send.trace && fanout.parent == relay.id {
					return true
				}
			}
		}
	}
	return false
}

func startPeer(t *testing.T, bin, databaseURL, redisURL, collector string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	cmd := exec.Command(bin)
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	// The test owns its collector, sampler and resource configuration.
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "OTEL_") {
			cmd.Env = append(cmd.Env, variable)
		}
	}
	cmd.Env = append(cmd.Env, "ENV=test", "ADMIN_PORT=", "PORT="+port, "DATABASE_URL="+databaseURL, "REDIS_URL="+redisURL,
		"OTEL_EXPORTER_OTLP_ENDPOINT="+collector, "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT="+collector+"/v1/traces", "OTEL_BSP_SCHEDULE_DELAY=100", "OTEL_TRACES_SAMPLER=always_on")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		stopped := make(chan error, 1)
		go func() { stopped <- cmd.Wait() }()
		select {
		case err := <-stopped:
			if err != nil {
				t.Errorf("peer exited: %v", err)
			}
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-stopped
			t.Error("peer did not drain")
		}
		if t.Failed() {
			t.Log(logs.String())
		}
	})
	url := "http://127.0.0.1:" + port
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/v1/ready", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return url
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("peer did not become ready")
	return ""
}

// Build bin/server and migrate a disposable DB before running. This also uses
// the same public two-instance smoke client exercised by the Compose CI job.
func TestCrossInstanceMessageTrace(t *testing.T) {
	bin, databaseURL, redisURL := os.Getenv("TEST_SERVER_BIN"), os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_REDIS_URL")
	if bin == "" || databaseURL == "" || redisURL == "" {
		t.Skip("set TEST_SERVER_BIN, TEST_DATABASE_URL and TEST_REDIS_URL")
	}
	records := &recorder{}
	writer := startPeer(t, bin, databaseURL, redisURL, records.endpoint(t, "writer"))
	reader := startPeer(t, bin, databaseURL, redisURL, records.endpoint(t, "reader"))
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "node", "../smoke.mjs", writer, reader).CombinedOutput()
	if err != nil {
		t.Fatalf("cross-instance smoke: %v\n%s", err, output)
	}
	for ctx.Err() == nil {
		if records.messageTrace() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	records.mu.Lock()
	defer records.mu.Unlock()
	t.Fatalf("missing HTTP send -> outbox.publish -> reader redis.fanout trace: %+v", records.spans)
}
