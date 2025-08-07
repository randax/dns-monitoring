package metrics

import (
	"sync"
	"time"

	"github.com/randax/dns-monitoring/internal/config"
	"github.com/randax/dns-monitoring/internal/dns"
)

type Collector struct {
	mu               sync.RWMutex
	results          []dns.Result
	config           *config.MetricsConfig
	windowStart      time.Time
	lastCleanup      time.Time
}

func NewCollector(cfg *config.MetricsConfig) *Collector {
	if cfg == nil {
		cfg = config.DefaultMetricsConfig()
	}
	
	return &Collector{
		results:     make([]dns.Result, 0, cfg.MaxStoredResults),
		config:      cfg,
		windowStart: time.Now(),
		lastCleanup: time.Now(),
	}
}

func (c *Collector) AddResult(result dns.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.results = append(c.results, result)
	
	if time.Since(c.lastCleanup) > c.config.CalculationInterval {
		c.cleanupOldData()
	}
	
	if len(c.results) > c.config.MaxStoredResults {
		c.results = c.results[len(c.results)-c.config.MaxStoredResults:]
	}
}

func (c *Collector) GetMetrics() *Metrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	if len(c.results) == 0 {
		return &Metrics{
			Period: MetricsPeriod{
				Start: c.windowStart,
				End:   time.Now(),
			},
		}
	}
	
	now := time.Now()
	windowStart := now.Add(-c.config.WindowDuration)
	
	var filteredResults []dns.Result
	for _, r := range c.results {
		if r.Timestamp.After(windowStart) {
			filteredResults = append(filteredResults, r)
		}
	}
	
	if len(filteredResults) == 0 {
		return &Metrics{
			Period: MetricsPeriod{
				Start: windowStart,
				End:   now,
			},
		}
	}
	
	return c.calculateMetrics(filteredResults, windowStart, now)
}

func (c *Collector) GetMetricsForPeriod(start, end time.Time) *Metrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	var filteredResults []dns.Result
	for _, r := range c.results {
		if r.Timestamp.After(start) && r.Timestamp.Before(end) {
			filteredResults = append(filteredResults, r)
		}
	}
	
	if len(filteredResults) == 0 {
		return &Metrics{
			Period: MetricsPeriod{
				Start: start,
				End:   end,
			},
		}
	}
	
	return c.calculateMetrics(filteredResults, start, end)
}

func (c *Collector) calculateMetrics(results []dns.Result, start, end time.Time) *Metrics {
	metrics := &Metrics{
		Period: MetricsPeriod{
			Start: start,
			End:   end,
		},
	}
	
	if len(results) == 0 {
		return metrics
	}
	
	metrics.Latency = calculateLatencyMetrics(results)
	metrics.Rates = calculateRateMetrics(results)
	metrics.ResponseCode = calculateResponseCodeDistribution(results)
	metrics.Throughput = calculateThroughputMetrics(results, start, end)
	metrics.QueryType = calculateQueryTypeDistribution(results)
	
	return metrics
}

func (c *Collector) cleanupOldData() {
	cutoff := time.Now().Add(-c.config.WindowDuration * 2)
	
	newResults := make([]dns.Result, 0, len(c.results))
	for _, r := range c.results {
		if r.Timestamp.After(cutoff) {
			newResults = append(newResults, r)
		}
	}
	
	c.results = newResults
	c.lastCleanup = time.Now()
}

func (c *Collector) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.results = c.results[:0]
	c.windowStart = time.Now()
	c.lastCleanup = time.Now()
}

func (c *Collector) GetResultCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return len(c.results)
}

func (c *Collector) GetConfig() *config.MetricsConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return c.config
}

func (c *Collector) UpdateConfig(cfg *config.MetricsConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.config = cfg
}