package exporters

import (
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type metricDefinitions struct {
	latencyBuckets []float64
	summaryObjectives map[float64]float64
	summaryMaxAge time.Duration
}

func getMetricDefinitions() metricDefinitions {
	return metricDefinitions{
		latencyBuckets: []float64{
			0.001,  // 1ms
			0.005,  // 5ms
			0.010,  // 10ms
			0.025,  // 25ms
			0.050,  // 50ms
			0.100,  // 100ms
			0.250,  // 250ms
			0.500,  // 500ms
			1.000,  // 1s
			2.500,  // 2.5s
			5.000,  // 5s
			10.000, // 10s
		},
		summaryObjectives: map[float64]float64{
			0.5:   0.05,   // P50 with 5% error
			0.95:  0.01,   // P95 with 1% error
			0.99:  0.001,  // P99 with 0.1% error
			0.999: 0.0001, // P999 with 0.01% error
		},
		summaryMaxAge: 10 * time.Minute,
	}
}

func createLatencyMetrics(prefix string, labelNames []string) (*prometheus.HistogramVec, *prometheus.SummaryVec) {
	defs := getMetricDefinitions()
	
	histogram := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    fmt.Sprintf("%s_query_duration_seconds", prefix),
			Help:    "DNS query duration in seconds",
			Buckets: defs.latencyBuckets,
		},
		labelNames,
	)
	
	summary := prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       fmt.Sprintf("%s_query_duration_summary_seconds", prefix),
			Help:       "DNS query duration summary in seconds with quantiles",
			Objectives: defs.summaryObjectives,
			MaxAge:     defs.summaryMaxAge,
		},
		labelNames,
	)
	
	return histogram, summary
}

func createCounterMetrics(prefix string, labelNames []string) map[string]*prometheus.CounterVec {
	return createCounterMetricsWithFeatures(prefix, labelNames, GetAllFeatures())
}

func createCounterMetricsWithFeatures(prefix string, labelNames []string, features MetricFeatures) map[string]*prometheus.CounterVec {
	counters := make(map[string]*prometheus.CounterVec)
	
	// === CORE DNS COUNTER METRICS (Always enabled) ===
	
	// Total queries counter
	counters["queries_total"] = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_queries_total", prefix),
			Help: "Total number of DNS queries",
		},
		labelNames,
	)
	
	// Success counter
	counters["success_total"] = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_queries_success_total", prefix),
			Help: "Total number of successful DNS queries",
		},
		labelNames,
	)
	
	// Error counter with type breakdown
	errorLabels := append([]string{}, labelNames...)
	errorLabels = append(errorLabels, "error_type")
	counters["error_total"] = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_queries_error_total", prefix),
			Help: "Total number of failed DNS queries by error type",
		},
		errorLabels,
	)
	
	// Response code breakdown
	responseCodeLabels := append([]string{}, labelNames...)
	responseCodeLabels = append(responseCodeLabels, "response_code")
	counters["response_codes"] = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_response_codes_total", prefix),
			Help: "Total number of DNS responses by response code",
		},
		responseCodeLabels,
	)
	
	// Query type breakdown
	queryTypeLabels := append([]string{}, labelNames...)
	queryTypeLabels = append(queryTypeLabels, "query_type")
	counters["query_types"] = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_query_types_total", prefix),
			Help: "Total number of DNS queries by query type",
		},
		queryTypeLabels,
	)
	
	// === ADVANCED COUNTER METRICS (Conditionally enabled) ===
	
	// Cache analysis counters
	if features.EnableCacheMetrics {
		counters["cache_queries_total"] = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("%s_cache_queries_total", prefix),
				Help: "Total number of cacheable DNS queries",
			},
			labelNames,
		)
	}
	
	// Network interface counters
	if features.EnableNetworkMetrics {
		interfaceLabels := append([]string{}, labelNames...)
		interfaceLabels = append(interfaceLabels, "interface")
		
		counters["network_packets_sent"] = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("%s_network_packets_sent_total", prefix),
				Help: "Total number of DNS packets sent per interface",
			},
			interfaceLabels,
		)
		
		counters["network_packets_received"] = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("%s_network_packets_received_total", prefix),
				Help: "Total number of DNS packets received per interface",
			},
			interfaceLabels,
		)
	}
	
	return counters
}

// MetricFeatures defines which metric groups are enabled
type MetricFeatures struct {
	EnableCacheMetrics    bool
	EnableNetworkMetrics  bool
	EnableJitterMetrics   bool
	EnableTTLMetrics      bool
	EnableMonitoringMetrics bool
}

