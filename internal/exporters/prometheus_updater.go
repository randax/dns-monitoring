package exporters

import (
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randax/dns-monitoring/internal/metrics"
)

// updateLatencyMetrics updates latency metrics including percentile gauges and optional histogram/summary
func (e *PrometheusExporter) updateLatencyMetrics(m *metrics.Metrics, labels prometheus.Labels) {
	// Validate inputs
	if m == nil {
		log.Printf("ERROR: updateLatencyMetrics called with nil metrics")
		return
	}
	
	if e.gauges == nil {
		log.Printf("ERROR: updateLatencyMetrics: gauges map is nil")
		return
	}
	
	// Update latency percentile gauges using pre-calculated percentiles
	// Values are in milliseconds, convert to seconds for Prometheus
	latencyP50 := m.Latency.P50 / 1000.0
	latencyP95 := m.Latency.P95 / 1000.0
	latencyP99 := m.Latency.P99 / 1000.0
	latencyP999 := m.Latency.P999 / 1000.0
	
	// Set P50 latency
	if gauge, ok := e.gauges["latency_p50"]; ok && gauge != nil {
		if isFinite(latencyP50) && latencyP50 >= 0 {
			gauge.With(labels).Set(latencyP50)
		}
	} else {
		log.Printf("WARNING: latency_p50 gauge not found or nil")
	}
	
	// Set P95 latency
	if gauge, ok := e.gauges["latency_p95"]; ok && gauge != nil {
		if isFinite(latencyP95) && latencyP95 >= 0 {
			gauge.With(labels).Set(latencyP95)
		}
	} else {
		log.Printf("WARNING: latency_p95 gauge not found or nil")
	}
	
	// Set P99 latency
	if gauge, ok := e.gauges["latency_p99"]; ok && gauge != nil {
		if isFinite(latencyP99) && latencyP99 >= 0 {
			gauge.With(labels).Set(latencyP99)
		}
	} else {
		log.Printf("WARNING: latency_p99 gauge not found or nil")
	}
	
	// Set P999 latency
	if gauge, ok := e.gauges["latency_p999"]; ok && gauge != nil {
		if isFinite(latencyP999) && latencyP999 >= 0 {
			gauge.With(labels).Set(latencyP999)
		}
	} else {
		log.Printf("WARNING: latency_p999 gauge not found or nil")
	}
	
	// Update histogram and summary metrics if sampling is enabled and supported
	if e.config != nil && e.config.EnableLatencySampling && e.supportsLatencySampling {
		// Validate histogram and summary exist
		if e.latencyHistogram == nil || e.latencySummary == nil {
			log.Printf("WARNING: latency histogram or summary is nil, skipping sampling")
			return
		}
		
		// Get recent latency samples
		samples := e.getRecentLatencySamplesSafe(1000)
		for _, latencySeconds := range samples {
			if isFinite(latencySeconds) && latencySeconds >= 0 {
				// Reserve panic recovery only for truly unexpected Prometheus library issues
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("ERROR: Unexpected panic in Prometheus Observe: %v, latency: %f", r, latencySeconds)
						}
					}()
					e.latencyHistogram.With(labels).Observe(latencySeconds)
					e.latencySummary.With(labels).Observe(latencySeconds)
				}()
			}
		}
	}
}

