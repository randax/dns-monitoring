package exporters

import (
	"fmt"
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
	counters := make(map[string]*prometheus.CounterVec)
	
	counters["queries_total"] = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_queries_total", prefix),
			Help: "Total number of DNS queries",
		},
		labelNames,
	)
	
	counters["success_total"] = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_queries_success_total", prefix),
			Help: "Total number of successful DNS queries",
		},
		labelNames,
	)
	
	errorLabels := append([]string{}, labelNames...)
	errorLabels = append(errorLabels, "error_type")
	counters["error_total"] = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_queries_error_total", prefix),
			Help: "Total number of failed DNS queries by error type",
		},
		errorLabels,
	)
	
	responseCodeLabels := append([]string{}, labelNames...)
	responseCodeLabels = append(responseCodeLabels, "response_code")
	counters["response_codes"] = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_response_codes_total", prefix),
			Help: "Total number of DNS responses by response code",
		},
		responseCodeLabels,
	)
	
	queryTypeLabels := append([]string{}, labelNames...)
	queryTypeLabels = append(queryTypeLabels, "query_type")
	counters["query_types"] = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_query_types_total", prefix),
			Help: "Total number of DNS queries by query type",
		},
		queryTypeLabels,
	)
	
	return counters
}

func createGaugeMetrics(prefix string, labelNames []string) map[string]*prometheus.GaugeVec {
	gauges := make(map[string]*prometheus.GaugeVec)
	
	gauges["qps"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_queries_per_second", prefix),
			Help: "Current DNS queries per second",
		},
		labelNames,
	)
	
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
	
	gauges["active_queries"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_active_queries", prefix),
			Help: "Number of currently active DNS queries",
		},
		labelNames,
	)
	
	return gauges
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
		     char == '_') {
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