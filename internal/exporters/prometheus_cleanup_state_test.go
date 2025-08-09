package exporters

import (
	"fmt"
	"testing"
	"time"

	"github.com/randax/dns-monitoring/internal/config"
	"github.com/randax/dns-monitoring/internal/metrics"
)

func TestPrometheusCounterStateCleanup(t *testing.T) {
	tests := []struct {
		name                string
		cleanupInterval     time.Duration
		maxAge              time.Duration
		expectCleanupStart  bool
	}{
		{
			name:                "DefaultConfiguration",
			cleanupInterval:     time.Hour,
			maxAge:              2 * time.Hour,
			expectCleanupStart:  true,
		},
		{
			name:                "CleanupDisabled",
			cleanupInterval:     0,
			maxAge:              2 * time.Hour,
			expectCleanupStart:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test configuration
			cfg := &config.PrometheusConfig{
				Enabled:                     true,
				Port:                        0, // Use random port
				Path:                        "/metrics",
				UpdateInterval:              100 * time.Millisecond,
				CounterStateCleanupInterval: tt.cleanupInterval,
				CounterStateMaxAge:          tt.maxAge,
			}

			// Create mock collector
			collector := &mockMetricsCollector{
				metrics: &metrics.Metrics{
					Latency: metrics.LatencyMetrics{
						P50: 10.5,
						P95: 50.2,
						P99: 100.1,
					},
					Rates: metrics.RateMetrics{
						TotalQueries:   1000,
						SuccessQueries: 950,
						ErrorQueries:   50,
					},
					QueryType: metrics.QueryTypeDistribution{
						A:    500,
						AAAA: 300,
						MX:   100,
						TXT:  50,
					},
				},
			}

			// Create exporter
			exporter, err := NewPrometheusExporter(cfg, collector)
			if err != nil {
				t.Fatalf("Failed to create exporter: %v", err)
			}

			// Start the exporter
			err = exporter.Start()
			if err != nil {
				t.Fatalf("Failed to start exporter: %v", err)
			}
			defer exporter.Stop()

			// Wait a moment to ensure initialization is complete
			time.Sleep(50 * time.Millisecond)
			
			// Add test data to counter state with clear timing
			now := time.Now()
			exporter.mu.Lock()
			// Clear any existing entries and add test entries
			exporter.lastCounters.queryTypes = make(map[string]int64)
			exporter.lastCounters.queryTypesLastUpdate = make(map[string]time.Time)
			
			exporter.lastCounters.queryTypes["TEST_OLD"] = 100
			exporter.lastCounters.queryTypesLastUpdate["TEST_OLD"] = now.Add(-3 * time.Hour)
			exporter.lastCounters.queryTypes["TEST_NEW"] = 200
			exporter.lastCounters.queryTypesLastUpdate["TEST_NEW"] = now
			exporter.mu.Unlock()

			if tt.expectCleanupStart && tt.cleanupInterval < time.Second {
				// For short intervals, wait for cleanup to run multiple times
				time.Sleep(tt.cleanupInterval * 3)

				// Check if old entries were cleaned up
				exporter.mu.Lock()
				_, oldExists := exporter.lastCounters.queryTypes["TEST_OLD"]
				newValue, newExists := exporter.lastCounters.queryTypes["TEST_NEW"]
				queryTypesCount := len(exporter.lastCounters.queryTypes)
				exporter.mu.Unlock()

				if tt.maxAge < time.Hour {
					// With short max age, old entry should be removed
					if oldExists {
						t.Errorf("Expected TEST_OLD to be cleaned up, but it still exists")
					}
				}
				
				// New entry should always remain with correct value
				if !newExists {
					t.Errorf("Expected TEST_NEW to remain, but it was removed (total entries: %d)", queryTypesCount)
				} else if newValue != 200 {
					t.Errorf("Expected TEST_NEW value to be 200, but got %d", newValue)
				}
			}
		})
	}
}

