// Package exporters provides various metric export implementations.
// The Prometheus exporter provides two approaches for latency metrics:
//
// 1. Percentile Gauges (Always Available):
//    - Pre-calculated P50, P95, P99, P999 percentiles
//    - Low overhead, updated from aggregated metrics
//    - Suitable for most monitoring use cases
//    - Memory usage: ~4 gauge metrics per label combination
//
// 2. Histogram/Summary Metrics (Optional via EnableLatencySampling):
//    - Provides full latency distribution data
//    - Higher memory/CPU overhead due to sample tracking
//    - Enables advanced Prometheus queries and quantile calculations
//    - Required for functions like histogram_quantile() in PromQL
//
// Memory Implications:
//   - When EnableLatencySampling is false (default):
//     * Only percentile gauges are registered and maintained
//     * Minimal memory overhead (~4 float64 values per metric set)
//     * Suitable for high-cardinality label combinations
//
//   - When EnableLatencySampling is true:
//     * Histogram: Allocates buckets for each label combination
//       - Default: 12 buckets × 8 bytes × label combinations
//       - Can grow significantly with many servers/protocols
//     * Summary: Maintains sliding window of samples
//       - Stores recent samples for quantile calculation
//       - Memory grows with sample rate and window size
//     * Recommended only when advanced PromQL queries are needed
//
// Configuration:
//   prometheus:
//     enable_latency_sampling: false  # Set to true for histogram/summary metrics
//
// Performance Recommendations:
//   - For most use cases, keep enable_latency_sampling: false
//   - Percentile gauges provide sufficient visibility with minimal overhead
//   - Enable sampling only if you need:
//     * histogram_quantile() calculations
//     * Heatmaps in Grafana
//     * Custom quantile aggregations across time windows
package exporters

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randax/dns-monitoring/internal/config"
	"github.com/randax/dns-monitoring/internal/metrics"
)

type PrometheusExporter struct {
	config    *config.PrometheusConfig
	collector metrics.MetricsProvider
	registry  *prometheus.Registry
	server    *prometheusServer
	
	// Latency metrics
	latencyHistogram   *prometheus.HistogramVec
	latencySummary     *prometheus.SummaryVec
	
	// Counter metrics
	counters map[string]*prometheus.CounterVec
	
	// Gauge metrics
	gauges map[string]*prometheus.GaugeVec
	
	// Cache metrics
	cacheMetrics map[string]prometheus.Collector
	
	// Network metrics
	networkMetrics map[string]prometheus.Collector
	
	updateInterval time.Duration
	stopCh        chan struct{}
	wg            sync.WaitGroup
	mu            sync.RWMutex
	
	// Track previous metrics values for calculating increments
	previousMetrics *metrics.Metrics
	
	// Counter state tracking for delta calculations
	lastCounters counterState
	
	// Feature availability flags based on interface capabilities
	supportsLatencySampling bool
}

// ExtendedMetricsProvider is now just an alias for MetricsProvider since
// the latency sampling methods are part of the base interface
type ExtendedMetricsProvider = metrics.MetricsProvider

// counterState tracks last known counter values for delta calculations
type counterState struct {
	totalQueries     int64
	successQueries   int64
	errorQueries     int64
	
	// Response code counters
	responseCodeNOERROR  int64
	responseCodeNXDOMAIN int64
	responseCodeSERVFAIL int64
	responseCodeFORMERR  int64
	responseCodeNOTIMPL  int64
	responseCodeREFUSED  int64
	responseCodeOther    int64
	
	// Query type counters with last update tracking
	queryTypes            map[string]int64
	queryTypesLastUpdate  map[string]time.Time
	
	// Cache counters
	cacheableQueries int64
	
	// Network interface counters with last update tracking
	interfacePacketsSent         map[string]int64
	interfacePacketsReceived     map[string]int64
	interfaceLastUpdate          map[string]time.Time
	
	// Global last update time for non-map counters
	lastUpdate time.Time
}

