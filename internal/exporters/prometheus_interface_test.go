package exporters

import (
	"testing"
	"time"

	"github.com/randax/dns-monitoring/internal/config"
	"github.com/randax/dns-monitoring/internal/metrics"
)

// MockMinimalCollector implements MetricsProvider but panics on extended methods
// simulating a collector that doesn't truly support latency sampling
type MockMinimalCollector struct {
	metrics *metrics.Metrics
}

func (m *MockMinimalCollector) GetMetrics() *metrics.Metrics {
	if m.metrics == nil {
		return &metrics.Metrics{
			Latency: metrics.LatencyMetrics{
				P50:  10.5,
				P95:  25.0,
				P99:  50.0,
				P999: 100.0,
			},
		}
	}
	return m.metrics
}

func (m *MockMinimalCollector) GetMetricsForPeriod(start, end time.Time) *metrics.Metrics {
	return m.GetMetrics()
}

func (m *MockMinimalCollector) GetRecentLatencySamples(maxSamples int) []float64 {
	panic("GetRecentLatencySamples not implemented")
}

func (m *MockMinimalCollector) GetLatencySamplesForPeriod(start, end time.Time, maxSamples int) []float64 {
	panic("GetLatencySamplesForPeriod not implemented")
}

func (m *MockMinimalCollector) GetSamplingMetrics() *metrics.SamplingMetrics {
	return nil
}

func (m *MockMinimalCollector) GetResultCount() int {
	return 0
}

func (m *MockMinimalCollector) GetConfig() *config.MetricsConfig {
	return &config.MetricsConfig{}
}

// MockExtendedCollector implements the full MetricsProvider interface
// including the extended latency sampling methods
type MockExtendedCollector struct {
	MockMinimalCollector
	samples []float64
}

func (m *MockExtendedCollector) GetRecentLatencySamples(maxSamples int) []float64 {
	if m.samples == nil {
		// Return some sample data in seconds
		return []float64{0.010, 0.025, 0.050, 0.100}
	}
	if maxSamples > len(m.samples) {
		return m.samples
	}
	return m.samples[:maxSamples]
}

func (m *MockExtendedCollector) GetLatencySamplesForPeriod(start, end time.Time, maxSamples int) []float64 {
	return m.GetRecentLatencySamples(maxSamples)
}

func TestPrometheusExporterInterfaceValidation(t *testing.T) {
	tests := []struct {
		name                    string
		collector              metrics.MetricsProvider
		enableLatencySampling  bool
		expectSupportsExtended bool
		expectMetricsCreated   bool
	}{
		{
			name:                    "minimal collector with sampling disabled",
			collector:              &MockMinimalCollector{},
			enableLatencySampling:  false,
			expectSupportsExtended: false,
			expectMetricsCreated:   false,
		},
		{
			name:                    "minimal collector with sampling enabled",
			collector:              &MockMinimalCollector{},
			enableLatencySampling:  true,
			expectSupportsExtended: false,
			expectMetricsCreated:   false, // Should not create histogram/summary
		},
		{
			name:                    "extended collector with sampling disabled",
			collector:              &MockExtendedCollector{},
			enableLatencySampling:  false,
			expectSupportsExtended: true,
			expectMetricsCreated:   false, // Disabled by config
		},
		{
			name:                    "extended collector with sampling enabled",
			collector:              &MockExtendedCollector{},
			enableLatencySampling:  true,
			expectSupportsExtended: true,
			expectMetricsCreated:   true, // Should create histogram/summary
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.PrometheusConfig{
				Port:                  9090,
				UpdateInterval:        time.Second,
				MetricPrefix:         "test",
				EnableLatencySampling: tt.enableLatencySampling,
			}

			exporter, err := NewPrometheusExporter(cfg, tt.collector)
			if err != nil {
				t.Fatalf("Failed to create exporter: %v", err)
			}
			defer exporter.Stop()

			// Check if the exporter correctly detected the interface capabilities
			if exporter.supportsLatencySampling != tt.expectSupportsExtended {
				t.Errorf("Expected supportsLatencySampling=%v, got %v",
					tt.expectSupportsExtended, exporter.supportsLatencySampling)
			}

			// Check if histogram/summary metrics were created
			hasHistogram := exporter.latencyHistogram != nil
			hasSummary := exporter.latencySummary != nil
			metricsCreated := hasHistogram && hasSummary

			if metricsCreated != tt.expectMetricsCreated {
				t.Errorf("Expected metrics created=%v, got histogram=%v summary=%v",
					tt.expectMetricsCreated, hasHistogram, hasSummary)
			}

			// Test that safe methods don't panic
			samples := exporter.getRecentLatencySamplesSafe(10)
			if tt.expectSupportsExtended && tt.enableLatencySampling {
				// Should return samples
				if len(samples) == 0 {
					t.Error("Expected samples from extended collector, got none")
				}
			} else {
				// Should return empty slice
				if len(samples) != 0 {
					t.Errorf("Expected no samples, got %d", len(samples))
				}
			}

			// Test period samples
			now := time.Now()
			periodSamples := exporter.getLatencySamplesForPeriodSafe(
				now.Add(-time.Hour), now, 10)
			if tt.expectSupportsExtended && tt.enableLatencySampling {
				// Should return samples
				if len(periodSamples) == 0 {
					t.Error("Expected period samples from extended collector, got none")
				}
			} else {
				// Should return empty slice
				if len(periodSamples) != 0 {
					t.Errorf("Expected no period samples, got %d", len(periodSamples))
				}
			}
		})
	}
}

func TestPrometheusExporterGracefulDegradation(t *testing.T) {
	// Test that the exporter works correctly even when collector
	// doesn't support extended methods
	cfg := &config.PrometheusConfig{
		Port:                  9091,
		UpdateInterval:        time.Second,
		MetricPrefix:         "test",
		EnableLatencySampling: true, // Enabled but not supported
	}

	collector := &MockMinimalCollector{
		metrics: &metrics.Metrics{
			Latency: metrics.LatencyMetrics{
				P50:  10.5,
				P95:  25.0,
				P99:  50.0,
				P999: 100.0,
			},
			Rates: metrics.RateMetrics{
				TotalQueries:   1000,
				SuccessQueries: 950,
				ErrorQueries:   50,
				SuccessRate:    95.0,
				ErrorRate:      5.0,
			},
			Throughput: metrics.ThroughputMetrics{
				CurrentQPS: 100.0,
				AverageQPS: 95.0,
				PeakQPS:    150.0,
			},
		},
	}

	exporter, err := NewPrometheusExporter(cfg, collector)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}
	defer exporter.Stop()

	// Verify that the exporter detected the lack of support
	if exporter.supportsLatencySampling {
		t.Error("Expected supportsLatencySampling=false for minimal collector")
	}

	// Verify histogram/summary were not created
	if exporter.latencyHistogram != nil {
		t.Error("Expected no histogram for unsupported collector")
	}
	if exporter.latencySummary != nil {
		t.Error("Expected no summary for unsupported collector")
	}

	// Test that UpdateMetrics doesn't panic
	exporter.UpdateMetrics()

	// Verify that percentile gauges still work
	if exporter.gauges["latency_p50"] == nil {
		t.Error("Expected latency_p50 gauge to exist")
	}
	if exporter.gauges["latency_p95"] == nil {
		t.Error("Expected latency_p95 gauge to exist")
	}
	if exporter.gauges["latency_p99"] == nil {
		t.Error("Expected latency_p99 gauge to exist")
	}
	if exporter.gauges["latency_p999"] == nil {
		t.Error("Expected latency_p999 gauge to exist")
	}
}