// GetDefaultFeatures returns a configuration with core metrics enabled
func GetDefaultFeatures() MetricFeatures {
	return MetricFeatures{
		EnableCacheMetrics:    false,
		EnableNetworkMetrics:  false,
		EnableJitterMetrics:   false,
		EnableTTLMetrics:      false,
		EnableMonitoringMetrics: false,
	}
}

// GetAllFeatures returns a configuration with all metrics enabled
func GetAllFeatures() MetricFeatures {
	return MetricFeatures{
		EnableCacheMetrics:    true,
		EnableNetworkMetrics:  true,
		EnableJitterMetrics:   true,
		EnableTTLMetrics:      true,
		EnableMonitoringMetrics: true,
	}
}

func createGaugeMetrics(prefix string, labelNames []string) map[string]*prometheus.GaugeVec {
	return createGaugeMetricsWithFeatures(prefix, labelNames, GetAllFeatures())
}

func createGaugeMetricsWithFeatures(prefix string, labelNames []string, features MetricFeatures) map[string]*prometheus.GaugeVec {
	gauges := make(map[string]*prometheus.GaugeVec)
	
	// === CORE DNS METRICS (Always enabled) ===
	// These are essential for basic DNS monitoring
	
	// Query rate metrics
	gauges["qps"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_queries_per_second", prefix),
			Help: "Current DNS queries per second",
		},
		labelNames,
	)
	
	// Success/Error rates
	gauges["success_rate"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_success_rate_ratio", prefix),
			Help: "Current DNS query success rate (0-1)",
		},
		labelNames,
	)
	
	gauges["error_rate"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_error_rate_ratio", prefix),
			Help: "Current DNS query error rate (0-1)",
		},
		labelNames,
	)
	
	// Core latency percentiles
	gauges["latency_p50"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_latency_p50_seconds", prefix),
			Help: "DNS query latency 50th percentile in seconds",
		},
		labelNames,
	)
	
	gauges["latency_p95"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_latency_p95_seconds", prefix),
			Help: "DNS query latency 95th percentile in seconds",
		},
		labelNames,
	)
	
	gauges["latency_p99"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_latency_p99_seconds", prefix),
			Help: "DNS query latency 99th percentile in seconds",
		},
		labelNames,
	)
	
	gauges["latency_p999"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_latency_p999_seconds", prefix),
			Help: "DNS query latency 99.9th percentile in seconds",
		},
		labelNames,
	)
	
	// Active queries
	gauges["active_queries"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_active_queries", prefix),
			Help: "Number of currently active DNS queries",
		},
		labelNames,
	)
	
	// QPS additional metrics
	gauges["qps_average"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_qps_average", prefix),
			Help: "Average queries per second over time window",
		},
		labelNames,
	)
	
	gauges["qps_peak"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_qps_peak", prefix),
			Help: "Peak queries per second observed",
		},
		labelNames,
	)
	
	// Error rate details
	gauges["timeout_rate"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_timeout_rate_ratio", prefix),
			Help: "DNS query timeout rate (0-1)",
		},
		labelNames,
	)
	
	// === ADVANCED METRICS (Conditionally enabled) ===
	
	// Cache performance metrics
	if features.EnableCacheMetrics {
		gauges["cache_hit_rate"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_cache_hit_rate_ratio", prefix),
				Help: "DNS cache hit rate (0-1)",
			},
			labelNames,
		)
		
		gauges["cache_efficiency"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_cache_efficiency_score", prefix),
				Help: "DNS cache efficiency score (0-1)",
			},
			labelNames,
		)
		
		gauges["cache_miss_rate"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_cache_miss_rate_ratio", prefix),
				Help: "DNS cache miss rate (0-1)",
			},
			labelNames,
		)
		
		gauges["cache_stale_rate"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_cache_stale_rate_ratio", prefix),
				Help: "DNS cache stale entry rate (0-1)",
			},
			labelNames,
		)
		
		gauges["cache_avg_ttl"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_cache_avg_ttl_seconds", prefix),
				Help: "Average DNS cache TTL in seconds",
			},
			labelNames,
		)
	}
	
	// Network analysis metrics
	if features.EnableNetworkMetrics {
		gauges["packet_loss"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_packet_loss_ratio", prefix),
				Help: "DNS packet loss rate (0-1)",
			},
			labelNames,
		)
		
		gauges["network_quality_score"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_network_quality_score", prefix),
				Help: "Network quality score (0-100)",
			},
			labelNames,
		)
		
		gauges["network_error_rate"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_network_error_rate_ratio", prefix),
				Help: "Network error rate (0-1)",
			},
			labelNames,
		)
		
		gauges["connection_error_rate"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_connection_error_rate_ratio", prefix),
				Help: "Connection error rate (0-1)",
			},
			labelNames,
		)
		
		gauges["network_latency_p50"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_network_latency_p50_seconds", prefix),
				Help: "Network latency 50th percentile in seconds",
			},
			labelNames,
		)
		
		gauges["network_latency_p95"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_network_latency_p95_seconds", prefix),
				Help: "Network latency 95th percentile in seconds",
			},
			labelNames,
		)
		
		gauges["network_latency_p99"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_network_latency_p99_seconds", prefix),
				Help: "Network latency 99th percentile in seconds",
			},
			labelNames,
		)
		
		gauges["network_latency_average"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_network_latency_average_seconds", prefix),
				Help: "Average network latency in seconds",
			},
			labelNames,
		)
	}
	
	// Jitter metrics
	if features.EnableJitterMetrics {
		gauges["jitter_p50"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_jitter_p50_seconds", prefix),
				Help: "DNS jitter 50th percentile in seconds",
			},
			labelNames,
		)
		
		gauges["jitter_p95"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_jitter_p95_seconds", prefix),
				Help: "DNS jitter 95th percentile in seconds",
			},
			labelNames,
		)
		
		gauges["jitter_p99"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_jitter_p99_seconds", prefix),
				Help: "DNS jitter 99th percentile in seconds",
			},
			labelNames,
		)
		
		gauges["jitter_average"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_jitter_average_seconds", prefix),
				Help: "Average DNS jitter in seconds",
			},
			labelNames,
		)
	}
	
	// TTL analysis metrics
	if features.EnableTTLMetrics {
		gauges["ttl_average"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_ttl_average_seconds", prefix),
				Help: "Average DNS record TTL in seconds",
			},
			labelNames,
		)
		
		gauges["ttl_min"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_ttl_min_seconds", prefix),
				Help: "Minimum DNS record TTL in seconds",
			},
			labelNames,
		)
		
		gauges["ttl_max"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_ttl_max_seconds", prefix),
				Help: "Maximum DNS record TTL in seconds",
			},
			labelNames,
		)
	}
	
	// Monitoring health metrics
	if features.EnableMonitoringMetrics {
		gauges["monitor_uptime"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_monitor_uptime_seconds", prefix),
				Help: "DNS monitor uptime in seconds",
			},
			labelNames,
		)
		
		gauges["monitor_health_score"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_monitor_health_score", prefix),
				Help: "DNS monitor health score (0-100)",
			},
			labelNames,
		)
		
		gauges["monitor_memory_usage"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_monitor_memory_usage_bytes", prefix),
				Help: "DNS monitor memory usage in bytes",
			},
			labelNames,
		)
		
		gauges["monitor_cpu_usage"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_monitor_cpu_usage_percent", prefix),
				Help: "DNS monitor CPU usage percentage",
			},
			labelNames,
		)
		
		gauges["monitor_goroutines"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_monitor_goroutines", prefix),
				Help: "Number of active goroutines in DNS monitor",
			},
			labelNames,
		)
		
		gauges["monitor_buffer_usage"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_monitor_buffer_usage_ratio", prefix),
				Help: "DNS monitor buffer usage ratio (0-1)",
			},
			labelNames,
		)
		
		gauges["monitor_dropped_queries"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_monitor_dropped_queries_total", prefix),
				Help: "Total number of queries dropped by monitor",
			},
			labelNames,
		)
		
		gauges["monitoring_update_errors"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_monitoring_update_errors_total", prefix),
				Help: "Total number of monitoring update errors",
			},
			labelNames,
		)
		
		gauges["monitoring_validation_errors"] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("%s_monitoring_validation_errors_total", prefix),
				Help: "Total number of monitoring validation errors",
			},
			labelNames,
		)
	}
	
	// Backward compatibility alias
	gauges["qps_avg"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_qps_avg", prefix),
			Help: "Average queries per second (alias for qps_average)",
		},
		labelNames,
	)
	
	return gauges
}