// checkLatencySamplingSupport checks if the collector actually supports latency sampling
// by attempting to call the methods and checking if they panic or return valid data
func checkLatencySamplingSupport(collector metrics.MetricsProvider) bool {
	// Use defer to catch any panics
	defer func() {
		if r := recover(); r != nil {
			// Method panicked, not supported
		}
	}()
	
	// Try to call GetRecentLatencySamples with a small sample size
	// If it panics or returns nil, it's not supported
	samples := func() (result []float64) {
		defer func() {
			if r := recover(); r != nil {
				result = nil
			}
		}()
		return collector.GetRecentLatencySamples(1)
	}()
	
	// If we got here without panic and have a non-nil result (even if empty),
	// the method is supported
	return samples != nil
}

func NewPrometheusExporter(cfg *config.PrometheusConfig, collector metrics.MetricsProvider) (*PrometheusExporter, error) {
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
		previousMetrics: &metrics.Metrics{},
		lastCounters: counterState{
			queryTypes:               make(map[string]int64),
			queryTypesLastUpdate:     make(map[string]time.Time),
			interfacePacketsSent:     make(map[string]int64),
			interfacePacketsReceived: make(map[string]int64),
			interfaceLastUpdate:      make(map[string]time.Time),
			lastUpdate:               time.Now(),
		},
	}
	
	// Check if collector supports extended latency sampling methods
	// We try to call the method with a safe wrapper to detect support
	exporter.supportsLatencySampling = checkLatencySamplingSupport(collector)
	
	// Log capability detection results
	if cfg.EnableLatencySampling {
		if exporter.supportsLatencySampling {
			log.Printf("INFO: Latency sampling enabled and collector supports extended methods")
		} else {
			log.Printf("WARNING: Latency sampling enabled but collector does not support extended methods (GetRecentLatencySamples, GetLatencySamplesForPeriod). Histogram/summary metrics will not be populated.")
		}
	}
	
	if err := exporter.registerMetrics(); err != nil {
		return nil, fmt.Errorf("failed to register metrics: %w", err)
	}
	
	// Initialize counter state with current metric values to avoid false deltas on startup
	exporter.initializeCounterState()
	
	server, err := newPrometheusServer(cfg, registry, exporter)
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus server: %w", err)
	}
	exporter.server = server
	
	return exporter, nil
}

