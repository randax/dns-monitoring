package dns

import (
	"context"
	"sync"
	"time"

	"github.com/randax/dns-monitoring/internal/config"
)

type WorkerPool struct {
	cfg       *config.Config
	sem       chan struct{}
	results   chan Result
	wg        sync.WaitGroup
}

func NewWorkerPool(cfg *config.Config) *WorkerPool {
	return &WorkerPool{
		cfg:     cfg,
		sem:     make(chan struct{}, cfg.Monitor.MaxConcurrent),
		results: make(chan Result, cfg.Monitor.MaxConcurrent*2),
	}
}

func (wp *WorkerPool) Execute(ctx context.Context) <-chan Result {
	go wp.run(ctx)
	return wp.results
}

func (wp *WorkerPool) run(ctx context.Context) {
	defer close(wp.results)

	jobs := wp.generateJobs()
	
	for _, job := range jobs {
		if !job.server.Enabled {
			continue
		}
		
		wp.wg.Add(1)
		go wp.executeJob(ctx, job)
	}
	
	wp.wg.Wait()
}

type queryJob struct {
	server config.DNSServer
	domain string
	qtype  string
}

func (wp *WorkerPool) generateJobs() []queryJob {
	var jobs []queryJob
	
	for _, server := range wp.cfg.DNS.Servers {
		if !server.Enabled {
			continue
		}
		for _, domain := range wp.cfg.DNS.Queries.Domains {
			for _, qtype := range wp.cfg.DNS.Queries.Types {
				jobs = append(jobs, queryJob{
					server: server,
					domain: domain,
					qtype:  qtype,
				})
			}
		}
	}
	
	return jobs
}

func (wp *WorkerPool) executeJob(ctx context.Context, job queryJob) {
	defer wp.wg.Done()
	
	select {
	case <-ctx.Done():
		return
	case wp.sem <- struct{}{}:
		defer func() { <-wp.sem }()
	}
	
	result := wp.executeQueryWithRetry(ctx, job)
	
	select {
	case wp.results <- result:
	case <-ctx.Done():
		return
	}
}

func (wp *WorkerPool) executeQueryWithRetry(ctx context.Context, job queryJob) Result {
	protocol := Protocol(job.server.Protocol)
	if protocol == "" {
		protocol = ProtocolUDP
	}
	
	result := Result{
		Server:    job.server.Address,
		Domain:    job.domain,
		QueryType: job.qtype,
		Protocol:  protocol,
		Timestamp: time.Now(),
	}
	
	timeout := wp.cfg.DNS.Queries.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	
	var lastErr error
	maxRetries := wp.cfg.DNS.Queries.Retries
	if maxRetries < 0 {
		maxRetries = 3
	}
	
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := calculateBackoff(attempt)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				result.Error = ctx.Err()
				result.Retries = attempt - 1
				return result
			}
		}
		
		queryCtx, cancel := context.WithTimeout(ctx, timeout)
		start := time.Now()
		
		err := wp.performQuery(queryCtx, job, &result)
		result.Duration = time.Since(start)
		cancel()
		
		if err == nil {
			result.Retries = attempt
			return result
		}
		
		lastErr = err
	}
	
	result.Error = lastErr
	result.Retries = maxRetries
	return result
}

func (wp *WorkerPool) performQuery(ctx context.Context, job queryJob, result *Result) error {
	switch result.Protocol {
	case ProtocolUDP:
		return queryUDP(ctx, job.server, job.domain, job.qtype, result)
	case ProtocolTCP:
		return queryTCP(ctx, job.server, job.domain, job.qtype, result)
	case ProtocolDoT:
		return queryDoT(ctx, job.server, job.domain, job.qtype, result)
	case ProtocolDoH:
		return queryDoH(ctx, job.server, job.domain, job.qtype, result)
	default:
		return queryUDP(ctx, job.server, job.domain, job.qtype, result)
	}
}

func calculateBackoff(attempt int) time.Duration {
	const (
		baseDelay   = 100 * time.Millisecond
		maxDelay    = 5 * time.Second
		multiplier  = 2.0
		jitterRatio = 0.1
	)
	
	if attempt <= 0 {
		return 0
	}
	
	// Use bit shifting for efficient power of 2 calculation (2^(attempt-1))
	// This prevents overflow for large attempt values
	var backoff time.Duration
	if attempt > 30 {
		// Prevent overflow: 2^30 * 100ms is already ~100,000 seconds
		backoff = maxDelay
	} else {
		// Calculate 2^(attempt-1) * baseDelay using bit shifting
		multiplierValue := uint(1) << uint(attempt-1)
		backoff = time.Duration(multiplierValue) * baseDelay
	}
	
	// Cap at maximum delay
	if backoff > maxDelay {
		backoff = maxDelay
	}
	
	// Add jitter to prevent thundering herd effect
	// Random jitter: ±10% of the backoff time
	jitter := time.Duration(float64(backoff) * jitterRatio)
	// Simple deterministic jitter based on attempt number
	// This avoids needing to import math/rand
	if attempt%2 == 0 {
		backoff -= jitter / 2
	} else {
		backoff += jitter / 2
	}
	
	return backoff
}