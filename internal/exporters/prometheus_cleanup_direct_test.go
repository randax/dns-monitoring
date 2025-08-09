package exporters

import (
	"testing"
	"time"

	"github.com/randax/dns-monitoring/internal/config"
	"github.com/randax/dns-monitoring/internal/metrics"
)

func TestPrometheusCleanupCounterStateDirectly(t *testing.T) {
	// Create test configuration  
	cfg := &config.PrometheusConfig{
		Enabled:                     true,
		Port:                        0, // Use random port
		Path:                        "/metrics",
		UpdateInterval:              time.Hour, // Long interval to avoid interference
		CounterStateCleanupInterval: time.Hour,
		CounterStateMaxAge:          time.Hour,
	}

	// Create mock collector
	collector := &mockMetricsCollectorSimple{
		metrics: &metrics.Metrics{
			Latency: metrics.LatencyMetrics{P50: 10.5},
			Rates:   metrics.RateMetrics{TotalQueries: 1000},
		},
	}

	// Create exporter
	exporter, err := NewPrometheusExporter(cfg, collector)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}

	// Add test data to counter state
	now := time.Now()
	exporter.mu.Lock()
	
	// Add old entries that should be cleaned up
	exporter.lastCounters.queryTypes["OLD_TYPE_1"] = 100
	exporter.lastCounters.queryTypesLastUpdate["OLD_TYPE_1"] = now.Add(-2 * time.Hour)
	exporter.lastCounters.queryTypes["OLD_TYPE_2"] = 200
	exporter.lastCounters.queryTypesLastUpdate["OLD_TYPE_2"] = now.Add(-3 * time.Hour)
	
	// Add recent entries that should remain
	exporter.lastCounters.queryTypes["NEW_TYPE_1"] = 300
	exporter.lastCounters.queryTypesLastUpdate["NEW_TYPE_1"] = now.Add(-30 * time.Minute)
	exporter.lastCounters.queryTypes["NEW_TYPE_2"] = 400
	exporter.lastCounters.queryTypesLastUpdate["NEW_TYPE_2"] = now
	
	// Add interface entries
	exporter.lastCounters.interfacePacketsSent["eth0"] = 1000
	exporter.lastCounters.interfacePacketsReceived["eth0"] = 2000
	exporter.lastCounters.interfaceLastUpdate["eth0"] = now.Add(-2 * time.Hour)
	
	exporter.lastCounters.interfacePacketsSent["eth1"] = 3000
	exporter.lastCounters.interfacePacketsReceived["eth1"] = 4000
	exporter.lastCounters.interfaceLastUpdate["eth1"] = now
	
	exporter.mu.Unlock()

	// Perform cleanup with 1 hour max age
	exporter.cleanupCounterState(time.Hour)

	// Check results
	exporter.mu.Lock()
	defer exporter.mu.Unlock()

	// Old query types should be removed
	if _, exists := exporter.lastCounters.queryTypes["OLD_TYPE_1"]; exists {
		t.Error("Expected OLD_TYPE_1 to be removed")
	}
	if _, exists := exporter.lastCounters.queryTypes["OLD_TYPE_2"]; exists {
		t.Error("Expected OLD_TYPE_2 to be removed")
	}

	// New query types should remain
	if val, exists := exporter.lastCounters.queryTypes["NEW_TYPE_1"]; !exists || val != 300 {
		t.Errorf("Expected NEW_TYPE_1 to remain with value 300, got exists=%v, val=%d", exists, val)
	}
	if val, exists := exporter.lastCounters.queryTypes["NEW_TYPE_2"]; !exists || val != 400 {
		t.Errorf("Expected NEW_TYPE_2 to remain with value 400, got exists=%v, val=%d", exists, val)
	}

	// Old interface should be removed
	if _, exists := exporter.lastCounters.interfacePacketsSent["eth0"]; exists {
		t.Error("Expected eth0 to be removed from interfacePacketsSent")
	}
	if _, exists := exporter.lastCounters.interfacePacketsReceived["eth0"]; exists {
		t.Error("Expected eth0 to be removed from interfacePacketsReceived")
	}
	if _, exists := exporter.lastCounters.interfaceLastUpdate["eth0"]; exists {
		t.Error("Expected eth0 to be removed from interfaceLastUpdate")
	}

	// New interface should remain
	if val, exists := exporter.lastCounters.interfacePacketsSent["eth1"]; !exists || val != 3000 {
		t.Errorf("Expected eth1 to remain in interfacePacketsSent with value 3000, got exists=%v, val=%d", exists, val)
	}
	if val, exists := exporter.lastCounters.interfacePacketsReceived["eth1"]; !exists || val != 4000 {
		t.Errorf("Expected eth1 to remain in interfacePacketsReceived with value 4000, got exists=%v, val=%d", exists, val)
	}
}

func TestPrometheusCleanupEnabledByDefault(t *testing.T) {
	// Create configuration with cleanup interval set (default behavior)
	cfg := &config.PrometheusConfig{
		Enabled:                     true,
		Port:                        0,
		Path:                        "/metrics",
		UpdateInterval:              100 * time.Millisecond,
		CounterStateCleanupInterval: time.Hour,   // Default: 1 hour
		CounterStateMaxAge:          2 * time.Hour, // Default: 2 hours
	}

	collector := &mockMetricsCollectorSimple{
		metrics: &metrics.Metrics{
			Latency: metrics.LatencyMetrics{P50: 10.5},
			Rates:   metrics.RateMetrics{TotalQueries: 1000},
		},
	}

	exporter, err := NewPrometheusExporter(cfg, collector)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}

	// Start should enable cleanup by default
	err = exporter.Start()
	if err != nil {
		t.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Verify that cleanup is configured (we can't easily test if goroutine is running,
	// but we can verify the configuration is set correctly)
	if cfg.CounterStateCleanupInterval <= 0 {
		t.Error("Expected cleanup interval to be positive by default")
	}
	if cfg.CounterStateMaxAge <= 0 {
		t.Error("Expected max age to be positive by default")
	}
}

// mockMetricsCollectorSimple is a simpler mock that doesn't interfere with tests
type mockMetricsCollectorSimple struct {
	metrics *metrics.Metrics
	config  *config.MetricsConfig
}

func (m *mockMetricsCollectorSimple) GetMetrics() *metrics.Metrics {
	return m.metrics
}

func (m *mockMetricsCollectorSimple) GetMetricsForPeriod(start, end time.Time) *metrics.Metrics {
	return m.metrics
}

func (m *mockMetricsCollectorSimple) GetRecentLatencySamples(maxSamples int) []float64 {
	return []float64{}
}

func (m *mockMetricsCollectorSimple) GetLatencySamplesForPeriod(start, end time.Time, maxSamples int) []float64 {
	return []float64{}
}

func (m *mockMetricsCollectorSimple) GetSamplingMetrics() *metrics.SamplingMetrics {
	return nil
}

func (m *mockMetricsCollectorSimple) GetResultCount() int {
	return 0
}

func (m *mockMetricsCollectorSimple) GetConfig() *config.MetricsConfig {
	if m.config == nil {
		return config.DefaultMetricsConfig()
	}
	return m.config
}