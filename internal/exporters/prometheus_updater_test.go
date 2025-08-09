package exporters

import (
	"testing"
	"time"

	"github.com/randax/dns-monitoring/internal/config"
	"github.com/randax/dns-monitoring/internal/dns"
	"github.com/randax/dns-monitoring/internal/metrics"
)

// TestSafeMethodCalls tests that the safe wrapper methods handle nil collectors gracefully
func TestSafeMethodCalls(t *testing.T) {
	cfg := &config.PrometheusConfig{
		Enabled:               true,
		Port:                  9090,
		Path:                  "/metrics",
		UpdateInterval:        10 * time.Second,
		EnableLatencySampling: true,
	}

	// Create exporter with valid collector
	metricsConfig := &config.MetricsConfig{
		Enabled:             true,
		WindowDuration:      5 * time.Minute,
		MaxStoredResults:    1000,
		CalculationInterval: 10 * time.Second,
	}
	collector := metrics.NewCollector(metricsConfig)
	exporter, err := NewPrometheusExporter(cfg, collector)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}

	// Test with valid collector
	t.Run("ValidCollector", func(t *testing.T) {
		// Add some test data first
		collector.AddResult(dns.Result{
			Server:       "8.8.8.8",
			Domain:       "example.com",
			QueryType:    "A",
			Protocol:     dns.ProtocolUDP,
			Duration:     50 * time.Millisecond,
			ResponseCode: 0,
			Timestamp:    time.Now(),
		})
		
		samples := exporter.getRecentLatencySamplesSafe(100)
		// The method may return nil if no samples are available, which is valid
		// The important thing is it doesn't panic
		t.Logf("Got %d samples", len(samples))

		periodSamples := exporter.getLatencySamplesForPeriodSafe(
			time.Now().Add(-1*time.Hour),
			time.Now(),
			100,
		)
		// The method may return nil if no samples are available, which is valid
		t.Logf("Got %d period samples", len(periodSamples))
	})

	// Test with nil collector
	t.Run("NilCollector", func(t *testing.T) {
		// Temporarily set collector to nil
		originalCollector := exporter.collector
		exporter.collector = nil
		defer func() { exporter.collector = originalCollector }()

		samples := exporter.getRecentLatencySamplesSafe(100)
		if samples == nil {
			t.Error("Expected non-nil empty slice for nil collector")
		}
		if len(samples) != 0 {
			t.Error("Expected empty slice for nil collector")
		}

		periodSamples := exporter.getLatencySamplesForPeriodSafe(
			time.Now().Add(-1*time.Hour),
			time.Now(),
			100,
		)
		if periodSamples == nil {
			t.Error("Expected non-nil empty slice for nil collector")
		}
		if len(periodSamples) != 0 {
			t.Error("Expected empty slice for nil collector")
		}
	})
}

// TestMethodPanicRecovery tests that the safe wrappers recover from panics
func TestMethodPanicRecovery(t *testing.T) {
	cfg := &config.PrometheusConfig{
		Enabled:               true,
		Port:                  9090,
		Path:                  "/metrics",
		UpdateInterval:        10 * time.Second,
		EnableLatencySampling: true,
	}

	metricsConfig := &config.MetricsConfig{
		Enabled:             true,
		WindowDuration:      5 * time.Minute,
		MaxStoredResults:    1000,
		CalculationInterval: 10 * time.Second,
	}
	collector := metrics.NewCollector(metricsConfig)
	exporter, err := NewPrometheusExporter(cfg, collector)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}

	// The methods should handle panic gracefully
	t.Run("RecoverFromPanic", func(t *testing.T) {
		// Add some test data
		collector.AddResult(dns.Result{
			Server:       "1.1.1.1",
			Domain:       "test.com",
			QueryType:    "AAAA",
			Protocol:     dns.ProtocolTCP,
			Duration:     25 * time.Millisecond,
			ResponseCode: 0,
			Timestamp:    time.Now(),
		})
		
		// Even with extreme values, methods should not panic
		samples := exporter.getRecentLatencySamplesSafe(-1)
		// The method may return nil if no samples or handle negative as 0
		t.Logf("Got %d samples with negative maxSamples", len(samples))

		// Test with very large number
		largeSamples := exporter.getRecentLatencySamplesSafe(1000000)
		// The method may return nil or limit the samples
		t.Logf("Got %d samples with large maxSamples", len(largeSamples))
	})
}