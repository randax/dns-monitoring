package exporters

import (
	"testing"
	"time"

	"github.com/randax/dns-monitoring/internal/config"
	"github.com/randax/dns-monitoring/internal/metrics"
)

func TestCounterStateCleanup(t *testing.T) {
	// Create a metrics collector
	metricsConfig := &config.MetricsConfig{
		MaxStoredResults:    100,
		WindowDuration:      5 * time.Minute,
		CalculationInterval: 10 * time.Second,
	}
	collector := metrics.NewCollector(metricsConfig)

	// Create Prometheus exporter with cleanup configuration
	promConfig := &config.PrometheusConfig{
		Enabled:                     true,
		Port:                        0, // Use port 0 to get a random available port
		Path:                        "/metrics",
		UpdateInterval:              1 * time.Second,
		IncludeServerLabels:         true,
		IncludeProtocolLabels:       true,
		CounterStateCleanupInterval: 100 * time.Millisecond, // Short interval for testing
		CounterStateMaxAge:          200 * time.Millisecond, // Short max age for testing
	}

	exporter, err := NewPrometheusExporter(promConfig, collector)
	if err != nil {
		t.Fatalf("Failed to create PrometheusExporter: %v", err)
	}

	// Manually add some counter state entries
	now := time.Now()
	oldTime := now.Add(-1 * time.Hour) // Old entries that should be cleaned up

	// Add query types with different timestamps
	exporter.lastCounters.queryTypes["A"] = 100
	exporter.lastCounters.queryTypesLastUpdate["A"] = now // Recent, should be kept

	exporter.lastCounters.queryTypes["AAAA"] = 200
	exporter.lastCounters.queryTypesLastUpdate["AAAA"] = oldTime // Old, should be removed

	exporter.lastCounters.queryTypes["MX"] = 300
	exporter.lastCounters.queryTypesLastUpdate["MX"] = oldTime // Old, should be removed

	// Add interface counters with different timestamps
	exporter.lastCounters.interfacePacketsSent["eth0"] = 1000
	exporter.lastCounters.interfacePacketsReceived["eth0"] = 2000
	exporter.lastCounters.interfaceLastUpdate["eth0"] = now // Recent, should be kept

	exporter.lastCounters.interfacePacketsSent["eth1"] = 3000
	exporter.lastCounters.interfacePacketsReceived["eth1"] = 4000
	exporter.lastCounters.interfaceLastUpdate["eth1"] = oldTime // Old, should be removed

	// Run cleanup with the configured max age
	exporter.cleanupCounterState(promConfig.CounterStateMaxAge)

	// Verify that old entries were removed
	if _, exists := exporter.lastCounters.queryTypes["AAAA"]; exists {
		t.Error("Old query type AAAA should have been removed")
	}
	if _, exists := exporter.lastCounters.queryTypesLastUpdate["AAAA"]; exists {
		t.Error("Old query type AAAA timestamp should have been removed")
	}
	if _, exists := exporter.lastCounters.queryTypes["MX"]; exists {
		t.Error("Old query type MX should have been removed")
	}

	// Verify that recent entries were kept
	if _, exists := exporter.lastCounters.queryTypes["A"]; !exists {
		t.Error("Recent query type A should have been kept")
	}
	if val := exporter.lastCounters.queryTypes["A"]; val != 100 {
		t.Errorf("Query type A value should be 100, got %d", val)
	}

	// Verify interface cleanup
	if _, exists := exporter.lastCounters.interfacePacketsSent["eth1"]; exists {
		t.Error("Old interface eth1 packets sent should have been removed")
	}
	if _, exists := exporter.lastCounters.interfacePacketsReceived["eth1"]; exists {
		t.Error("Old interface eth1 packets received should have been removed")
	}
	if _, exists := exporter.lastCounters.interfaceLastUpdate["eth1"]; exists {
		t.Error("Old interface eth1 timestamp should have been removed")
	}

	// Verify that recent interface entries were kept
	if _, exists := exporter.lastCounters.interfacePacketsSent["eth0"]; !exists {
		t.Error("Recent interface eth0 packets sent should have been kept")
	}
	if val := exporter.lastCounters.interfacePacketsSent["eth0"]; val != 1000 {
		t.Errorf("Interface eth0 packets sent should be 1000, got %d", val)
	}
}