func TestPrometheusCounterStateMapSizeMonitoring(t *testing.T) {
	// Create test configuration with short cleanup intervals for testing
	cfg := &config.PrometheusConfig{
		Enabled:                     true,
		Port:                        0, // Use random port
		Path:                        "/metrics",
		UpdateInterval:              100 * time.Millisecond,
		CounterStateCleanupInterval: 100 * time.Millisecond,
		CounterStateMaxAge:          200 * time.Millisecond,
	}

	// Create mock collector
	collector := &mockMetricsCollector{
		metrics: &metrics.Metrics{
			QueryType: metrics.QueryTypeDistribution{},
			Network: metrics.NetworkMetrics{
				InterfaceStats: make(map[string]metrics.NetworkInterfaceStats),
			},
		},
	}

	// Create exporter
	exporter, err := NewPrometheusExporter(cfg, collector)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}

	// Add many entries to trigger size warnings
	exporter.mu.Lock()
	now := time.Now()
	
	// Add many query type entries (should trigger warning at > 1000)
	for i := 0; i < 100; i++ {
		qtype := fmt.Sprintf("QTYPE_%d", i)
		exporter.lastCounters.queryTypes[qtype] = int64(i)
		// Half are old, half are new
		if i < 50 {
			exporter.lastCounters.queryTypesLastUpdate[qtype] = now.Add(-time.Hour)
		} else {
			exporter.lastCounters.queryTypesLastUpdate[qtype] = now
		}
	}
	
	// Add interface entries (should trigger warning at > 100)
	for i := 0; i < 20; i++ {
		iface := fmt.Sprintf("eth%d", i)
		exporter.lastCounters.interfacePacketsSent[iface] = int64(i * 1000)
		exporter.lastCounters.interfacePacketsReceived[iface] = int64(i * 2000)
		// Half are old, half are new
		if i < 10 {
			exporter.lastCounters.interfaceLastUpdate[iface] = now.Add(-time.Hour)
		} else {
			exporter.lastCounters.interfaceLastUpdate[iface] = now
		}
	}
	exporter.mu.Unlock()

	// Manually trigger cleanup to test monitoring
	exporter.cleanupCounterState(200 * time.Millisecond)

	// Verify that old entries were removed
	exporter.mu.Lock()
	queryTypesCount := len(exporter.lastCounters.queryTypes)
	interfacesCount := len(exporter.lastCounters.interfacePacketsSent)
	exporter.mu.Unlock()

	// Should have removed old entries (those older than 200ms)
	if queryTypesCount >= 100 {
		t.Errorf("Expected some query types to be cleaned up, but have %d", queryTypesCount)
	}
	if interfacesCount >= 20 {
		t.Errorf("Expected some interfaces to be cleaned up, but have %d", interfacesCount)
	}

	// Stop the exporter
	exporter.Stop()
}

// mockMetricsCollector implements MetricsProvider for testing
type mockMetricsCollector struct {
	metrics *metrics.Metrics
	config  *config.MetricsConfig
}

func (m *mockMetricsCollector) GetMetrics() *metrics.Metrics {
	return m.metrics
}

func (m *mockMetricsCollector) GetMetricsForPeriod(start, end time.Time) *metrics.Metrics {
	return m.metrics
}

func (m *mockMetricsCollector) GetRecentLatencySamples(maxSamples int) []float64 {
	// Return empty slice - this mock doesn't support sampling
	return []float64{}
}

func (m *mockMetricsCollector) GetLatencySamplesForPeriod(start, end time.Time, maxSamples int) []float64 {
	// Return empty slice - this mock doesn't support sampling
	return []float64{}
}

func (m *mockMetricsCollector) GetSamplingMetrics() *metrics.SamplingMetrics {
	return nil
}

func (m *mockMetricsCollector) GetResultCount() int {
	return 0
}

func (m *mockMetricsCollector) GetConfig() *config.MetricsConfig {
	if m.config == nil {
		return config.DefaultMetricsConfig()
	}
	return m.config
}