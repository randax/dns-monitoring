package dns

import (
	"context"

	"github.com/randax/dns-monitoring/internal/config"
)

type Engine struct {
	cfg  *config.Config
	pool *WorkerPool
}

func NewEngine(cfg *config.Config) *Engine {
	return &Engine{
		cfg:  cfg,
		pool: NewWorkerPool(cfg),
	}
}

func (e *Engine) Run(ctx context.Context) (<-chan Result, error) {
	return e.pool.Execute(ctx), nil
}