// Package middleware provides HTTP middleware for authentication, rate-limiting,
// distributed tracing, and request/response logging.
package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lekeomotayo/radar/internal/apierr"
	"github.com/lekeomotayo/radar/internal/logger"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// ── W3C Trace Context ────────────────────────────────────────────────────────

const (
	headerTraceparent = "traceparent"
	headerTracestate  = "tracestate"
	ctxKeyTraceID     = "trace_id"
	ctxKeySpanID      = "span_id"
)

// TraceContext injects W3C Trace Context headers (traceparent / tracestate)
// into every request, generating a new trace ID if one is not already present.
func TraceContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		tp := c.GetHeader(headerTraceparent)
		var traceID, spanID string

		if tp != "" {
			// Parse existing traceparent: "version-traceId-spanId-flags"
			parts := strings.SplitN(tp, "-", 4)
			if len(parts) == 4 {
				traceID = parts[1]
				spanID = newSpanID()
			}
		}

		if traceID == "" {
			traceID = strings.ReplaceAll(uuid.New().String(), "-", "")
		}
		if spanID == "" {
			spanID = newSpanID()
		}

		// Build outgoing traceparent and propagate
		newTP := "00-" + traceID + "-" + spanID + "-01"
		c.Request.Header.Set(headerTraceparent, newTP)
		c.Header(headerTraceparent, newTP)

		if ts := c.GetHeader(headerTracestate); ts != "" {
			c.Header(headerTracestate, ts)
		}

		c.Set(ctxKeyTraceID, traceID)
		c.Set(ctxKeySpanID, spanID)

		c.Next()
	}
}

func newSpanID() string {
	id := uuid.New().String()
	return strings.ReplaceAll(id, "-", "")[:16]
}

// ── Structured Request Logger ────────────────────────────────────────────────

// RequestLogger logs every request with trace IDs, latency, and status.
func RequestLogger(log logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latency := time.Since(start)

		traceID, _ := c.Get(ctxKeyTraceID)
		spanID, _ := c.Get(ctxKeySpanID)

		log.Info("request",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", latency),
			zap.String("ip", c.ClientIP()),
			zap.Any("trace_id", traceID),
			zap.Any("span_id", spanID),
		)
	}
}

// ── Per-IP Rate Limiter ──────────────────────────────────────────────────────

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type rateLimiterStore struct {
	mu       sync.Mutex
	visitors map[string]*ipLimiter
	rps      rate.Limit
	burst    int
}

func newRateLimiterStore(rps, burst int) *rateLimiterStore {
	s := &rateLimiterStore{
		visitors: make(map[string]*ipLimiter),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
	go s.cleanup()
	return s
}

func (s *rateLimiterStore) get(ip string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, exists := s.visitors[ip]
	if !exists {
		l := rate.NewLimiter(s.rps, s.burst)
		s.visitors[ip] = &ipLimiter{limiter: l, lastSeen: time.Now()}
		return l
	}
	v.lastSeen = time.Now()
	return v.limiter
}

// cleanup evicts stale entries every minute to prevent unbounded growth.
func (s *rateLimiterStore) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		for ip, v := range s.visitors {
			if time.Since(v.lastSeen) > 3*time.Minute {
				delete(s.visitors, ip)
			}
		}
		s.mu.Unlock()
	}
}

// RateLimiter returns a per-IP token-bucket rate limiter middleware.
func RateLimiter(rps, burst int) gin.HandlerFunc {
	store := newRateLimiterStore(rps, burst)

	return func(c *gin.Context) {
		if !store.get(c.ClientIP()).Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": apierr.ErrRateLimited,
			})
			return
		}
		c.Next()
	}
}

// ── Bearer Token Authentication ──────────────────────────────────────────────

// BearerAuth validates Authorization: Bearer <token> against the configured
// agent token allowlist. If the allowlist is empty, auth is disabled (dev mode).
func BearerAuth(tokens []string) gin.HandlerFunc {
	if len(tokens) == 0 {
		// Dev mode: auth disabled
		return func(c *gin.Context) { c.Next() }
	}

	allowed := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		allowed[t] = struct{}{}
	}

	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": apierr.ErrUnauthorized})
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if _, ok := allowed[token]; !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": apierr.ErrUnauthorized})
			return
		}

		c.Next()
	}
}

// ── Security Headers ─────────────────────────────────────────────────────────

// SecurityHeaders sets defensive HTTP response headers.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Cache-Control", "no-store")
		c.Next()
	}
}
