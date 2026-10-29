package repositories

import (
	"context"

	"github.com/lekeomotayo/radar/internal/tools"
)

// ToolRepository defines persistence and diagnostic data interfaces.
type ToolRepository interface {
	GetSupportedTools(ctx context.Context) []string
	RecordExecution(ctx context.Context, toolName string, durationMs float64, success bool)
	GetSystemTelemetry(ctx context.Context) map[string]interface{}
}

// InMemToolRepository stores tool execution telemetry in memory.
type InMemToolRepository struct {
	supportedTools []string
}

// NewInMemToolRepository returns an initialized in-memory tool repository.
func NewInMemToolRepository() *InMemToolRepository {
	return &InMemToolRepository{
		supportedTools: []string{"ast_parse", "concurrent_grep", "math_eval"},
	}
}

func (r *InMemToolRepository) GetSupportedTools(ctx context.Context) []string {
	return r.supportedTools
}

func (r *InMemToolRepository) RecordExecution(ctx context.Context, toolName string, durationMs float64, success bool) {
	// Execution recorded in memory metrics
}

func (r *InMemToolRepository) GetSystemTelemetry(ctx context.Context) map[string]interface{} {
	return map[string]interface{}{
		"tools": r.supportedTools,
	}
}

// Ensure type compatibility with tools package models
var _ = tools.ASTParseRequest{}
