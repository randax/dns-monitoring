package metrics

import (
	"time"
	"github.com/randax/dns-monitoring/internal/config"
)

// MetricsProvider defines the interface for providing metrics data
type MetricsProvider interface {
	// GetMetrics returns the current metrics
	GetMetrics() *Metrics
	
	// GetMetricsForPeriod returns metrics for a specific time period
	GetMetricsForPeriod(start, end time.Time) *Metrics
	
	// GetRecentLatencySamples returns recent latency samples for histogram/summary updates
	GetRecentLatencySamples(maxSamples int) []float64
	
	// GetLatencySamplesForPeriod returns latency samples within a time period
	GetLatencySamplesForPeriod(start, end time.Time, maxSamples int) []float64
	
	// GetSamplingMetrics returns adaptive sampling metrics if available
	GetSamplingMetrics() *SamplingMetrics
	
	// GetResultCount returns the number of stored results
	GetResultCount() int
	
	// GetConfig returns the current metrics configuration
	GetConfig() *config.MetricsConfig
}

// Ensure Collector implements MetricsProvider
var _ MetricsProvider = (*Collector)(nil)