// Package server wires up the Gin router with all middleware, REST endpoints,
// and the JSON-RPC multiplexer.
package server

import (
	"embed"
	"io/fs"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lekeomotayo/radar/internal/config"
	"github.com/lekeomotayo/radar/internal/jsonrpc"
	"github.com/lekeomotayo/radar/internal/logger"
	"github.com/lekeomotayo/radar/internal/middleware"
	"github.com/lekeomotayo/radar/internal/tools"
)

//go:embed static/*
var staticFS embed.FS

// New constructs and returns a configured *http.Server ready to ListenAndServe.
func New(cfg *config.Config, log logger.Logger) *http.Server {
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()

	// ── Global middleware stack ────────────────────────────────────────────
	r.Use(gin.Recovery())
	r.Use(middleware.SecurityHeaders())
	r.Use(middleware.TraceContext())
	r.Use(middleware.RequestLogger(log))
	r.Use(middleware.RateLimiter(cfg.RateLimit, cfg.RateBurst))

	// ── Embedded Frontend Developer Console ────────────────────────────────
	indexHTML, err := staticFS.ReadFile("static/index.html")
	if err == nil {
		r.GET("/", func(c *gin.Context) {
			c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
		})
	}
	staticContent, err := fs.Sub(staticFS, "static")
	if err == nil {
		r.StaticFS("/static", http.FS(staticContent))
	}
	styleCSS, errStyle := staticFS.ReadFile("static/style.css")
	if errStyle == nil {
		cssHandler := func(c *gin.Context) {
			c.Data(http.StatusOK, "text/css; charset=utf-8", styleCSS)
		}
		r.GET("/style.css", cssHandler)
		r.GET("/css/styles.css", cssHandler)
		r.GET("/styles.css", cssHandler)
	}
	appJS, errApp := staticFS.ReadFile("static/app.js")
	if errApp == nil {
		jsHandler := func(c *gin.Context) {
			c.Data(http.StatusOK, "application/javascript; charset=utf-8", appJS)
		}
		r.GET("/app.js", jsHandler)
		r.GET("/js/app.js", jsHandler)
	}

	// ── Health & readiness ─────────────────────────────────────────────────
	r.GET("/health", handleHealth(cfg))
	r.GET("/ready", handleReady())

	// ── Authenticated tool routes ──────────────────────────────────────────
	api := r.Group("/tools", middleware.BearerAuth(cfg.AgentTokens))
	{
		api.POST("/ast-parse", tools.HandleASTparse(cfg, log))
		api.POST("/grep", tools.HandleGrep(cfg, log))
		api.POST("/math-eval", tools.HandleMathEval(cfg, log))
	}

	// ── JSON-RPC 2.0 endpoint ──────────────────────────────────────────────
	rpc := r.Group("/rpc", middleware.BearerAuth(cfg.AgentTokens))
	{
		rpc.POST("", jsonrpc.Handler(cfg))
	}

	return &http.Server{
		Addr:         cfg.Addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}

// ── Health handlers ──────────────────────────────────────────────────────────

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Service string `json:"service"`
}

func handleHealth(cfg *config.Config) gin.HandlerFunc {
	resp := healthResponse{
		Status:  "ok",
		Version: cfg.Version,
		Service: cfg.ServiceName,
	}
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, resp)
	}
}

func handleReady() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}