// updateGaugeMetrics updates QPS and rate gauges
func (e *PrometheusExporter) updateGaugeMetrics(m *metrics.Metrics, labels prometheus.Labels) {
	// Validate inputs
	if m == nil {
		log.Printf("ERROR: updateGaugeMetrics called with nil metrics")
		return
	}
	
	// Check if gauges map is initialized
	if e.gauges == nil {
		log.Printf("ERROR: updateGaugeMetrics: gauges map is nil")
		return
	}
	
	// Update QPS gauges
	if gauge, ok := e.gauges["qps"]; ok && gauge != nil {
		if isFinite(m.Throughput.CurrentQPS) && m.Throughput.CurrentQPS >= 0 {
			gauge.With(labels).Set(m.Throughput.CurrentQPS)
		}
	}
	
	if gauge, ok := e.gauges["qps_avg"]; ok && gauge != nil {
		if isFinite(m.Throughput.AverageQPS) && m.Throughput.AverageQPS >= 0 {
			gauge.With(labels).Set(m.Throughput.AverageQPS)
		}
	}
	
	if gauge, ok := e.gauges["qps_peak"]; ok && gauge != nil {
		if isFinite(m.Throughput.PeakQPS) && m.Throughput.PeakQPS >= 0 {
			gauge.With(labels).Set(m.Throughput.PeakQPS)
		}
	}
	
	// Update rate gauges (convert from percentage to ratio)
	if gauge, ok := e.gauges["success_rate"]; ok && gauge != nil {
		successRateRatio := m.Rates.SuccessRate / 100.0
		if isFinite(successRateRatio) && successRateRatio >= 0 && successRateRatio <= 1 {
			gauge.With(labels).Set(successRateRatio)
		}
	}
	
	if gauge, ok := e.gauges["error_rate"]; ok && gauge != nil {
		errorRateRatio := m.Rates.ErrorRate / 100.0
		if isFinite(errorRateRatio) && errorRateRatio >= 0 && errorRateRatio <= 1 {
			gauge.With(labels).Set(errorRateRatio)
		}
	}
	
	// Set network error rate (only if enabled)
	if gauge, ok := e.gauges["network_error_rate"]; ok && m.Rates.TotalQueries > 0 {
		networkErrorRate := float64(m.Rates.NetworkErrors) / float64(m.Rates.TotalQueries)
		if isFinite(networkErrorRate) && networkErrorRate >= 0 && networkErrorRate <= 1 {
			gauge.With(labels).Set(networkErrorRate)
		}
	}
	
	// Update Cache Metrics (only if enabled)
	if gauge, ok := e.gauges["cache_hit_rate"]; ok && isFinite(m.Cache.HitRate) && m.Cache.HitRate >= 0 && m.Cache.HitRate <= 1 {
		gauge.With(labels).Set(m.Cache.HitRate)
	}
	
	if gauge, ok := e.gauges["cache_miss_rate"]; ok && isFinite(m.Cache.MissRate) && m.Cache.MissRate >= 0 && m.Cache.MissRate <= 1 {
		gauge.With(labels).Set(m.Cache.MissRate)
	}
	
	if gauge, ok := e.gauges["cache_efficiency"]; ok && isFinite(m.Cache.Efficiency) && m.Cache.Efficiency >= 0 && m.Cache.Efficiency <= 1 {
		gauge.With(labels).Set(m.Cache.Efficiency)
	}
	
	avgTTLSeconds := m.Cache.AverageTTL.Seconds()
	if gauge, ok := e.gauges["cache_avg_ttl"]; ok && isFinite(avgTTLSeconds) && avgTTLSeconds >= 0 {
		gauge.With(labels).Set(avgTTLSeconds)
	}
	
	// Update Network Metrics
	if gauge, ok := e.gauges["packet_loss"]; ok && gauge != nil {
		if isFinite(m.Network.PacketLossPercentage) && m.Network.PacketLossPercentage >= 0 && m.Network.PacketLossPercentage <= 1 {
			gauge.With(labels).Set(m.Network.PacketLossPercentage)
		}
	}
	
	if gauge, ok := e.gauges["network_quality_score"]; ok && gauge != nil {
		if isFinite(m.Network.NetworkQualityScore) && m.Network.NetworkQualityScore >= 0 {
			gauge.With(labels).Set(m.Network.NetworkQualityScore)
		}
	}
	
	// Update jitter metrics (converting from ms to seconds)
	jitterP50Seconds := m.Network.JitterStats.P50 / 1000.0
	jitterP95Seconds := m.Network.JitterStats.P95 / 1000.0
	jitterP99Seconds := m.Network.JitterStats.P99 / 1000.0
	
	if gauge, ok := e.gauges["jitter_p50"]; ok && gauge != nil && isFinite(jitterP50Seconds) {
		gauge.With(labels).Set(jitterP50Seconds)
	}
	if gauge, ok := e.gauges["jitter_p95"]; ok && gauge != nil && isFinite(jitterP95Seconds) {
		gauge.With(labels).Set(jitterP95Seconds)
	}
	if gauge, ok := e.gauges["jitter_p99"]; ok && gauge != nil && isFinite(jitterP99Seconds) {
		gauge.With(labels).Set(jitterP99Seconds)
	}
	
	// Update network latency metrics (converting from ms to seconds)
	netLatencyP50Seconds := m.Network.NetworkLatencyStats.P50 / 1000.0
	netLatencyP95Seconds := m.Network.NetworkLatencyStats.P95 / 1000.0
	netLatencyP99Seconds := m.Network.NetworkLatencyStats.P99 / 1000.0
	
	if gauge, ok := e.gauges["network_latency_p50"]; ok && gauge != nil && isFinite(netLatencyP50Seconds) {
		gauge.With(labels).Set(netLatencyP50Seconds)
	}
	if gauge, ok := e.gauges["network_latency_p95"]; ok && gauge != nil && isFinite(netLatencyP95Seconds) {
		gauge.With(labels).Set(netLatencyP95Seconds)
	}
	if gauge, ok := e.gauges["network_latency_p99"]; ok && gauge != nil && isFinite(netLatencyP99Seconds) {
		gauge.With(labels).Set(netLatencyP99Seconds)
	}
}

