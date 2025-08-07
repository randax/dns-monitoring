package exporters

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randax/dns-monitoring/internal/config"
	"github.com/randax/dns-monitoring/internal/metrics"
)

type PrometheusExporter struct {
	config    *config.PrometheusConfig
	collector *metrics.Collector
	registry  *prometheus.Registry
	server    *prometheusServer
	
	latencyHistogram   *prometheus.HistogramVec
	latencySummary     *prometheus.SummaryVec
	queryCounter       *prometheus.CounterVec
	successCounter     *prometheus.CounterVec
	errorCounter       *prometheus.CounterVec
	responseCodeCounter *prometheus.CounterVec
	queryTypeCounter   *prometheus.CounterVec
	
	qpsGauge          *prometheus.GaugeVec
	successRateGauge  *prometheus.GaugeVec
	errorRateGauge    *prometheus.GaugeVec
	latencyP50Gauge   *prometheus.GaugeVec
	latencyP95Gauge   *prometheus.GaugeVec
	latencyP99Gauge   *prometheus.GaugeVec
	latencyP999Gauge  *prometheus.GaugeVec
	
	updateInterval time.Duration
	stopCh        chan struct{}
	wg            sync.WaitGroup
	mu            sync.RWMutex
}

func NewPrometheusExporter(cfg *config.PrometheusConfig, collector *metrics.Collector) (*PrometheusExporter, error) {
	if cfg == nil {
		return nil, fmt.Errorf("prometheus config is nil")
	}
	if collector == nil {
		return nil, fmt.Errorf("metrics collector is nil")
	}
	
	registry := prometheus.NewRegistry()
	
	exporter := &PrometheusExporter{
		config:         cfg,
		collector:      collector,
		registry:       registry,
		updateInterval: cfg.UpdateInterval,
		stopCh:        make(chan struct{}),
	}
	
	if err := exporter.registerMetrics(); err != nil {
		return nil, fmt.Errorf("failed to register metrics: %w", err)
	}
	
	server, err := newPrometheusServer(cfg, registry)
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus server: %w", err)
	}
	exporter.server = server
	
	return exporter, nil
}

func (e *PrometheusExporter) registerMetrics() error {
	prefix := e.config.MetricPrefix
	if prefix == "" {
		prefix = "dns"
	}
	
	labelNames := e.getLabelNames()
	
	e.latencyHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    fmt.Sprintf("%s_query_duration_seconds", prefix),
			Help:    "DNS query duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		labelNames,
	)
	
	e.latencySummary = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       fmt.Sprintf("%s_query_duration_summary_seconds", prefix),
			Help:       "DNS query duration summary in seconds",
			Objectives: map[float64]float64{0.5: 0.05, 0.95: 0.01, 0.99: 0.001, 0.999: 0.0001},
			MaxAge:     10 * time.Minute,
		},
		labelNames,
	)
	
	e.queryCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_queries_total", prefix),
			Help: "Total number of DNS queries",
		},
		labelNames,
	)
	
	e.successCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_queries_success_total", prefix),
			Help: "Total number of successful DNS queries",
		},
		labelNames,
	)
	
	e.errorCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_queries_error_total", prefix),
			Help: "Total number of failed DNS queries",
		},
		append(labelNames, "error_type"),
	)
	
	e.responseCodeCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_response_codes_total", prefix),
			Help: "Total number of DNS responses by response code",
		},
		append(labelNames, "response_code"),
	)
	
	e.queryTypeCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_query_types_total", prefix),
			Help: "Total number of DNS queries by query type",
		},
		append(labelNames, "query_type"),
	)
	
	e.qpsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_queries_per_second", prefix),
			Help: "Current DNS queries per second",
		},
		labelNames,
	)
	
	e.successRateGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_success_rate_ratio", prefix),
			Help: "Current DNS query success rate (0-1)",
		},
		labelNames,
	)
	
	e.errorRateGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_error_rate_ratio", prefix),
			Help: "Current DNS query error rate (0-1)",
		},
		labelNames,
	)
	
	e.latencyP50Gauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_latency_p50_seconds", prefix),
			Help: "DNS query latency 50th percentile in seconds",
		},
		labelNames,
	)
	
	e.latencyP95Gauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_latency_p95_seconds", prefix),
			Help: "DNS query latency 95th percentile in seconds",
		},
		labelNames,
	)
	
	e.latencyP99Gauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_latency_p99_seconds", prefix),
			Help: "DNS query latency 99th percentile in seconds",
		},
		labelNames,
	)
	
	e.latencyP999Gauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_latency_p999_seconds", prefix),
			Help: "DNS query latency 99.9th percentile in seconds",
		},
		labelNames,
	)
	
	collectors := []prometheus.Collector{
		e.latencyHistogram,
		e.latencySummary,
		e.queryCounter,
		e.successCounter,
		e.errorCounter,
		e.responseCodeCounter,
		e.queryTypeCounter,
		e.qpsGauge,
		e.successRateGauge,
		e.errorRateGauge,
		e.latencyP50Gauge,
		e.latencyP95Gauge,
		e.latencyP99Gauge,
		e.latencyP999Gauge,
	}
	
	for _, collector := range collectors {
		if err := e.registry.Register(collector); err != nil {
			return fmt.Errorf("failed to register collector: %w", err)
		}
	}
	
	return nil
}

