package observ

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestMetricsContainRouteTemplates(t *testing.T) {
	m := NewHTTPMetrics()
	app := gin.New()
	app.Use(m.Observe(zap.NewNop()))
	app.GET("/channels/:id", func(c *gin.Context) { c.Status(204) })
	for _, path := range []string{"/channels/private-id?token=sample", "/missing-user-supplied-path"} {
		app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	rr := httptest.NewRecorder()
	m.Management(func() int64 { return 2 }, func(context.Context) (int64, float64, error) { return 3, 4.5, nil }).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	output := rr.Body.String()
	for _, s := range []string{`route="/channels/:id",status="204"} 1`, `route="unmatched",status="404"} 1`, "echostream_websocket_connections 2", "echostream_outbox_oldest_seconds 4.5"} {
		if !strings.Contains(output, s) {
			t.Fatalf("missing %q in %s", s, output)
		}
	}
	for _, s := range []string{"private-id", "token=", "missing-user-supplied-path"} {
		if strings.Contains(output, s) {
			t.Fatalf("raw request value %q in metrics", s)
		}
	}
}