// initializeCounterState initializes the counter state with current metric values
// to avoid false deltas on application restart
func (e *PrometheusExporter) initializeCounterState() {
	// Safely get current metrics from collector with error handling
	defer func() {
		if r := recover(); r != nil {
			log.Printf("WARNING: Could not initialize counter state: %v", r)
		}
	}()
	
	// Get current metrics from collector
	currentMetrics := e.collector.GetMetrics()
	if currentMetrics == nil {
		// If no metrics available yet, start with zeros (which is already the case)
		log.Printf("INFO: No metrics available yet for counter initialization, starting with zeros")
		return
	}
	
	// Initialize query counters with current values
	e.lastCounters.totalQueries = currentMetrics.Rates.TotalQueries
	e.lastCounters.successQueries = currentMetrics.Rates.SuccessQueries
	e.lastCounters.errorQueries = currentMetrics.Rates.ErrorQueries
	
	// Initialize response code counters with current values
	e.lastCounters.responseCodeNOERROR = currentMetrics.ResponseCode.NoError
	e.lastCounters.responseCodeNXDOMAIN = currentMetrics.ResponseCode.NXDomain
	e.lastCounters.responseCodeSERVFAIL = currentMetrics.ResponseCode.ServFail
	e.lastCounters.responseCodeFORMERR = currentMetrics.ResponseCode.FormErr
	e.lastCounters.responseCodeNOTIMPL = currentMetrics.ResponseCode.NotImpl
	e.lastCounters.responseCodeREFUSED = currentMetrics.ResponseCode.Refused
	e.lastCounters.responseCodeOther = currentMetrics.ResponseCode.Other
	
	// Initialize query type counters with current values
	if e.lastCounters.queryTypes == nil {
		e.lastCounters.queryTypes = make(map[string]int64)
	}
	if e.lastCounters.queryTypesLastUpdate == nil {
		e.lastCounters.queryTypesLastUpdate = make(map[string]time.Time)
	}
	now := time.Now()
	e.lastCounters.queryTypes["A"] = currentMetrics.QueryType.A
	e.lastCounters.queryTypes["AAAA"] = currentMetrics.QueryType.AAAA
	e.lastCounters.queryTypes["CNAME"] = currentMetrics.QueryType.CNAME
	e.lastCounters.queryTypes["MX"] = currentMetrics.QueryType.MX
	e.lastCounters.queryTypes["TXT"] = currentMetrics.QueryType.TXT
	e.lastCounters.queryTypes["NS"] = currentMetrics.QueryType.NS
	e.lastCounters.queryTypes["SOA"] = currentMetrics.QueryType.SOA
	e.lastCounters.queryTypes["PTR"] = currentMetrics.QueryType.PTR
	e.lastCounters.queryTypes["OTHER"] = currentMetrics.QueryType.Other
	// Mark all as recently updated
	for qtype := range e.lastCounters.queryTypes {
		e.lastCounters.queryTypesLastUpdate[qtype] = now
	}
	
	// Initialize cache counters with current values
	e.lastCounters.cacheableQueries = currentMetrics.Cache.TotalCacheableQueries
	
	// Initialize network interface counters with current values
	if e.lastCounters.interfacePacketsSent == nil {
		e.lastCounters.interfacePacketsSent = make(map[string]int64)
	}
	if e.lastCounters.interfacePacketsReceived == nil {
		e.lastCounters.interfacePacketsReceived = make(map[string]int64)
	}
	if e.lastCounters.interfaceLastUpdate == nil {
		e.lastCounters.interfaceLastUpdate = make(map[string]time.Time)
	}
	if currentMetrics.Network.InterfaceStats != nil {
		for iface, stats := range currentMetrics.Network.InterfaceStats {
			e.lastCounters.interfacePacketsSent[iface] = stats.PacketsSent
			e.lastCounters.interfacePacketsReceived[iface] = stats.PacketsReceived
			e.lastCounters.interfaceLastUpdate[iface] = now
		}
	}
	
	// Also store the current metrics as previousMetrics for consistency
	e.previousMetrics = currentMetrics
	e.lastCounters.lastUpdate = now
	
	log.Printf("INFO: Initialized Prometheus counter state with current metric values to avoid false deltas on restart")
}

