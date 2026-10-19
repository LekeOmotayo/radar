// Package middleware_test tests rate limiting, tracing, and auth middleware.
// Run with: go test -race ./internal/middleware/...
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lekeomotayo/radar/internal/middleware"
)

func init() { gin.SetMode(gin.TestMode) }

func simpleRouter(handlers ...gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.GET("/test", append(handlers, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})...)
	return r
}

// ── Trace Context ─────────────────────────────────────────────────────────────

func TestTraceContext_GeneratesHeaders(t *testing.T) {
	r := simpleRouter(middleware.TraceContext())
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	tp := w.Header().Get("traceparent")
	if tp == "" {
		t.Error("expected traceparent response header")
	}
	// Format: "00-<traceId>-<spanId>-01"
	if len(tp) < 10 {
		t.Errorf("traceparent too short: %q", tp)
	}
}

func TestTraceContext_PropagatesExisting(t *testing.T) {
	r := simpleRouter(middleware.TraceContext())
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	existingTrace := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	req.Header.Set("traceparent", existingTrace)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	tp := w.Header().Get("traceparent")
	// trace-id should be preserved, span-id regenerated
	if !containsTraceID(tp, "4bf92f3577b34da6a3ce929d0e0e4736") {
		t.Errorf("expected trace ID to be preserved, got: %q", tp)
	}
}

func containsTraceID(traceparent, traceID string) bool {
	// traceparent format: "00-TRACEID-SPANID-01"
	if len(traceparent) < 3+len(traceID) {
		return false
	}
	return traceparent[3:3+len(traceID)] == traceID
}

// ── Rate Limiter ──────────────────────────────────────────────────────────────

func TestRateLimiter_AllowsNormalTraffic(t *testing.T) {
	r := simpleRouter(middleware.RateLimiter(100, 200))
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, w.Code)
		}
	}
}

func TestRateLimiter_BlocksBurst(t *testing.T) {
	// Very tight limits: 1 rps, burst of 1
	r := simpleRouter(middleware.RateLimiter(1, 1))
	rejected := 0
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.1:9999"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			rejected++
		}
	}
	if rejected == 0 {
		t.Error("expected some requests to be rate-limited")
	}
}

func TestRateLimiter_Concurrent(t *testing.T) {
	r := simpleRouter(middleware.RateLimiter(50, 100))
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = "10.0.0.1:0"
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			// We don't assert a specific status — just ensure no panic/race
		}(i)
	}
	wg.Wait()
}

// ── Bearer Auth ───────────────────────────────────────────────────────────────

func TestBearerAuth_DevMode_NoTokens(t *testing.T) {
	// Empty token list = dev mode = no auth
	r := simpleRouter(middleware.BearerAuth(nil))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("dev mode: expected 200 without auth, got %d", w.Code)
	}
}

func TestBearerAuth_ValidToken(t *testing.T) {
	r := simpleRouter(middleware.BearerAuth([]string{"secret-agent-token"}))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer secret-agent-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with valid token, got %d", w.Code)
	}
}

func TestBearerAuth_InvalidToken(t *testing.T) {
	r := simpleRouter(middleware.BearerAuth([]string{"secret-agent-token"}))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong token, got %d", w.Code)
	}
}

func TestBearerAuth_MissingHeader(t *testing.T) {
	r := simpleRouter(middleware.BearerAuth([]string{"token123"}))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth header, got %d", w.Code)
	}
}