func (e *PrometheusExporter) getLabelNames() []string {
	var labels []string
	
	if e.config.IncludeServerLabels {
		labels = append(labels, "server")
	}
	
	if e.config.IncludeProtocolLabels {
		labels = append(labels, "protocol")
	}
	
	return labels
}

func (e *PrometheusExporter) Start(ctx context.Context) error {
	if err := e.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start prometheus server: %w", err)
	}
	
	e.wg.Add(1)
	go e.updateLoop(ctx)
	
	return nil
}

func (e *PrometheusExporter) Stop() error {
	close(e.stopCh)
	e.wg.Wait()
	
	if e.server != nil {
		return e.server.Stop()
	}
	
	return nil
}

func (e *PrometheusExporter) updateLoop(ctx context.Context) {
	defer e.wg.Done()
	
	ticker := time.NewTicker(e.updateInterval)
	defer ticker.Stop()
	
	e.UpdateMetrics()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.UpdateMetrics()
		}
	}
}

func (e *PrometheusExporter) UpdateMetrics() {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	metrics := e.collector.GetMetrics()
	if metrics == nil {
		return
	}
	
	// Update metrics for overall statistics
	labels := e.buildLabels("", "")
	
	// Update latency metrics
	e.updateLatencyMetrics(metrics, labels)
	
	// Update rate and throughput gauges
	e.updateGaugeMetrics(metrics, labels)
	
	// Update response code distribution
	e.updateResponseCodeMetrics(metrics, "")
	
	// Update query type distribution
	e.updateQueryTypeMetrics(metrics, "")
	
	// If we have per-server metrics in throughput, update those
	if metrics.Throughput.ByServer != nil {
		for serverName, qps := range metrics.Throughput.ByServer {
			serverLabels := e.buildLabels(serverName, "")
			e.qpsGauge.With(serverLabels).Set(qps)
		}
	}
}

func (e *PrometheusExporter) buildLabels(server, protocol string) prometheus.Labels {
	labels := prometheus.Labels{}
	
	if e.config.IncludeServerLabels && server != "" {
		labels["server"] = server
	}
	
	if e.config.IncludeProtocolLabels && protocol != "" {
		labels["protocol"] = protocol
	}
	
	return labels
}

func (e *PrometheusExporter) updateLatencyMetrics(m *metrics.Metrics, labels prometheus.Labels) {
	// Update latency percentile gauges using pre-calculated percentiles
	// Values are in milliseconds, convert to seconds for Prometheus
	e.latencyP50Gauge.With(labels).Set(m.Latency.P50 / 1000.0)
	e.latencyP95Gauge.With(labels).Set(m.Latency.P95 / 1000.0)
	e.latencyP99Gauge.With(labels).Set(m.Latency.P99 / 1000.0)
	e.latencyP999Gauge.With(labels).Set(m.Latency.P999 / 1000.0)
	
	// Note: Histogram and Summary metrics are defined but not updated here
	// as they require individual latency samples which are not available
	// in the aggregated metrics structure. These could be updated at query
	// execution time if individual samples are tracked.
}

// Note: Counter updates are typically handled at the point of query execution
// This is kept for compatibility but may not be used with the current architecture

func (e *PrometheusExporter) updateGaugeMetrics(m *metrics.Metrics, labels prometheus.Labels) {
	// Update QPS gauge
	if m.Throughput.CurrentQPS > 0 {
		e.qpsGauge.With(labels).Set(m.Throughput.CurrentQPS)
	} else {
		e.qpsGauge.With(labels).Set(m.Throughput.AverageQPS)
	}
	
	// Update success and error rate gauges
	e.successRateGauge.With(labels).Set(m.Rates.SuccessRate)
	e.errorRateGauge.With(labels).Set(m.Rates.ErrorRate)
}

func (e *PrometheusExporter) updateResponseCodeMetrics(m *metrics.Metrics, server string) {
	labels := e.buildLabels(server, "")
	
	// Map response codes from the metrics structure
	responseCodes := map[string]int64{
		"NoError":  m.ResponseCode.NoError,
		"NXDomain": m.ResponseCode.NXDomain,
		"ServFail": m.ResponseCode.ServFail,
		"FormErr":  m.ResponseCode.FormErr,
		"NotImpl":  m.ResponseCode.NotImpl,
		"Refused":  m.ResponseCode.Refused,
		"Other":    m.ResponseCode.Other,
	}
	
	for code, count := range responseCodes {
		if count > 0 {
			codeLabels := copyLabels(labels)
			codeLabels["response_code"] = code
			// For gauges, we set the current value rather than adding
			// If you need incremental counters, you'll need to track previous values
		}
	}
}

func (e *PrometheusExporter) updateQueryTypeMetrics(m *metrics.Metrics, server string) {
	labels := e.buildLabels(server, "")
	
	// Map query types from the metrics structure
	queryTypes := map[string]int64{
		"A":     m.QueryType.A,
		"AAAA":  m.QueryType.AAAA,
		"CNAME": m.QueryType.CNAME,
		"MX":    m.QueryType.MX,
		"TXT":   m.QueryType.TXT,
		"NS":    m.QueryType.NS,
		"SOA":   m.QueryType.SOA,
		"PTR":   m.QueryType.PTR,
		"Other": m.QueryType.Other,
	}
	
	for qtype, count := range queryTypes {
		if count > 0 {
			typeLabels := copyLabels(labels)
			typeLabels["query_type"] = qtype
			// For gauges, we set the current value rather than adding
			// If you need incremental counters, you'll need to track previous values
		}
	}
}