func (e *PrometheusExporter) registerMetrics() error {
	prefix := e.config.MetricPrefix
	if prefix == "" {
		prefix = "dns"
	}
	
	// Validate metric prefix
	if err := validateMetricName(prefix); err != nil {
		return fmt.Errorf("invalid metric prefix: %w", err)
	}
	
	labelNames := e.getLabelNames()
	
	// Convert config features to metric features
	features := MetricFeatures{
		EnableCacheMetrics:    e.config.MetricFeatures.EnableCacheMetrics,
		EnableNetworkMetrics:  e.config.MetricFeatures.EnableNetworkMetrics,
		EnableJitterMetrics:   e.config.MetricFeatures.EnableJitterMetrics,
		EnableTTLMetrics:      e.config.MetricFeatures.EnableTTLMetrics,
		EnableMonitoringMetrics: e.config.MetricFeatures.EnableMonitoringMetrics,
	}
	
	// Log which metric features are enabled
	if features.EnableCacheMetrics || features.EnableNetworkMetrics || features.EnableJitterMetrics || 
	   features.EnableTTLMetrics || features.EnableMonitoringMetrics {
		log.Printf("INFO: Advanced metrics enabled - Cache: %v, Network: %v, Jitter: %v, TTL: %v, Monitoring: %v",
			features.EnableCacheMetrics, features.EnableNetworkMetrics, features.EnableJitterMetrics,
			features.EnableTTLMetrics, features.EnableMonitoringMetrics)
	} else {
		log.Printf("INFO: Using core DNS metrics only (latency, QPS, success rates, response codes). Enable advanced metrics in config for additional insights.")
	}
	
	// Create metrics - histogram/summary are conditionally created
	// When EnableLatencySampling is false OR the collector doesn't support it,
	// histogram and summary metrics are not registered to avoid memory overhead 
	// from unused collectors. Percentile gauges provide sufficient latency visibility 
	// for most monitoring use cases with minimal overhead.
	if e.config.EnableLatencySampling && e.supportsLatencySampling {
		e.latencyHistogram, e.latencySummary = createLatencyMetrics(prefix, labelNames)
	}
	
	// Create metrics with feature-based selection
	e.counters = createCounterMetricsWithFeatures(prefix, labelNames, features)
	e.gauges = createGaugeMetricsWithFeatures(prefix, labelNames, features)
	e.cacheMetrics = createCacheMetricsWithFeatures(prefix, labelNames, features)
	e.networkMetrics = createNetworkMetricsWithFeatures(prefix, labelNames, features)
	
	// Collect all metrics for registration
	var collectors []prometheus.Collector
	
	// Only add histogram/summary metrics if latency sampling is enabled AND supported
	// This prevents memory allocation for unused metric buckets and quantiles
	if e.config.EnableLatencySampling && e.supportsLatencySampling {
		collectors = append(collectors, e.latencyHistogram, e.latencySummary)
	}
	
	// Add counter metrics
	for _, counter := range e.counters {
		collectors = append(collectors, counter)
	}
	
	// Add gauge metrics
	for _, gauge := range e.gauges {
		collectors = append(collectors, gauge)
	}
	
	// Add cache metrics
	for _, metric := range e.cacheMetrics {
		collectors = append(collectors, metric)
	}
	
	// Add network metrics
	for _, metric := range e.networkMetrics {
		collectors = append(collectors, metric)
	}
	
	
	// Register all collectors using helper function
	return registerAllMetrics(e.registry, collectors)
}

func (e *PrometheusExporter) getLabelNames() []string {
	// Use helper function from prometheus_metrics.go
	return createLabelSet(e.config.IncludeServerLabels, e.config.IncludeProtocolLabels)
}

func (e *PrometheusExporter) Start() error {
	ctx := context.Background()
	if err := e.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start prometheus server: %w", err)
	}
	
	e.wg.Add(1)
	go e.updateLoop(ctx)
	
	// Start cleanup goroutine by default (unless explicitly disabled by setting interval to 0)
	// This helps prevent memory growth from unbounded map entries
	if e.config.CounterStateCleanupInterval > 0 {
		e.wg.Add(1)
		go e.cleanupLoop(ctx)
	}
	
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
	
	defer func() {
		if r := recover(); r != nil {
			log.Printf("ERROR: panic in UpdateMetrics: %v", r)
		}
	}()
	
	metrics := e.collector.GetMetrics()
	if metrics == nil {
		log.Printf("WARNING: no metrics available from collector")
		return
	}
	
	// Update metrics for overall statistics
	labels := e.buildLabels("", "")
	
	// Update latency metrics (including histogram/summary if sampling is enabled)
	e.updateLatencyMetrics(metrics, labels)
	
	// Update rate and throughput gauges
	e.updateGaugeMetrics(metrics, labels)
	
	// Update counter metrics (queries, response codes, query types, cache, network)
	e.updateCounterMetrics(metrics, labels)
	
	// Update per-protocol metrics if available
	e.updatePerProtocolMetrics(metrics)
	
	// Update per-server metrics if available
	e.updatePerServerMetrics(metrics)
	
	// Store current metrics as previous for next update
	e.previousMetrics = metrics
}