// updateCounterMetrics updates counter metrics (queries, response codes, query types)
func (e *PrometheusExporter) updateCounterMetrics(m *metrics.Metrics, labels prometheus.Labels) {
	// Validate inputs
	if m == nil {
		log.Printf("ERROR: updateCounterMetrics called with nil metrics")
		return
	}
	
	if e.counters == nil {
		log.Printf("ERROR: updateCounterMetrics: counters map is nil")
		return
	}
	
	// Update query counters with delta calculation
	e.updateQueryCounters(m.Rates.TotalQueries, m.Rates.SuccessQueries, m.Rates.ErrorQueries, labels)
	
	// Update response code counters
	// Always update as they have minimal overhead
	e.updateResponseCodeCounters(&m.ResponseCode, labels)
	
	// Update query type counters
	// Always update as they have minimal overhead
	e.updateQueryTypeCounters(&m.QueryType, labels)
	
	// Update cache counters
	// Always update as they have minimal overhead
	e.updateCacheCounters(m.Cache, labels)
	
	// Update network counters
	// Always update as they have minimal overhead
	e.updateNetworkCounters(m.Network, labels)
}

// updateQueryCounters updates the query counter metrics
func (e *PrometheusExporter) updateQueryCounters(total, success, errors int64, labels prometheus.Labels) {
	// Calculate deltas from last update
	deltaTotal := total - e.lastCounters.totalQueries
	deltaSuccess := success - e.lastCounters.successQueries
	deltaErrors := errors - e.lastCounters.errorQueries
	
	// Initialize counters if they haven't been used yet (ensures they appear in metrics)
	// This is safe because counters can only increase, never decrease
	if _, ok := e.counters["queries_total"]; ok {
		counter := e.counters["queries_total"].With(labels)
		// Add 0 to ensure the metric is initialized and appears in the output
		counter.Add(0)
		
		// Only add positive deltas to avoid counter decreases
		if deltaTotal > 0 {
			counter.Add(float64(deltaTotal))
		}
	}
	
	if _, ok := e.counters["success_total"]; ok {
		counter := e.counters["success_total"].With(labels)
		counter.Add(0) // Initialize
		if deltaSuccess > 0 {
			counter.Add(float64(deltaSuccess))
		}
	}
	
	if _, ok := e.counters["error_total"]; ok {
		errorLabels := copyLabelsWithExtra(labels, "error_type", "query_error")
		counter := e.counters["error_total"].With(errorLabels)
		counter.Add(0) // Initialize
		if deltaErrors > 0 {
			counter.Add(float64(deltaErrors))
		}
	}
	
	// Update last known values for next delta calculation
	e.lastCounters.totalQueries = total
	e.lastCounters.successQueries = success
	e.lastCounters.errorQueries = errors
	e.lastCounters.lastUpdate = time.Now()
}

