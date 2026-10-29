// Package config loads and validates all runtime configuration from environment variables.
package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all application-level settings.
type Config struct {
	Env     string
	Version string
	Addr    string

	// Rate limiting
	RateLimit  int           // requests per second per IP
	RateBurst  int           // burst allowance
	RateWindow time.Duration // window for cleanup

	// Security
	AgentTokens []string // valid bearer tokens for agent auth

	// Tracing
	ServiceName string

	// Logging
	LogLevel string

	// Tool-specific
	MaxGrepFileSizeMB int64 // max file size for grep tool (MB)
	MaxGrepMatches    int   // max returned grep matches
	SandboxTimeoutSec int   // AST parse / expression eval timeout
}

// Load reads configuration from the environment with safe defaults.
func Load() *Config {
	return &Config{
		Env:               getEnv("RADAR_ENV", "development"),
		Version:           getEnv("RADAR_VERSION", "1.0.0"),
		Addr:              getEnv("RADAR_ADDR", ":8087"),
		RateLimit:         getEnvInt("RADAR_RATE_LIMIT", 20),
		RateBurst:         getEnvInt("RADAR_RATE_BURST", 40),
		RateWindow:        time.Minute,
		AgentTokens:       splitEnv("RADAR_AGENT_TOKENS", ","),
		ServiceName:       getEnv("RADAR_SERVICE_NAME", "radar"),
		LogLevel:          getEnv("RADAR_LOG_LEVEL", "info"),
		MaxGrepFileSizeMB: int64(getEnvInt("RADAR_MAX_GREP_FILE_MB", 10)),
		MaxGrepMatches:    getEnvInt("RADAR_MAX_GREP_MATCHES", 500),
		SandboxTimeoutSec: getEnvInt("RADAR_SANDBOX_TIMEOUT_SEC", 5),
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func splitEnv(key, sep string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(v); i++ {
		if string(v[i]) == sep {
			if part := v[start:i]; part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	if part := v[start:]; part != "" {
		out = append(out, part)
	}
	return out
}
