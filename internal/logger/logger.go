// Package logger provides a thin, zero-allocation-friendly wrapper around go.uber.org/zap.
package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger is the project-wide structured logger alias.
type Logger = *zap.Logger

// Field aliases for ergonomic call sites.
var (
	String = zap.String
	Int    = zap.Int
	Bool   = zap.Bool
	Err    = zap.Error
	Any    = zap.Any
)

// New constructs a production-grade zap logger at the requested level.
// Panics if the configuration is invalid (only possible with a bad level string).
func New(level string) *zap.Logger {
	lvl := zapcore.InfoLevel
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = zapcore.InfoLevel
	}

	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(lvl)
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	log, err := cfg.Build(zap.AddCallerSkip(0))
	if err != nil {
		panic("logger init failed: " + err.Error())
	}
	return log
}