// updateResponseCodeCounters updates response code distribution counters
func (e *PrometheusExporter) updateResponseCodeCounters(codes *metrics.ResponseCodeDistribution, labels prometheus.Labels) {
	// Validate inputs
	if codes == nil {
		log.Printf("WARNING: updateResponseCodeCounters called with nil codes")
		return
	}
	
	if e.counters == nil {
		log.Printf("ERROR: updateResponseCodeCounters: counters map is nil")
		return
	}
	
	// Check if response_codes counter exists
	counter, ok := e.counters["response_codes"]
	if !ok || counter == nil {
		log.Printf("ERROR: response_codes counter not found or nil")
		return
	}
	
	// Calculate deltas for each response code
	deltaNOERROR := codes.NoError - e.lastCounters.responseCodeNOERROR
	deltaNXDOMAIN := codes.NXDomain - e.lastCounters.responseCodeNXDOMAIN
	deltaSERVFAIL := codes.ServFail - e.lastCounters.responseCodeSERVFAIL
	deltaFORMERR := codes.FormErr - e.lastCounters.responseCodeFORMERR
	deltaNOTIMPL := codes.NotImpl - e.lastCounters.responseCodeNOTIMPL
	deltaREFUSED := codes.Refused - e.lastCounters.responseCodeREFUSED
	deltaOther := codes.Other - e.lastCounters.responseCodeOther
	
	// Add positive deltas to counters
	if deltaNOERROR > 0 {
		labelsWithCode := copyLabelsWithExtra(labels, "response_code", "NOERROR")
		counter.With(labelsWithCode).Add(float64(deltaNOERROR))
	}
	if deltaNXDOMAIN > 0 {
		labelsWithCode := copyLabelsWithExtra(labels, "response_code", "NXDOMAIN")
		counter.With(labelsWithCode).Add(float64(deltaNXDOMAIN))
	}
	if deltaSERVFAIL > 0 {
		labelsWithCode := copyLabelsWithExtra(labels, "response_code", "SERVFAIL")
		counter.With(labelsWithCode).Add(float64(deltaSERVFAIL))
	}
	if deltaFORMERR > 0 {
		labelsWithCode := copyLabelsWithExtra(labels, "response_code", "FORMERR")
		counter.With(labelsWithCode).Add(float64(deltaFORMERR))
	}
	if deltaNOTIMPL > 0 {
		labelsWithCode := copyLabelsWithExtra(labels, "response_code", "NOTIMP")
		counter.With(labelsWithCode).Add(float64(deltaNOTIMPL))
	}
	if deltaREFUSED > 0 {
		labelsWithCode := copyLabelsWithExtra(labels, "response_code", "REFUSED")
		counter.With(labelsWithCode).Add(float64(deltaREFUSED))
	}
	if deltaOther > 0 {
		labelsWithCode := copyLabelsWithExtra(labels, "response_code", "OTHER")
		counter.With(labelsWithCode).Add(float64(deltaOther))
	}
	
	// Update last known values
	e.lastCounters.responseCodeNOERROR = codes.NoError
	e.lastCounters.responseCodeNXDOMAIN = codes.NXDomain
	e.lastCounters.responseCodeSERVFAIL = codes.ServFail
	e.lastCounters.responseCodeFORMERR = codes.FormErr
	e.lastCounters.responseCodeNOTIMPL = codes.NotImpl
	e.lastCounters.responseCodeREFUSED = codes.Refused
	e.lastCounters.responseCodeOther = codes.Other
}

// updateQueryTypeCounters updates query type distribution counters
func (e *PrometheusExporter) updateQueryTypeCounters(types *metrics.QueryTypeDistribution, labels prometheus.Labels) {
	// Validate inputs
	if types == nil {
		log.Printf("WARNING: updateQueryTypeCounters called with nil types")
		return
	}
	
	if e.counters == nil {
		log.Printf("ERROR: updateQueryTypeCounters: counters map is nil")
		return
	}
	
	if e.lastCounters.queryTypes == nil {
		// Initialize if not already initialized
		e.lastCounters.queryTypes = make(map[string]int64)
		e.lastCounters.queryTypesLastUpdate = make(map[string]time.Time)
	}
	
	// Check if query_types counter exists
	counter, ok := e.counters["query_types"]
	if !ok || counter == nil {
		log.Printf("ERROR: query_types counter not found or nil")
		return
	}
	
	// Update counter for each query type with delta calculation
	queryTypes := map[string]int64{
		"A":     types.A,
		"AAAA":  types.AAAA,
		"CNAME": types.CNAME,
		"MX":    types.MX,
		"TXT":   types.TXT,
		"NS":    types.NS,
		"SOA":   types.SOA,
		"PTR":   types.PTR,
		"OTHER": types.Other,
	}
	
	now := time.Now()
	for qtype, count := range queryTypes {
		lastCount := e.lastCounters.queryTypes[qtype]
		delta := count - lastCount
		if delta > 0 {
			labelsWithType := copyLabelsWithExtra(labels, "query_type", qtype)
			counter.With(labelsWithType).Add(float64(delta))
		}
		e.lastCounters.queryTypes[qtype] = count
		if e.lastCounters.queryTypesLastUpdate != nil {
			e.lastCounters.queryTypesLastUpdate[qtype] = now
		}
	}
}

