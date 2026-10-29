package services

import (
	"context"
	"runtime"
	"time"

	"github.com/lekeomotayo/radar/internal/api/repositories"
	"github.com/lekeomotayo/radar/internal/config"
	"github.com/lekeomotayo/radar/internal/logger"
)

// ToolService defines the business logic for tool execution and runtime diagnostics.
type ToolService interface {
	GetRuntimeMetrics(ctx context.Context) map[string]interface{}
	GetToolCatalog(ctx context.Context) []string
}

type toolService struct {
	cfg  *config.Config
	log  logger.Logger
	repo repositories.ToolRepository
}

// NewToolService constructs a new ToolService instance.
func NewToolService(cfg *config.Config, log logger.Logger, repo repositories.ToolRepository) ToolService {
	return &toolService{
		cfg:  cfg,
		log:  log,
		repo: repo,
	}
}

func (s *toolService) GetRuntimeMetrics(ctx context.Context) map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return map[string]interface{}{
		"goroutines":   runtime.NumGoroutine(),
		"alloc_bytes":  m.Alloc,
		"total_alloc":  m.TotalAlloc,
		"sys_bytes":    m.Sys,
		"gc_cycles":    m.NumGC,
		"num_cpu":      runtime.NumCPU(),
		"version":      s.cfg.Version,
		"env":          s.cfg.Env,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}
}

func (s *toolService) GetToolCatalog(ctx context.Context) []string {
	return s.repo.GetSupportedTools(ctx)
}
