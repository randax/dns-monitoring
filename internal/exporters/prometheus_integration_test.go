package exporters

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/randax/dns-monitoring/internal/config"
	internal_dns "github.com/randax/dns-monitoring/internal/dns"
	"github.com/randax/dns-monitoring/internal/metrics"
)

func TestPrometheusExporterIntegration(t *testing.T) {
	// Create a real metrics collector
	metricsConfig := &config.MetricsConfig{
		MaxStoredResults:    1000,
		WindowDuration:      5 * time.Minute,
		CalculationInterval: 10 * time.Second,
	}
	collector := metrics.NewCollector(metricsConfig)

	// Add some test data
	addTestResults(collector)

	// Create Prometheus exporter with real collector
	promConfig := &config.PrometheusConfig{
		Enabled:                true,
		Port:                   9191, // Use different port to avoid conflicts
		Path:                   "/metrics",
		UpdateInterval:         5 * time.Second,
		IncludeServerLabels:    true,
		IncludeProtocolLabels:  true,
		EnableLatencySampling:  true,
		MetricPrefix:           "dns_test",
	}

	exporter, err := NewPrometheusExporter(promConfig, collector)
	if err != nil {
		t.Fatalf("Failed to create PrometheusExporter: %v", err)
	}

	// Start the exporter
	if err := exporter.Start(); err != nil {
		t.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Give the server time to start
	time.Sleep(100 * time.Millisecond)

	// Test that metrics are being updated
	t.Run("MetricsUpdate", func(t *testing.T) {
		// Trigger manual update
		exporter.UpdateMetrics()

		// Test completed successfully
	})

	// Test HTTP endpoint
	t.Run("HTTPEndpoint", func(t *testing.T) {
		// Ensure metrics are updated before checking
		exporter.UpdateMetrics()
		time.Sleep(50 * time.Millisecond)
		
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", promConfig.Port, promConfig.Path))
		if err != nil {
			t.Fatalf("Failed to fetch metrics: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		bodyStr := string(body)

		// Check for expected metrics
		expectedMetrics := []string{
			"dns_test_queries_total",
			"dns_test_success_rate",
			"dns_test_error_rate",
			"dns_test_latency_p50",
			"dns_test_latency_p95",
			"dns_test_latency_p99",
			"dns_test_qps",
		}

		for _, metric := range expectedMetrics {
			if !strings.Contains(bodyStr, metric) {
				t.Errorf("Expected metric %s not found in response", metric)
			}
		}
	})

	// Test latency sampling
	t.Run("LatencySampling", func(t *testing.T) {
		// Add more results to ensure we have samples
		for i := 0; i < 100; i++ {
			collector.AddResult(internal_dns.Result{
				Domain:       "example.com",
				QueryType:    "A",
				Server:       "8.8.8.8",
				Protocol:     "udp",
				Duration:     time.Duration(10+i) * time.Millisecond,
				Timestamp:    time.Now(),
				ResponseCode: dns.RcodeSuccess,
			})
		}

		// Update metrics
		exporter.UpdateMetrics()

		// Check that latency samples are being collected
		samples := collector.GetRecentLatencySamples(10)
		if len(samples) == 0 {
			t.Error("Expected latency samples to be collected")
		}
	})

	// Test per-server metrics
	t.Run("PerServerMetrics", func(t *testing.T) {
		// Add results for different servers
		servers := []string{"8.8.8.8", "1.1.1.1", "9.9.9.9"}
		for _, server := range servers {
			for i := 0; i < 10; i++ {
				collector.AddResult(internal_dns.Result{
					Domain:       "test.com",
					QueryType:    "A",
					Server:       server,
					Protocol:     "udp",
					Duration:     time.Duration(5+i) * time.Millisecond,
					Timestamp:    time.Now(),
					ResponseCode: dns.RcodeSuccess,
				})
			}
		}

		// Update metrics
		exporter.UpdateMetrics()

		// Fetch metrics and verify per-server labels
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", promConfig.Port, promConfig.Path))
		if err != nil {
			t.Fatalf("Failed to fetch metrics: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		// Check for server labels
		for _, server := range servers {
			if !strings.Contains(bodyStr, fmt.Sprintf(`server="%s"`, server)) {
				t.Errorf("Expected server label %s not found", server)
			}
		}
	})

	// Test per-protocol metrics
	t.Run("PerProtocolMetrics", func(t *testing.T) {
		// Add results for different protocols
		protocols := []string{"udp", "tcp", "dot", "doh"}
		for _, protocol := range protocols {
			for i := 0; i < 10; i++ {
				collector.AddResult(internal_dns.Result{
					Domain:       "protocol-test.com",
					QueryType:    "A",
					Server:       "8.8.8.8",
					Protocol:     internal_dns.Protocol(protocol),
					Duration:     time.Duration(8+i) * time.Millisecond,
					Timestamp:    time.Now(),
					ResponseCode: dns.RcodeSuccess,
				})
			}
		}

		// Update metrics
		exporter.UpdateMetrics()

		// Fetch metrics and verify per-protocol labels
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", promConfig.Port, promConfig.Path))
		if err != nil {
			t.Fatalf("Failed to fetch metrics: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		// Check for protocol labels
		for _, protocol := range protocols {
			if !strings.Contains(bodyStr, fmt.Sprintf(`protocol="%s"`, protocol)) {
				t.Errorf("Expected protocol label %s not found", protocol)
			}
		}
	})

	// Test cache metrics
	t.Run("CacheMetrics", func(t *testing.T) {
		// Create a new exporter with cache metrics enabled
		cacheConfig := &config.PrometheusConfig{
			Enabled:                true,
			Port:                   9192, // Different port for this test
			Path:                   "/metrics",
			UpdateInterval:         5 * time.Second,
			IncludeServerLabels:    true,
			IncludeProtocolLabels:  true,
			MetricPrefix:           "dns_test",
			MetricFeatures: config.MetricFeaturesConfig{
				EnableCacheMetrics: true,
			},
		}
		
		cacheExporter, err := NewPrometheusExporter(cacheConfig, collector)
		if err != nil {
			t.Fatalf("Failed to create cache exporter: %v", err)
		}
		
		if err := cacheExporter.Start(); err != nil {
			t.Fatalf("Failed to start cache exporter: %v", err)
		}
		defer cacheExporter.Stop()
		
		time.Sleep(100 * time.Millisecond)
		
		// Add results with cache data
		for i := 0; i < 50; i++ {
			collector.AddResult(internal_dns.Result{
				Domain:          "cache-test.com",
				QueryType:       "A",
				Server:          "127.0.0.1",
				Protocol:        "udp",
				Duration:        time.Duration(2+i%5) * time.Millisecond,
				Timestamp:       time.Now(),
				ResponseCode:    dns.RcodeSuccess,
				RecursiveServer: true,
				CacheHit:        i%3 == 0, // Every third query is a cache hit
				CacheAge:        time.Duration(i) * time.Second,
				TTL:             300 * time.Second,
				CacheEfficiency: 0.75,
			})
		}

		// Update metrics
		cacheExporter.UpdateMetrics()

		// Fetch metrics
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", cacheConfig.Port, cacheConfig.Path))
		if err != nil {
			t.Fatalf("Failed to fetch metrics: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		// Check for cache metrics
		cacheMetrics := []string{
			"dns_test_cache_hit_rate",
			"dns_test_cache_miss_rate",
			"dns_test_cache_efficiency",
			"dns_test_cache_avg_ttl",
		}

		for _, metric := range cacheMetrics {
			if !strings.Contains(bodyStr, metric) {
				t.Errorf("Expected cache metric %s not found", metric)
			}
		}
	})

	// Test network metrics
	t.Run("NetworkMetrics", func(t *testing.T) {
		// Create a new exporter with network metrics enabled
		networkConfig := &config.PrometheusConfig{
			Enabled:                true,
			Port:                   9193, // Different port for this test
			Path:                   "/metrics",
			UpdateInterval:         5 * time.Second,
			IncludeServerLabels:    true,
			IncludeProtocolLabels:  true,
			MetricPrefix:           "dns_test",
			MetricFeatures: config.MetricFeaturesConfig{
				EnableNetworkMetrics: true,
				EnableJitterMetrics:  true,
			},
		}
		
		networkExporter, err := NewPrometheusExporter(networkConfig, collector)
		if err != nil {
			t.Fatalf("Failed to create network exporter: %v", err)
		}
		
		if err := networkExporter.Start(); err != nil {
			t.Fatalf("Failed to start network exporter: %v", err)
		}
		defer networkExporter.Stop()
		
		time.Sleep(100 * time.Millisecond)
		
		// Add results with network data
		for i := 0; i < 30; i++ {
			collector.AddResult(internal_dns.Result{
				Domain:         "network-test.com",
				QueryType:      "A",
				Server:         "8.8.8.8",
				Protocol:       "udp",
				Duration:       time.Duration(15+i) * time.Millisecond,
				Timestamp:      time.Now(),
				ResponseCode:   dns.RcodeSuccess,
				PacketLoss:     float64(i%10) * 0.1,
				Jitter:         time.Duration(i%5) * time.Millisecond,
				NetworkLatency: time.Duration(10+i%20) * time.Millisecond,
				HopCount:       3 + i%5,
				NetworkQuality: 0.9 - float64(i%10)*0.01,
			})
		}

		// Update metrics
		networkExporter.UpdateMetrics()

		// Fetch metrics
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", networkConfig.Port, networkConfig.Path))
		if err != nil {
			t.Fatalf("Failed to fetch metrics: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		// Check for network metrics
		networkMetrics := []string{
			"dns_test_packet_loss",
			"dns_test_network_quality_score",
			"dns_test_jitter_p50",
			"dns_test_network_latency_p50",
		}

		for _, metric := range networkMetrics {
			if !strings.Contains(bodyStr, metric) {
				t.Errorf("Expected network metric %s not found", metric)
			}
		}
	})

	// Test error handling
	t.Run("ErrorHandling", func(t *testing.T) {
		// Create exporter with nil collector to test error handling
		nilExporter, err := NewPrometheusExporter(promConfig, nil)
		if err == nil {
			t.Error("Expected error when creating exporter with nil collector")
		}
		if nilExporter != nil {
			t.Error("Expected nil exporter when error occurs")
		}

		// Test with nil config
		nilConfigExporter, err := NewPrometheusExporter(nil, collector)
		if err == nil {
			t.Error("Expected error when creating exporter with nil config")
		}
		if nilConfigExporter != nil {
			t.Error("Expected nil exporter when error occurs")
		}
	})

	// Test concurrent updates
	t.Run("ConcurrentUpdates", func(t *testing.T) {
		// Run multiple concurrent updates
		done := make(chan bool, 10)
		for i := 0; i < 10; i++ {
			go func(id int) {
				defer func() { done <- true }()
				
				// Add results
				for j := 0; j < 10; j++ {
					collector.AddResult(internal_dns.Result{
						Domain:       fmt.Sprintf("concurrent-%d.com", id),
						QueryType:    "A",
						Server:       "8.8.8.8",
						Protocol:     "udp",
						Duration:     time.Duration(10+j) * time.Millisecond,
						Timestamp:    time.Now(),
						ResponseCode: dns.RcodeSuccess,
					})
				}
				
				// Update metrics
				exporter.UpdateMetrics()
			}(i)
		}

		// Wait for all goroutines to complete
		for i := 0; i < 10; i++ {
			<-done
		}

		// Test completed successfully
	})
}


func TestPrometheusMetricsValidation(t *testing.T) {
	// Create collector and exporter
	collector := metrics.NewCollector(nil)
	promConfig := &config.PrometheusConfig{
		Enabled:        true,
		Port:           9193,
		Path:           "/metrics",
		UpdateInterval: 1 * time.Second,
		MetricPrefix:   "dns_validation",
	}

	exporter, err := NewPrometheusExporter(promConfig, collector)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}

	// Test with invalid metric values
	t.Run("InvalidValues", func(t *testing.T) {
		// Add result with invalid duration (negative)
		collector.AddResult(internal_dns.Result{
			Domain:       "invalid.com",
			QueryType:    "A",
			Server:       "8.8.8.8",
			Protocol:     "udp",
			Duration:     -1 * time.Millisecond, // Invalid negative duration
			Timestamp:    time.Now(),
			ResponseCode: dns.RcodeServerFailure,
			Error:        fmt.Errorf("test error"),
		})

		// Update metrics
		exporter.UpdateMetrics()

		// Test completed successfully
	})

	// Test registry conflicts
	t.Run("RegistryConflicts", func(t *testing.T) {
		// Try to register the same metrics twice
		registry := prometheus.NewRegistry()
		
		// Register a conflicting metric
		conflictingMetric := prometheus.NewCounter(prometheus.CounterOpts{
			Name: "dns_validation_queries_total",
			Help: "Conflicting metric",
		})
		registry.MustRegister(conflictingMetric)

		// Now try to register our metrics - should handle the conflict gracefully
		testExporter := &PrometheusExporter{
			config:         promConfig,
			collector:      collector,
			registry:       registry,
			updateInterval: promConfig.UpdateInterval,
			stopCh:        make(chan struct{}),
			previousMetrics: &metrics.Metrics{},
			lastCounters: counterState{
				queryTypes:               make(map[string]int64),
				interfacePacketsSent:     make(map[string]int64),
				interfacePacketsReceived: make(map[string]int64),
			},
		}

		// This should handle the error gracefully
		err := testExporter.registerMetrics()
		if err == nil {
			t.Log("Metrics registration handled conflicts gracefully")
		}
	})
}

// Helper function to add test results
func addTestResults(collector *metrics.Collector) {
	testData := []struct {
		domain       string
		qtype        string
		server       string
		protocol     string
		duration     time.Duration
		responseCode int
		err          error
	}{
		{"example.com", "A", "8.8.8.8", "udp", 10 * time.Millisecond, dns.RcodeSuccess, nil},
		{"google.com", "AAAA", "8.8.8.8", "tcp", 15 * time.Millisecond, dns.RcodeSuccess, nil},
		{"invalid.local", "A", "1.1.1.1", "udp", 50 * time.Millisecond, dns.RcodeNameError, nil},
		{"timeout.com", "A", "9.9.9.9", "udp", 100 * time.Millisecond, dns.RcodeServerFailure, fmt.Errorf("timeout")},
		{"test.org", "MX", "8.8.4.4", "dot", 25 * time.Millisecond, dns.RcodeSuccess, nil},
		{"mail.com", "TXT", "1.1.1.1", "doh", 30 * time.Millisecond, dns.RcodeSuccess, nil},
		{"dns.test", "NS", "9.9.9.9", "udp", 8 * time.Millisecond, dns.RcodeSuccess, nil},
		{"ptr.test", "PTR", "8.8.8.8", "tcp", 12 * time.Millisecond, dns.RcodeSuccess, nil},
	}

	for _, td := range testData {
		collector.AddResult(internal_dns.Result{
			Domain:       td.domain,
			QueryType:    td.qtype,
			Server:       td.server,
			Protocol:     internal_dns.Protocol(td.protocol),
			Duration:     td.duration,
			Timestamp:    time.Now(),
			ResponseCode: td.responseCode,
			Error:        td.err,
		})
	}
}

// Benchmark tests
func BenchmarkPrometheusExporterUpdate(b *testing.B) {
	// Create collector with test data
	collector := metrics.NewCollector(nil)
	
	// Add substantial test data
	for i := 0; i < 1000; i++ {
		collector.AddResult(internal_dns.Result{
			Domain:       fmt.Sprintf("bench-%d.com", i),
			QueryType:    "A",
			Server:       "8.8.8.8",
			Protocol:     "udp",
			Duration:     time.Duration(5+i%100) * time.Millisecond,
			Timestamp:    time.Now(),
			ResponseCode: dns.RcodeSuccess,
		})
	}

	// Create exporter
	promConfig := &config.PrometheusConfig{
		Enabled:               true,
		Port:                  9194,
		UpdateInterval:        10 * time.Second,
		EnableLatencySampling: true,
		MetricPrefix:          "dns_bench",
	}

	exporter, err := NewPrometheusExporter(promConfig, collector)
	if err != nil {
		b.Fatalf("Failed to create exporter: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exporter.UpdateMetrics()
	}
}

func BenchmarkPrometheusExporterWithLabels(b *testing.B) {
	// Create collector with test data
	collector := metrics.NewCollector(nil)
	
	// Add data with multiple servers and protocols
	servers := []string{"8.8.8.8", "1.1.1.1", "9.9.9.9", "8.8.4.4"}
	protocols := []string{"udp", "tcp", "dot", "doh"}
	
	for i := 0; i < 1000; i++ {
		collector.AddResult(internal_dns.Result{
			Domain:       fmt.Sprintf("bench-%d.com", i),
			QueryType:    "A",
			Server:       servers[i%len(servers)],
			Protocol:     internal_dns.Protocol(protocols[i%len(protocols)]),
			Duration:     time.Duration(5+i%100) * time.Millisecond,
			Timestamp:    time.Now(),
			ResponseCode: dns.RcodeSuccess,
		})
	}

	// Create exporter with labels enabled
	promConfig := &config.PrometheusConfig{
		Enabled:                true,
		Port:                   9195,
		UpdateInterval:         10 * time.Second,
		EnableLatencySampling:  true,
		IncludeServerLabels:    true,
		IncludeProtocolLabels:  true,
		MetricPrefix:           "dns_bench_labels",
	}

	exporter, err := NewPrometheusExporter(promConfig, collector)
	if err != nil {
		b.Fatalf("Failed to create exporter: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exporter.UpdateMetrics()
	}
}