// updateCacheCounters updates cache-related counter metrics
func (e *PrometheusExporter) updateCacheCounters(cache *metrics.CacheMetrics, labels prometheus.Labels) {
	// Validate inputs
	if cache == nil {
		log.Printf("WARNING: updateCacheCounters called with nil cache")
		return
	}
	
	if e.counters == nil {
		log.Printf("ERROR: updateCacheCounters: counters map is nil")
		return
	}
	
	// Check if cache_queries_total counter exists
	counter, ok := e.counters["cache_queries_total"]
	if !ok || counter == nil {
		log.Printf("WARNING: cache_queries_total counter not found or nil")
		return
	}
	
	// Add counter for total cacheable queries observed
	delta := cache.TotalCacheableQueries - e.lastCounters.cacheableQueries
	if delta > 0 {
		counter.With(labels).Add(float64(delta))
	}
	e.lastCounters.cacheableQueries = cache.TotalCacheableQueries
}

// updateNetworkCounters updates network-related counter metrics
func (e *PrometheusExporter) updateNetworkCounters(network *metrics.NetworkMetrics, labels prometheus.Labels) {
	// Validate inputs
	if network == nil {
		log.Printf("WARNING: updateNetworkCounters called with nil network")
		return
	}
	
	if network.InterfaceStats == nil {
		log.Printf("DEBUG: updateNetworkCounters: no interface stats available")
		return
	}
	
	if e.counters == nil {
		log.Printf("ERROR: updateNetworkCounters: counters map is nil")
		return
	}
	
	// Validate the required counters exist
	sentCounter, sentOk := e.counters["network_packets_sent"]
	recvCounter, recvOk := e.counters["network_packets_received"]
	
	if !sentOk || sentCounter == nil || !recvOk || recvCounter == nil {
		log.Printf("WARNING: network packet counters not found or nil")
		return
	}
	
	// Initialize maps if nil
	if e.lastCounters.interfacePacketsSent == nil {
		e.lastCounters.interfacePacketsSent = make(map[string]int64)
	}
	if e.lastCounters.interfacePacketsReceived == nil {
		e.lastCounters.interfacePacketsReceived = make(map[string]int64)
	}
	if e.lastCounters.interfaceLastUpdate == nil {
		e.lastCounters.interfaceLastUpdate = make(map[string]time.Time)
	}
	
	// Update interface-specific counters
	now := time.Now()
	for iface, stats := range network.InterfaceStats {
		ifaceLabels := copyLabelsWithExtra(labels, "interface", iface)
		
		// Update packets sent counter
		lastSent := e.lastCounters.interfacePacketsSent[iface]
		deltaSent := stats.PacketsSent - lastSent
		if deltaSent > 0 {
			sentCounter.With(ifaceLabels).Add(float64(deltaSent))
		}
		e.lastCounters.interfacePacketsSent[iface] = stats.PacketsSent
		
		// Update packets received counter
		lastReceived := e.lastCounters.interfacePacketsReceived[iface]
		deltaReceived := stats.PacketsReceived - lastReceived
		if deltaReceived > 0 {
			recvCounter.With(ifaceLabels).Add(float64(deltaReceived))
		}
		e.lastCounters.interfacePacketsReceived[iface] = stats.PacketsReceived
		e.lastCounters.interfaceLastUpdate[iface] = now
	}
}