func (e *PrometheusExporter) buildLabels(server, protocol string) prometheus.Labels {
	labels := prometheus.Labels{}
	
	// Add instance label if no other labels are configured
	if !e.config.IncludeServerLabels && !e.config.IncludeProtocolLabels {
		labels["instance"] = "default"
	}
	
	if e.config.IncludeServerLabels {
		// Always include server label when configured, use "unknown" if empty
		if server == "" {
			server = "unknown"
		}
		labels["server"] = server
	}
	
	if e.config.IncludeProtocolLabels {
		// Always include protocol label when configured, use "unknown" if empty
		if protocol == "" {
			protocol = "unknown"
		}
		labels["protocol"] = protocol
	}
	
	return labels
}

// cleanupLoop runs periodically to clean up old counter state entries
func (e *PrometheusExporter) cleanupLoop(ctx context.Context) {
	defer e.wg.Done()
	
	// Use default values if not configured or invalid
	cleanupInterval := e.config.CounterStateCleanupInterval
	if cleanupInterval <= 0 {
		cleanupInterval = time.Hour // Default to 1 hour
	}
	
	maxAge := e.config.CounterStateMaxAge
	if maxAge <= 0 {
		maxAge = 2 * time.Hour // Default to 2 hours
	}
	
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	
	log.Printf("INFO: Started counter state cleanup goroutine (interval: %v, max age: %v)", cleanupInterval, maxAge)
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.cleanupCounterState(maxAge)
		}
	}
}

// cleanupCounterState removes entries from counterState maps that haven't been updated recently
func (e *PrometheusExporter) cleanupCounterState(maxAge time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	defer func() {
		if r := recover(); r != nil {
			log.Printf("ERROR: panic in cleanupCounterState: %v", r)
		}
	}()
	
	// Log current map sizes for monitoring potential memory issues
	queryTypesSize := len(e.lastCounters.queryTypes)
	interfacesSize := len(e.lastCounters.interfacePacketsSent)
	
	// Warn if maps are growing too large
	if queryTypesSize > 1000 {
		log.Printf("WARNING: Query types map has %d entries - potential memory issue", queryTypesSize)
	}
	if interfacesSize > 100 {
		log.Printf("WARNING: Interface counters map has %d entries - potential memory issue", interfacesSize)
	}
	
	now := time.Now()
	cutoff := now.Add(-maxAge)
	
	queryTypesRemoved := 0
	interfacesRemoved := 0
	
	// Clean up query types that haven't been updated
	for qtype, lastUpdate := range e.lastCounters.queryTypesLastUpdate {
		if lastUpdate.Before(cutoff) {
			delete(e.lastCounters.queryTypes, qtype)
			delete(e.lastCounters.queryTypesLastUpdate, qtype)
			queryTypesRemoved++
		}
	}
	
	// Clean up interface counters that haven't been updated
	for iface, lastUpdate := range e.lastCounters.interfaceLastUpdate {
		if lastUpdate.Before(cutoff) {
			delete(e.lastCounters.interfacePacketsSent, iface)
			delete(e.lastCounters.interfacePacketsReceived, iface)
			delete(e.lastCounters.interfaceLastUpdate, iface)
			interfacesRemoved++
		}
	}
	
	if queryTypesRemoved > 0 || interfacesRemoved > 0 {
		log.Printf("INFO: Counter state cleanup completed - removed %d query types, %d interfaces (older than %v). Current sizes: query_types=%d, interfaces=%d",
			queryTypesRemoved, interfacesRemoved, maxAge, 
			len(e.lastCounters.queryTypes), len(e.lastCounters.interfacePacketsSent))
	} else if queryTypesSize > 50 || interfacesSize > 20 {
		// Log periodic status even when nothing removed if maps are moderately large
		log.Printf("INFO: Counter state cleanup check - no stale entries. Current sizes: query_types=%d, interfaces=%d",
			len(e.lastCounters.queryTypes), len(e.lastCounters.interfacePacketsSent))
	}
}