func createCacheMetrics(prefix string, labelNames []string) map[string]prometheus.Collector {
	return createCacheMetricsWithFeatures(prefix, labelNames, GetAllFeatures())
}

func createCacheMetricsWithFeatures(prefix string, labelNames []string, features MetricFeatures) map[string]prometheus.Collector {
	metrics := make(map[string]prometheus.Collector)
	
	// Only create cache metrics if the feature is enabled
	if !features.EnableCacheMetrics {
		return metrics
	}
	
	// Cache TTL histogram
	metrics["cache_ttl"] = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: fmt.Sprintf("%s_cache_ttl_seconds", prefix),
			Help: "DNS cache TTL distribution in seconds",
			Buckets: []float64{10, 30, 60, 300, 600, 1800, 3600, 7200, 14400, 28800, 86400},
		},
		labelNames,
	)
	
	// Cache age histogram
	metrics["cache_age"] = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: fmt.Sprintf("%s_cache_age_seconds", prefix),
			Help: "DNS cache age distribution in seconds",
			Buckets: []float64{1, 5, 10, 30, 60, 300, 600, 1800, 3600, 7200},
		},
		labelNames,
	)
	
	// Per-server cache performance
	serverLabels := append([]string{}, labelNames...)
	serverLabels = append(serverLabels, "recursive_server")
	metrics["recursive_performance"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_recursive_server_performance", prefix),
			Help: "Recursive DNS server cache performance",
		},
		serverLabels,
	)
	
	return metrics
}