// updatePerProtocolMetrics updates metrics broken down by protocol
func (e *PrometheusExporter) updatePerProtocolMetrics(m *metrics.Metrics) {
	// Validate inputs
	if m == nil || e.config == nil || e.gauges == nil {
		return
	}
	
	if e.config.IncludeProtocolLabels && m.Throughput.ByProtocol != nil {
		gauge, ok := e.gauges["qps"]
		if !ok || gauge == nil {
			log.Printf("WARNING: qps gauge not found for protocol metrics")
			return
		}
		
		for protocol, qps := range m.Throughput.ByProtocol {
			if !isFinite(qps) || qps < 0 {
				log.Printf("WARNING: Invalid QPS value for protocol %s: %f", protocol, qps)
				continue
			}
			protocolLabels := e.buildLabels("", protocol)
			gauge.With(protocolLabels).Set(qps)
		}
	}
}

// updatePerServerMetrics updates metrics broken down by server
func (e *PrometheusExporter) updatePerServerMetrics(m *metrics.Metrics) {
	// Validate inputs
	if m == nil || e.config == nil || e.gauges == nil {
		return
	}
	
	if e.config.IncludeServerLabels && m.Throughput.ByServer != nil {
		gauge, ok := e.gauges["qps"]
		if !ok || gauge == nil {
			log.Printf("WARNING: qps gauge not found for server metrics")
			return
		}
		
		for serverName, qps := range m.Throughput.ByServer {
			if !isFinite(qps) || qps < 0 {
				log.Printf("WARNING: Invalid QPS value for server %s: %f", serverName, qps)
				continue
			}
			
			serverLabels := e.buildLabels(serverName, "")
			gauge.With(serverLabels).Set(qps)
			
			// Update per-server latency metrics if sampling is enabled
			if e.config.EnableLatencySampling && e.config.IncludeServerLabels {
				if e.latencyHistogram == nil || e.latencySummary == nil {
					log.Printf("WARNING: latency histogram or summary nil for server %s", serverName)
					continue
				}
				
				// Get samples for this specific time period with error handling
				samples := e.getLatencySamplesForPeriodSafe(
					m.Period.Start,
					m.Period.End,
					100, // Limit to 100 samples per server to avoid excessive updates
				)
				
				for _, latencySeconds := range samples {
					if isFinite(latencySeconds) && latencySeconds >= 0 {
						// Reserve panic recovery only for truly unexpected Prometheus library issues
						func() {
							defer func() {
								if r := recover(); r != nil {
									log.Printf("ERROR: Unexpected panic in server metrics Observe: %v, server: %s", r, serverName)
								}
							}()
							e.latencyHistogram.With(serverLabels).Observe(latencySeconds)
							e.latencySummary.With(serverLabels).Observe(latencySeconds)
						}()
					}
				}
			}
		}
	}
}

// getRecentLatencySamplesSafe safely calls GetRecentLatencySamples with error handling
// Returns empty slice if the method is not available or encounters an error
func (e *PrometheusExporter) getRecentLatencySamplesSafe(maxSamples int) []float64 {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("WARNING: Failed to get recent latency samples: %v", r)
		}
	}()
	
	// Check if sampling is enabled, collector is not nil, and supports latency sampling
	if e.collector == nil || !e.config.EnableLatencySampling || !e.supportsLatencySampling {
		return []float64{}
	}
	
	// Use type assertion to call the extended method
	if extendedCollector, ok := e.collector.(ExtendedMetricsProvider); ok {
		return extendedCollector.GetRecentLatencySamples(maxSamples)
	}
	
	// This shouldn't happen if supportsLatencySampling is properly set
	return []float64{}
}

// getLatencySamplesForPeriodSafe safely calls GetLatencySamplesForPeriod with error handling
// Returns empty slice if the method is not available or encounters an error
func (e *PrometheusExporter) getLatencySamplesForPeriodSafe(start, end time.Time, maxSamples int) []float64 {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("WARNING: Failed to get latency samples for period: %v", r)
		}
	}()
	
	// Check if sampling is enabled, collector is not nil, and supports latency sampling
	if e.collector == nil || !e.config.EnableLatencySampling || !e.supportsLatencySampling {
		return []float64{}
	}
	
	// Use type assertion to call the extended method
	if extendedCollector, ok := e.collector.(ExtendedMetricsProvider); ok {
		return extendedCollector.GetLatencySamplesForPeriod(start, end, maxSamples)
	}
	
	// This shouldn't happen if supportsLatencySampling is properly set
	return []float64{}
}

