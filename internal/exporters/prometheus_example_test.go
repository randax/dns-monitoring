package exporters_test

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/miekg/dns"
	"github.com/randax/dns-monitoring/internal/config"
	internal_dns "github.com/randax/dns-monitoring/internal/dns"
	"github.com/randax/dns-monitoring/internal/exporters"
	"github.com/randax/dns-monitoring/internal/metrics"
)

// Example of basic Prometheus exporter setup and usage
func ExamplePrometheusExporter_basic() {
	// Create metrics configuration
	metricsConfig := &config.MetricsConfig{
		Enabled:             true,
		WindowDuration:      15 * time.Minute,
		MaxStoredResults:    100000,
		CalculationInterval: 10 * time.Second,
	}

	// Create metrics collector
	collector := metrics.NewCollector(metricsConfig)

	// Configure Prometheus exporter
	promConfig := &config.PrometheusConfig{
		Enabled:               true,
		Port:                  9090,
		Path:                  "/metrics",
		UpdateInterval:        30 * time.Second,
		IncludeServerLabels:   true,
		IncludeProtocolLabels: true,
		MetricPrefix:          "dns",
		EnableLatencySampling: false, // Use percentile gauges only
	}

	// Create and start the exporter
	exporter, err := exporters.NewPrometheusExporter(promConfig, collector)
	if err != nil {
		log.Fatalf("Failed to create Prometheus exporter: %v", err)
	}

	if err := exporter.Start(); err != nil {
		log.Fatalf("Failed to start Prometheus exporter: %v", err)
	}
	defer exporter.Stop()

	// Simulate DNS query results
	collector.AddResult(internal_dns.Result{
		Domain:       "example.com",
		QueryType:    "A",
		Server:       "8.8.8.8",
		Protocol:     "udp",
		Duration:     25 * time.Millisecond,
		Timestamp:    time.Now(),
		ResponseCode: dns.RcodeSuccess,
		Answers:      []string{"93.184.216.34"},
	})

	// Metrics are now available at http://localhost:9090/metrics
	fmt.Println("Prometheus metrics available at http://localhost:9090/metrics")
}

// Example of using Prometheus exporter with high-performance settings
func ExamplePrometheusExporter_highPerformance() {
	// Create metrics configuration optimized for high volume
	metricsConfig := &config.MetricsConfig{
		Enabled:             true,
		WindowDuration:      5 * time.Minute,  // Shorter window for less memory
		MaxStoredResults:    50000,            // Limit stored results
		CalculationInterval: 30 * time.Second, // Less frequent calculations
	}

	collector := metrics.NewCollector(metricsConfig)

	// Configure for high performance (minimal overhead)
	promConfig := &config.PrometheusConfig{
		Enabled:               true,
		Port:                  9090,
		Path:                  "/metrics",
		UpdateInterval:        60 * time.Second,  // Less frequent updates
		IncludeServerLabels:   false,             // Reduce label cardinality
		IncludeProtocolLabels: false,             // Reduce label cardinality
		MetricPrefix:          "dns",
		EnableLatencySampling: false,             // Disable histogram/summary
	}

	exporter, err := exporters.NewPrometheusExporter(promConfig, collector)
	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}

	if err := exporter.Start(); err != nil {
		log.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	fmt.Println("High-performance Prometheus exporter started")
}

// Example of using Prometheus exporter with detailed metrics
func ExamplePrometheusExporter_detailedMetrics() {
	metricsConfig := &config.MetricsConfig{
		Enabled:             true,
		WindowDuration:      30 * time.Minute,
		MaxStoredResults:    200000,
		CalculationInterval: 5 * time.Second,
	}

	collector := metrics.NewCollector(metricsConfig)

	// Configure for detailed metrics collection
	promConfig := &config.PrometheusConfig{
		Enabled:               true,
		Port:                  9090,
		Path:                  "/metrics",
		UpdateInterval:        10 * time.Second,  // Frequent updates
		IncludeServerLabels:   true,              // Include server labels
		IncludeProtocolLabels: true,              // Include protocol labels
		MetricPrefix:          "dns_detailed",
		EnableLatencySampling: true,              // Enable histogram/summary
	}

	exporter, err := exporters.NewPrometheusExporter(promConfig, collector)
	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}

	if err := exporter.Start(); err != nil {
		log.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Add various types of results for detailed metrics
	testQueries := []struct {
		domain   string
		qtype    string
		server   string
		protocol string
		duration time.Duration
		rcode    int
	}{
		{"example.com", "A", "8.8.8.8", "udp", 10 * time.Millisecond, dns.RcodeSuccess},
		{"google.com", "AAAA", "1.1.1.1", "tcp", 15 * time.Millisecond, dns.RcodeSuccess},
		{"test.local", "A", "8.8.4.4", "dot", 25 * time.Millisecond, dns.RcodeNameError},
		{"api.example.com", "A", "cloudflare-dns.com", "doh", 35 * time.Millisecond, dns.RcodeSuccess},
	}

	for _, q := range testQueries {
		collector.AddResult(internal_dns.Result{
			Domain:       q.domain,
			QueryType:    q.qtype,
			Server:       q.server,
			Protocol:     internal_dns.Protocol(q.protocol),
			Duration:     q.duration,
			Timestamp:    time.Now(),
			ResponseCode: q.rcode,
		})
	}

	fmt.Println("Detailed metrics collection enabled")
}