func createNetworkMetrics(prefix string, labelNames []string) map[string]prometheus.Collector {
	return createNetworkMetricsWithFeatures(prefix, labelNames, GetAllFeatures())
}

func createNetworkMetricsWithFeatures(prefix string, labelNames []string, features MetricFeatures) map[string]prometheus.Collector {
	metrics := make(map[string]prometheus.Collector)
	
	// Only create network metrics if the feature is enabled
	if !features.EnableNetworkMetrics {
		return metrics
	}
	
	// Network latency histogram
	metrics["network_latency"] = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: fmt.Sprintf("%s_network_latency_seconds", prefix),
			Help: "DNS network latency in seconds",
			Buckets: []float64{0.001, 0.005, 0.010, 0.025, 0.050, 0.100, 0.250, 0.500, 1.000, 2.000},
		},
		labelNames,
	)
	
	// Hop count histogram
	metrics["hop_count"] = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: fmt.Sprintf("%s_hop_count", prefix),
			Help: "Network hop count to DNS servers",
			Buckets: []float64{1, 2, 3, 5, 8, 10, 15, 20, 25, 30},
		},
		labelNames,
	)
	
	// Per-interface metrics
	interfaceLabels := append([]string{}, labelNames...)
	interfaceLabels = append(interfaceLabels, "interface")
	metrics["interface_packet_loss"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_interface_packet_loss_ratio", prefix),
			Help: "Packet loss ratio per network interface",
		},
		interfaceLabels,
	)
	
	// Only add jitter metrics if specifically enabled
	if features.EnableJitterMetrics {
		metrics["jitter"] = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: fmt.Sprintf("%s_jitter_seconds", prefix),
				Help: "DNS network jitter in seconds",
				Buckets: []float64{0.001, 0.005, 0.010, 0.025, 0.050, 0.100, 0.250, 0.500, 1.000},
			},
			labelNames,
		)
	}
	
	return metrics
}

func registerMetric(registry *prometheus.Registry, collector prometheus.Collector) error {
	if err := registry.Register(collector); err != nil {
		if _, ok := err.(prometheus.AlreadyRegisteredError); ok {
			registry.Unregister(collector)
			return registry.Register(collector)
		}
		return fmt.Errorf("failed to register metric: %w", err)
	}
	return nil
}

func registerAllMetrics(registry *prometheus.Registry, metrics []prometheus.Collector) error {
	for _, metric := range metrics {
		if err := registerMetric(registry, metric); err != nil {
			return err
		}
	}
	return nil
}

func createLabelSet(includeServer, includeProtocol bool) []string {
	var labels []string
	
	if includeServer {
		labels = append(labels, "server")
	}
	
	if includeProtocol {
		labels = append(labels, "protocol")
	}
	
	if len(labels) == 0 {
		labels = []string{"instance"}
	}
	
	return labels
}

func validateMetricName(prefix string) error {
	if prefix == "" {
		return fmt.Errorf("metric prefix cannot be empty")
	}
	
	for _, char := range prefix {
		if !((char >= 'a' && char <= 'z') || 
		     (char >= 'A' && char <= 'Z') || 
		     (char >= '0' && char <= '9') || 
		     char == '_' || 
		     char == ':') {
			return fmt.Errorf("invalid character '%c' in metric prefix", char)
		}
	}
	
	return nil
}