func TestCounterStateCleanupLoop(t *testing.T) {
	// Create a metrics collector
	metricsConfig := &config.MetricsConfig{
		MaxStoredResults:    100,
		WindowDuration:      5 * time.Minute,
		CalculationInterval: 10 * time.Second,
	}
	collector := metrics.NewCollector(metricsConfig)

	// Create Prometheus exporter with short cleanup intervals for testing
	promConfig := &config.PrometheusConfig{
		Enabled:                     true,
		Port:                        0, // Use port 0 to get a random available port
		Path:                        "/metrics",
		UpdateInterval:              1 * time.Second,
		CounterStateCleanupInterval: 100 * time.Millisecond,
		CounterStateMaxAge:          50 * time.Millisecond,
	}

	exporter, err := NewPrometheusExporter(promConfig, collector)
	if err != nil {
		t.Fatalf("Failed to create PrometheusExporter: %v", err)
	}

	// Start the exporter (which should start the cleanup loop)
	if err := exporter.Start(); err != nil {
		t.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Add some old entries
	oldTime := time.Now().Add(-1 * time.Hour)
	exporter.mu.Lock()
	exporter.lastCounters.queryTypes["OLD_TYPE"] = 999
	exporter.lastCounters.queryTypesLastUpdate["OLD_TYPE"] = oldTime
	exporter.mu.Unlock()

	// Wait for cleanup to run (should happen within 100ms + some buffer)
	time.Sleep(200 * time.Millisecond)

	// Verify that old entries were cleaned up by the background loop
	exporter.mu.RLock()
	_, exists := exporter.lastCounters.queryTypes["OLD_TYPE"]
	exporter.mu.RUnlock()

	if exists {
		t.Error("Old query type should have been cleaned up by background loop")
	}
}

func TestCounterStateNoCleanupWhenDisabled(t *testing.T) {
	// Create a metrics collector
	metricsConfig := &config.MetricsConfig{
		MaxStoredResults:    100,
		WindowDuration:      5 * time.Minute,
		CalculationInterval: 10 * time.Second,
	}
	collector := metrics.NewCollector(metricsConfig)

	// Create Prometheus exporter with cleanup disabled (interval = 0)
	promConfig := &config.PrometheusConfig{
		Enabled:                     true,
		Port:                        0,
		Path:                        "/metrics",
		UpdateInterval:              1 * time.Second,
		CounterStateCleanupInterval: 0, // Disabled
		CounterStateMaxAge:          0, // Disabled
	}

	exporter, err := NewPrometheusExporter(promConfig, collector)
	if err != nil {
		t.Fatalf("Failed to create PrometheusExporter: %v", err)
	}

	// Start the exporter (cleanup loop should NOT start)
	if err := exporter.Start(); err != nil {
		t.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Add some old entries
	oldTime := time.Now().Add(-1 * time.Hour)
	exporter.mu.Lock()
	exporter.lastCounters.queryTypes["OLD_TYPE"] = 999
	exporter.lastCounters.queryTypesLastUpdate["OLD_TYPE"] = oldTime
	exporter.mu.Unlock()

	// Wait a bit to ensure no cleanup happens
	time.Sleep(200 * time.Millisecond)

	// Verify that old entries were NOT cleaned up (since cleanup is disabled)
	exporter.mu.RLock()
	val, exists := exporter.lastCounters.queryTypes["OLD_TYPE"]
	exporter.mu.RUnlock()

	if !exists {
		t.Error("Query type should NOT have been cleaned up when cleanup is disabled")
	}
	if val != 999 {
		t.Errorf("Query type value should be 999, got %d", val)
	}
}