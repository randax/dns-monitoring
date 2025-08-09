package dns

import (
	"context"

	"github.com/randax/dns-monitoring/internal/config"
)

type Engine struct {
	cfg             *config.Config
	pool            *WorkerPool
	cacheAnalyzer   *CacheAnalyzer
	networkAnalyzer *NetworkAnalyzer
}

func NewEngine(cfg *config.Config) *Engine {
	e := &Engine{
		cfg:  cfg,
		pool: NewWorkerPool(cfg),
	}
	
	// Initialize analyzers if enabled
	if cfg.Cache.Enabled {
		e.cacheAnalyzer = NewCacheAnalyzer()
	}
	
	if cfg.Network.Enabled {
		e.networkAnalyzer = NewNetworkAnalyzer(cfg.Network.SampleSize)
	}
	
	return e
}

func (e *Engine) Run(ctx context.Context) (<-chan Result, error) {
	// Create a channel for raw results from the worker pool
	rawResults := e.pool.Execute(ctx)
	
	// Create a channel for processed results
	processedResults := make(chan Result, cap(rawResults))
	
	// Start a goroutine to process results through analyzers
	go func() {
		defer close(processedResults)
		
		for result := range rawResults {
			// Apply cache analysis if enabled
			if e.cacheAnalyzer != nil {
				e.cacheAnalyzer.AnalyzeResult(&result, e.cfg.Cache)
			}
			
			// Apply network analysis if enabled
			if e.networkAnalyzer != nil {
				e.networkAnalyzer.AnalyzeResult(&result, e.cfg.Network)
			}
			
			// Send the enhanced result
			select {
			case processedResults <- result:
			case <-ctx.Done():
				return
			}
		}
	}()
	
	return processedResults, nil
}