func getDefaultLabels() prometheus.Labels {
	return prometheus.Labels{
		"instance": "default",
	}
}

func mergeLabels(base, additional prometheus.Labels) prometheus.Labels {
	result := make(prometheus.Labels)
	
	for k, v := range base {
		result[k] = v
	}
	
	for k, v := range additional {
		result[k] = v
	}
	
	return result
}

func copyLabels(labels prometheus.Labels) prometheus.Labels {
	newLabels := make(prometheus.Labels)
	for k, v := range labels {
		newLabels[k] = v
	}
	return newLabels
}

// Normalize response code for consistent labeling
func normalizeResponseCode(code string) string {
	code = strings.ToUpper(code)
	switch code {
	case "NOERROR", "NO_ERROR", "SUCCESS":
		return "NoError"
	case "NXDOMAIN", "NX_DOMAIN":
		return "NXDomain"
	case "SERVFAIL", "SERV_FAIL", "SERVER_FAIL":
		return "ServFail"
	case "FORMERR", "FORM_ERR", "FORMAT_ERROR":
		return "FormErr"
	case "NOTIMP", "NOT_IMP", "NOT_IMPLEMENTED":
		return "NotImpl"
	case "REFUSED":
		return "Refused"
	default:
		return "Other"
	}
}

// Normalize query type for consistent labeling
func normalizeQueryType(qtype string) string {
	qtype = strings.ToUpper(qtype)
	switch qtype {
	case "A", "AAAA", "CNAME", "MX", "TXT", "NS", "SOA", "PTR", "SRV", "CAA":
		return qtype
	default:
		return "Other"
	}
}

// Validation functions for metric values

// validateRatio ensures a value is between 0 and 1
func validateRatio(value float64, metricName string) (float64, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s: ratio cannot be negative (got %f)", metricName, value)
	}
	if value > 1 {
		return 1, fmt.Errorf("%s: ratio cannot exceed 1 (got %f)", metricName, value)
	}
	return value, nil
}

// validatePercentage ensures a value is between 0 and 100
func validatePercentage(value float64, metricName string) (float64, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s: percentage cannot be negative (got %f)", metricName, value)
	}
	if value > 100 {
		return 100, fmt.Errorf("%s: percentage cannot exceed 100 (got %f)", metricName, value)
	}
	return value, nil
}

// validateNonNegative ensures a value is not negative
func validateNonNegative(value float64, metricName string) (float64, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s: value cannot be negative (got %f)", metricName, value)
	}
	return value, nil
}

// validateCounter ensures counter increments are valid
func validateCounterIncrement(increment int64, metricName string) (float64, error) {
	if increment < 0 {
		return 0, fmt.Errorf("%s: counter increment cannot be negative (got %d)", metricName, increment)
	}
	if increment > 1000000 {
		return float64(increment), fmt.Errorf("%s: unusually large counter increment (%d), possible data issue", metricName, increment)
	}
	return float64(increment), nil
}

// validateLatency ensures latency values are reasonable (in seconds)
func validateLatency(value float64, metricName string) (float64, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s: latency cannot be negative (got %f seconds)", metricName, value)
	}
	if value > 300 {
		return value, fmt.Errorf("%s: unusually high latency (%f seconds), possible timeout", metricName, value)
	}
	return value, nil
}

// isFinite checks if a float64 is finite (not NaN or Inf)
func isFinite(value float64) bool {
	return !strings.Contains(fmt.Sprint(value), "NaN") && !strings.Contains(fmt.Sprint(value), "Inf")
}

// validateQPS ensures QPS values are reasonable
func validateQPS(value float64, metricName string) (float64, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s: QPS cannot be negative (got %f)", metricName, value)
	}
	if value > 1000000 {
		return value, fmt.Errorf("%s: unusually high QPS (%f), possible error", metricName, value)
	}
	return value, nil
}

// validateRate ensures rate values are between 0 and 1
func validateRate(value float64, metricName string) (float64, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s: rate cannot be negative (got %f)", metricName, value)
	}
	if value > 1 {
		return 1, fmt.Errorf("%s: rate cannot exceed 1 (got %f)", metricName, value)
	}
	return value, nil
}

// copyLabelsWithExtra creates a new label map with an additional label
func copyLabelsWithExtra(labels prometheus.Labels, key, value string) prometheus.Labels {
	newLabels := make(prometheus.Labels)
	for k, v := range labels {
		newLabels[k] = v
	}
	newLabels[key] = value
	return newLabels
}