// Example of integrating with existing HTTP server
func ExamplePrometheusExporter_withHTTPServer() {
	// Create collector and exporter
	collector := metrics.NewCollector(nil)
	
	promConfig := &config.PrometheusConfig{
		Enabled:        true,
		Port:           9090,
		Path:           "/metrics",
		UpdateInterval: 30 * time.Second,
		MetricPrefix:   "dns",
	}

	exporter, err := exporters.NewPrometheusExporter(promConfig, collector)
	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}

	// Start the exporter
	if err := exporter.Start(); err != nil {
		log.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// You can also add additional HTTP handlers on different ports
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		// Check if metrics are available
		m := collector.GetMetrics()
		if m != nil && m.Rates.TotalQueries > 0 {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Ready"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Not Ready"))
		}
	})

	// Start health check server on different port
	go http.ListenAndServe(":8080", mux)

	fmt.Println("Prometheus metrics: http://localhost:9090/metrics")
	fmt.Println("Health check: http://localhost:8080/health")
	fmt.Println("Readiness check: http://localhost:8080/ready")
}

// Example of monitoring multiple DNS servers with labels
func ExamplePrometheusExporter_multiServer() {
	collector := metrics.NewCollector(nil)
	
	// Enable server and protocol labels for multi-server monitoring
	promConfig := &config.PrometheusConfig{
		Enabled:               true,
		Port:                  9090,
		Path:                  "/metrics",
		UpdateInterval:        30 * time.Second,
		IncludeServerLabels:   true,
		IncludeProtocolLabels: true,
		MetricPrefix:          "dns",
	}

	exporter, err := exporters.NewPrometheusExporter(promConfig, collector)
	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}

	if err := exporter.Start(); err != nil {
		log.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Simulate monitoring multiple DNS servers
	servers := []struct {
		name     string
		address  string
		protocol string
	}{
		{"Google Primary", "8.8.8.8", "udp"},
		{"Google Secondary", "8.8.4.4", "udp"},
		{"Cloudflare Primary", "1.1.1.1", "tcp"},
		{"Cloudflare DoH", "cloudflare-dns.com", "doh"},
		{"Quad9", "9.9.9.9", "dot"},
	}

	// Simulate queries to different servers
	for _, server := range servers {
		for i := 0; i < 10; i++ {
			collector.AddResult(internal_dns.Result{
				Domain:       fmt.Sprintf("test%d.example.com", i),
				QueryType:    "A",
				Server:       server.address,
				Protocol:     internal_dns.Protocol(server.protocol),
				Duration:     time.Duration(10+i*5) * time.Millisecond,
				Timestamp:    time.Now(),
				ResponseCode: dns.RcodeSuccess,
			})
		}
	}

	fmt.Println("Multi-server monitoring with labels enabled")
	fmt.Println("Metrics will include server and protocol labels for detailed analysis")
}

// Example of using Prometheus exporter with passive monitoring
func ExamplePrometheusExporter_passiveMonitoring() {
	collector := metrics.NewCollector(nil)
	
	promConfig := &config.PrometheusConfig{
		Enabled:               true,
		Port:                  9090,
		Path:                  "/metrics",
		UpdateInterval:        10 * time.Second, // More frequent updates for passive monitoring
		IncludeServerLabels:   true,
		IncludeProtocolLabels: true,
		MetricPrefix:          "dns_passive",
	}

	exporter, err := exporters.NewPrometheusExporter(promConfig, collector)
	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}

	if err := exporter.Start(); err != nil {
		log.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Simulate passive capture results
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	// Simulate captured DNS traffic
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Simulate captured DNS query/response
				collector.AddResult(internal_dns.Result{
					Domain:       "captured.example.com",
					QueryType:    "A",
					Server:       "192.168.1.1", // Local DNS server
					Protocol:     "udp",
					Duration:     time.Duration(5+time.Now().Unix()%20) * time.Millisecond,
					Timestamp:    time.Now(),
					ResponseCode: dns.RcodeSuccess,
				})
			}
		}
	}()

	fmt.Println("Passive monitoring metrics being collected")
	fmt.Println("Metrics available at http://localhost:9090/metrics")
}

// Example of cache detection metrics
func ExamplePrometheusExporter_cacheMetrics() {
	collector := metrics.NewCollector(nil)
	
	promConfig := &config.PrometheusConfig{
		Enabled:        true,
		Port:           9090,
		Path:           "/metrics",
		UpdateInterval: 30 * time.Second,
		MetricPrefix:   "dns",
	}

	exporter, err := exporters.NewPrometheusExporter(promConfig, collector)
	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}

	if err := exporter.Start(); err != nil {
		log.Fatalf("Failed to start exporter: %v", err)
	}
	defer exporter.Stop()

	// Simulate queries with cache detection
	for i := 0; i < 100; i++ {
		isCacheHit := i%5 == 0 // Every 5th query is a cache hit
		duration := 25 * time.Millisecond
		if isCacheHit {
			duration = 2 * time.Millisecond // Cache hits are faster
		}

		collector.AddResult(internal_dns.Result{
			Domain:          "cached.example.com",
			QueryType:       "A",
			Server:          "192.168.1.1",
			Protocol:        "udp",
			Duration:        duration,
			Timestamp:       time.Now(),
			ResponseCode:    dns.RcodeSuccess,
			CacheHit:        isCacheHit,
			CacheAge:        time.Duration(i) * time.Second,
			TTL:             300 * time.Second,
			RecursiveServer: true,
			CacheEfficiency: 0.80, // 80% cache efficiency
		})
	}

	fmt.Println("Cache metrics being collected")
	fmt.Println("Metrics include cache hit rate, TTL, and efficiency")
}