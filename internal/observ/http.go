package observ

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Only registered route templates enter this map. Paths, IDs and query strings never become labels.
type HTTPMetrics struct {
	mu       sync.Mutex
	counts   map[string]uint64
	duration map[string]float64
}

func NewHTTPMetrics() *HTTPMetrics {
	return &HTTPMetrics{counts: make(map[string]uint64), duration: make(map[string]float64)}
}

func (m *HTTPMetrics) Observe(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		method := c.Request.Method
		switch method {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD":
		default:
			method = "OTHER"
		}
		ctx := TraceContext.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		ctx, span := otel.Tracer("echostream").Start(ctx, method+" "+route, trace.WithSpanKind(trace.SpanKindServer))
		c.Request = c.Request.WithContext(ctx)
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("request panicked", zap.String("route", route), zap.Stack("stack"))
				c.AbortWithStatus(http.StatusInternalServerError)
			}
			status := c.Writer.Status()
			elapsed := time.Since(start)
			span.SetAttributes(attribute.String("http.route", route), attribute.Int("http.response.status_code", status))
			if status >= 500 {
				span.SetStatus(codes.Error, "server error")
			}
			span.End()
			key := fmt.Sprintf("method=%q,route=%q,status=%q", method, route, fmt.Sprint(status))
			m.mu.Lock()
			m.counts[key]++
			m.duration[key] += elapsed.Seconds()
			m.mu.Unlock()
			logger.Debug("http request", zap.String("method", method), zap.String("route", route), zap.Int("status", status), zap.Duration("duration", elapsed))
		}()
		c.Next()
	}
}

type QueueStats func(context.Context) (pending int64, oldestSeconds float64, err error)

func (m *HTTPMetrics) Management(active func() int64, queue QueueStats) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		pending, oldest, err := queue(ctx)
		if err != nil {
			http.Error(w, "outbox metrics unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprintf(w, "# TYPE echostream_websocket_connections gauge\nechostream_websocket_connections %d\n# TYPE echostream_outbox_pending gauge\nechostream_outbox_pending %d\n# TYPE echostream_outbox_oldest_seconds gauge\nechostream_outbox_oldest_seconds %g\n", active(), pending, oldest)
		m.mu.Lock()
		keys := make([]string, 0, len(m.counts))
		counters := make(map[string]uint64, len(m.counts))
		durations := make(map[string]float64, len(m.counts))
		for k, v := range m.counts {
			keys = append(keys, k)
			counters[k] = v
			durations[k] = m.duration[k]
		}
		m.mu.Unlock()
		sort.Strings(keys)
		fmt.Fprintln(w, "# TYPE echostream_http_requests_total counter")
		for _, k := range keys {
			fmt.Fprintf(w, "echostream_http_requests_total{%s} %d\n", k, counters[k])
		}
		fmt.Fprintln(w, "# TYPE echostream_http_request_duration_seconds summary")
		for _, k := range keys {
			fmt.Fprintf(w, "echostream_http_request_duration_seconds_count{%s} %d\nechostream_http_request_duration_seconds_sum{%s} %g\n", k, counters[k], k, durations[k])
		}
	})
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	return mux
}
