package exporters

import (
	"fmt"
	"math"
	"strings"

	"github.com/randax/dns-monitoring/internal/metrics"
)

type ZabbixRequest struct {
	Request string       `json:"request"`
	Data    []ZabbixItem `json:"data"`
	Clock   int64        `json:"clock,omitempty"`
}

type ZabbixData struct {
	Data []ZabbixItem `json:"data"`
}

type ZabbixItem struct {
	Host  string `json:"host"`
	Key   string `json:"key"`
	Value string `json:"value"`
	Clock int64  `json:"clock,omitempty"`
}

type ZabbixResponse struct {
	Response string `json:"response"`
	Info     string `json:"info,omitempty"`
	Success  int    `json:"success,omitempty"`
	Failed   int    `json:"failed,omitempty"`
	Total    int    `json:"total,omitempty"`
}

func mapLatencyMetrics(m *metrics.Metrics, hostname, prefix string, timestamp int64) []ZabbixItem {
	var items []ZabbixItem
	
	
	// P50 (median)
	if !math.IsNaN(m.Latency.P50) && !math.IsInf(m.Latency.P50, 0) {
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.latency.p50", prefix),
			Value: formatFloat(m.Latency.P50),
			Clock: timestamp,
		})
	}
	
	// P95
	if !math.IsNaN(m.Latency.P95) && !math.IsInf(m.Latency.P95, 0) {
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.latency.p95", prefix),
			Value: formatFloat(m.Latency.P95),
			Clock: timestamp,
		})
	}
	
	// P99
	if !math.IsNaN(m.Latency.P99) && !math.IsInf(m.Latency.P99, 0) {
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.latency.p99", prefix),
			Value: formatFloat(m.Latency.P99),
			Clock: timestamp,
		})
	}
	
	// P999
	if !math.IsNaN(m.Latency.P999) && !math.IsInf(m.Latency.P999, 0) {
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.latency.p999", prefix),
			Value: formatFloat(m.Latency.P999),
			Clock: timestamp,
		})
	}
	
	// Average
	if !math.IsNaN(m.Latency.Mean) && !math.IsInf(m.Latency.Mean, 0) {
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.latency.avg", prefix),
			Value: formatFloat(m.Latency.Mean),
			Clock: timestamp,
		})
	}
	
	// Min
	if !math.IsNaN(m.Latency.Min) && !math.IsInf(m.Latency.Min, 0) {
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.latency.min", prefix),
			Value: formatFloat(m.Latency.Min),
			Clock: timestamp,
		})
	}
	
	// Max
	if !math.IsNaN(m.Latency.Max) && !math.IsInf(m.Latency.Max, 0) {
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.latency.max", prefix),
			Value: formatFloat(m.Latency.Max),
			Clock: timestamp,
		})
	}
	
	return items
}

func mapRateMetrics(m *metrics.Metrics, hostname, prefix string, timestamp int64) []ZabbixItem {
	var items []ZabbixItem
	
	
	// Success rate (convert to percentage 0-100)
	successRate := m.Rates.SuccessRate * 100
	if !math.IsNaN(successRate) && !math.IsInf(successRate, 0) {
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.success_rate", prefix),
			Value: formatFloat(successRate),
			Clock: timestamp,
		})
	}
	
	// Error rate (convert to percentage 0-100)
	errorRate := m.Rates.ErrorRate * 100
	if !math.IsNaN(errorRate) && !math.IsInf(errorRate, 0) {
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.error_rate", prefix),
			Value: formatFloat(errorRate),
			Clock: timestamp,
		})
	}
	
	// Total queries
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.queries.total", prefix),
		Value: fmt.Sprintf("%d", m.Rates.TotalQueries),
		Clock: timestamp,
	})
	
	// Successful queries
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.queries.success", prefix),
		Value: fmt.Sprintf("%d", m.Rates.SuccessQueries),
		Clock: timestamp,
	})
	
	// Failed queries
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.queries.error", prefix),
		Value: fmt.Sprintf("%d", m.Rates.ErrorQueries),
		Clock: timestamp,
	})
	
	return items
}

func mapThroughputMetrics(m *metrics.Metrics, hostname, prefix string, timestamp int64) []ZabbixItem {
	var items []ZabbixItem
	
	
	// Current QPS
	if !math.IsNaN(m.Throughput.CurrentQPS) && !math.IsInf(m.Throughput.CurrentQPS, 0) {
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.qps.current", prefix),
			Value: formatFloat(m.Throughput.CurrentQPS),
			Clock: timestamp,
		})
	}
	
	// Average QPS
	if !math.IsNaN(m.Throughput.AverageQPS) && !math.IsInf(m.Throughput.AverageQPS, 0) {
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.qps.avg", prefix),
			Value: formatFloat(m.Throughput.AverageQPS),
			Clock: timestamp,
		})
	}
	
	// Peak QPS
	if !math.IsNaN(m.Throughput.PeakQPS) && !math.IsInf(m.Throughput.PeakQPS, 0) {
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.qps.peak", prefix),
			Value: formatFloat(m.Throughput.PeakQPS),
			Clock: timestamp,
		})
	}
	
	return items
}

func mapResponseCodeMetrics(m *metrics.Metrics, hostname, prefix string, timestamp int64) []ZabbixItem {
	var items []ZabbixItem
	
	// NoError
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.response_code.noerror", prefix),
		Value: fmt.Sprintf("%d", m.ResponseCode.NoError),
		Clock: timestamp,
	})
	
	// NXDomain
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.response_code.nxdomain", prefix),
		Value: fmt.Sprintf("%d", m.ResponseCode.NXDomain),
		Clock: timestamp,
	})
	
	// ServFail
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.response_code.servfail", prefix),
		Value: fmt.Sprintf("%d", m.ResponseCode.ServFail),
		Clock: timestamp,
	})
	
	// Refused
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.response_code.refused", prefix),
		Value: fmt.Sprintf("%d", m.ResponseCode.Refused),
		Clock: timestamp,
	})
	
	// FormErr
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.response_code.formerr", prefix),
		Value: fmt.Sprintf("%d", m.ResponseCode.FormErr),
		Clock: timestamp,
	})
	
	// NotImpl
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.response_code.notimpl", prefix),
		Value: fmt.Sprintf("%d", m.ResponseCode.NotImpl),
		Clock: timestamp,
	})
	
	// Other
	if m.ResponseCode.Other > 0 {
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.response_code.other", prefix),
			Value: fmt.Sprintf("%d", m.ResponseCode.Other),
			Clock: timestamp,
		})
	}
	
	return items
}

func mapQueryTypeMetrics(m *metrics.Metrics, hostname, prefix string, timestamp int64) []ZabbixItem {
	var items []ZabbixItem
	
	// A
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.query_type.a", prefix),
		Value: fmt.Sprintf("%d", m.QueryType.A),
		Clock: timestamp,
	})
	
	// AAAA
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.query_type.aaaa", prefix),
		Value: fmt.Sprintf("%d", m.QueryType.AAAA),
		Clock: timestamp,
	})
	
	// CNAME
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.query_type.cname", prefix),
		Value: fmt.Sprintf("%d", m.QueryType.CNAME),
		Clock: timestamp,
	})
	
	// MX
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.query_type.mx", prefix),
		Value: fmt.Sprintf("%d", m.QueryType.MX),
		Clock: timestamp,
	})
	
	// TXT
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.query_type.txt", prefix),
		Value: fmt.Sprintf("%d", m.QueryType.TXT),
		Clock: timestamp,
	})
	
	// NS
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.query_type.ns", prefix),
		Value: fmt.Sprintf("%d", m.QueryType.NS),
		Clock: timestamp,
	})
	
	// SOA
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.query_type.soa", prefix),
		Value: fmt.Sprintf("%d", m.QueryType.SOA),
		Clock: timestamp,
	})
	
	// PTR
	items = append(items, ZabbixItem{
		Host:  hostname,
		Key:   fmt.Sprintf("%s.query_type.ptr", prefix),
		Value: fmt.Sprintf("%d", m.QueryType.PTR),
		Clock: timestamp,
	})
	
	// Other
	if m.QueryType.Other > 0 {
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.query_type.other", prefix),
			Value: fmt.Sprintf("%d", m.QueryType.Other),
			Clock: timestamp,
		})
	}
	
	return items
}

func mapCacheMetrics(m *metrics.Metrics, hostname, prefix string, timestamp int64) []ZabbixItem {
	var items []ZabbixItem
	
	// Cache hit rate (convert to percentage 0-100)
	if m.Cache != nil {
		cacheHitRate := m.Cache.HitRate * 100
		if !math.IsNaN(cacheHitRate) && !math.IsInf(cacheHitRate, 0) {
			items = append(items, ZabbixItem{
				Host:  hostname,
				Key:   fmt.Sprintf("%s.cache.hit_rate", prefix),
				Value: formatFloat(cacheHitRate),
				Clock: timestamp,
			})
		}
		
		// Cache efficiency score (0-100)
		cacheEfficiency := m.Cache.Efficiency * 100
		if !math.IsNaN(cacheEfficiency) && !math.IsInf(cacheEfficiency, 0) {
			items = append(items, ZabbixItem{
				Host:  hostname,
				Key:   fmt.Sprintf("%s.cache.efficiency", prefix),
				Value: formatFloat(cacheEfficiency),
				Clock: timestamp,
			})
		}
		
		// Average TTL
		if !math.IsNaN(m.Cache.AvgTTL) && !math.IsInf(m.Cache.AvgTTL, 0) {
			items = append(items, ZabbixItem{
				Host:  hostname,
				Key:   fmt.Sprintf("%s.cache.avg_ttl", prefix),
				Value: formatFloat(m.Cache.AvgTTL),
				Clock: timestamp,
			})
		}
		
		// Min TTL
		if !math.IsNaN(m.Cache.MinTTL) && !math.IsInf(m.Cache.MinTTL, 0) {
			items = append(items, ZabbixItem{
				Host:  hostname,
				Key:   fmt.Sprintf("%s.cache.min_ttl", prefix),
				Value: formatFloat(m.Cache.MinTTL),
				Clock: timestamp,
			})
		}
		
		// Max TTL
		if !math.IsNaN(m.Cache.MaxTTL) && !math.IsInf(m.Cache.MaxTTL, 0) {
			items = append(items, ZabbixItem{
				Host:  hostname,
				Key:   fmt.Sprintf("%s.cache.max_ttl", prefix),
				Value: formatFloat(m.Cache.MaxTTL),
				Clock: timestamp,
			})
		}
		
		// Total cacheable queries
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.cache.queries_total", prefix),
			Value: fmt.Sprintf("%d", m.Cache.TotalCacheableQueries),
			Clock: timestamp,
		})
		
		// Cache hits
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.cache.hits", prefix),
			Value: fmt.Sprintf("%d", m.Cache.Hits),
			Clock: timestamp,
		})
		
		// Cache misses
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.cache.misses", prefix),
			Value: fmt.Sprintf("%d", m.Cache.Misses),
			Clock: timestamp,
		})
	}
	
	return items
}

func mapNetworkMetrics(m *metrics.Metrics, hostname, prefix string, timestamp int64) []ZabbixItem {
	var items []ZabbixItem
	
	// Network metrics
	if m.Network != nil {
		// Packet loss (convert to percentage 0-100)
		packetLoss := m.Network.PacketLoss * 100
		if !math.IsNaN(packetLoss) && !math.IsInf(packetLoss, 0) {
			items = append(items, ZabbixItem{
				Host:  hostname,
				Key:   fmt.Sprintf("%s.network.packet_loss", prefix),
				Value: formatFloat(packetLoss),
				Clock: timestamp,
			})
		}
		
		// Network quality score (0-100)
		if !math.IsNaN(m.Network.QualityScore) && !math.IsInf(m.Network.QualityScore, 0) {
			items = append(items, ZabbixItem{
				Host:  hostname,
				Key:   fmt.Sprintf("%s.network.quality_score", prefix),
				Value: formatFloat(m.Network.QualityScore),
				Clock: timestamp,
			})
		}
		
		// Average jitter
		if !math.IsNaN(m.Network.AvgJitter) && !math.IsInf(m.Network.AvgJitter, 0) {
			items = append(items, ZabbixItem{
				Host:  hostname,
				Key:   fmt.Sprintf("%s.network.jitter_avg", prefix),
				Value: formatFloat(m.Network.AvgJitter),
				Clock: timestamp,
			})
		}
		
		// Network latency P50
		if !math.IsNaN(m.Network.LatencyP50) && !math.IsInf(m.Network.LatencyP50, 0) {
			items = append(items, ZabbixItem{
				Host:  hostname,
				Key:   fmt.Sprintf("%s.network.latency_p50", prefix),
				Value: formatFloat(m.Network.LatencyP50),
				Clock: timestamp,
			})
		}
		
		// Network latency P95
		if !math.IsNaN(m.Network.LatencyP95) && !math.IsInf(m.Network.LatencyP95, 0) {
			items = append(items, ZabbixItem{
				Host:  hostname,
				Key:   fmt.Sprintf("%s.network.latency_p95", prefix),
				Value: formatFloat(m.Network.LatencyP95),
				Clock: timestamp,
			})
		}
		
		// Network latency P99
		if !math.IsNaN(m.Network.LatencyP99) && !math.IsInf(m.Network.LatencyP99, 0) {
			items = append(items, ZabbixItem{
				Host:  hostname,
				Key:   fmt.Sprintf("%s.network.latency_p99", prefix),
				Value: formatFloat(m.Network.LatencyP99),
				Clock: timestamp,
			})
		}
		
		// Average hop count
		if m.Network.AvgHopCount > 0 {
			items = append(items, ZabbixItem{
				Host:  hostname,
				Key:   fmt.Sprintf("%s.network.hop_count_avg", prefix),
				Value: formatFloat(m.Network.AvgHopCount),
				Clock: timestamp,
			})
		}
		
		// Total packets sent
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.network.packets_sent", prefix),
			Value: fmt.Sprintf("%d", m.Network.PacketsSent),
			Clock: timestamp,
		})
		
		// Total packets received
		items = append(items, ZabbixItem{
			Host:  hostname,
			Key:   fmt.Sprintf("%s.network.packets_received", prefix),
			Value: fmt.Sprintf("%d", m.Network.PacketsReceived),
			Clock: timestamp,
		})
	}
	
	return items
}

func formatFloat(value float64) string {
	// Handle special cases
	if math.IsNaN(value) {
		return "0"
	}
	if math.IsInf(value, 1) {
		return "999999999"
	}
	if math.IsInf(value, -1) {
		return "-999999999"
	}
	
	// Format with reasonable precision
	s := fmt.Sprintf("%.6f", value)
	
	// Remove trailing zeros and decimal point if not needed
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	
	return s
}

func validateItemKey(key string) error {
	// Zabbix item key validation
	if len(key) == 0 {
		return fmt.Errorf("item key cannot be empty")
	}
	
	if len(key) > 255 {
		return fmt.Errorf("item key too long (max 255 characters)")
	}
	
	// Check for invalid characters
	invalidChars := []string{" ", "\t", "\n", "\r", "\"", "'", "`", "*", "?", "[", "]", "{", "}", "~", "$", "!", "&", ";", "(", ")", "<", ">", "|", "#", "@"}
	for _, char := range invalidChars {
		if strings.Contains(key, char) {
			return fmt.Errorf("item key contains invalid character: %s", char)
		}
	}
	
	return nil
}

func validateItemValue(value string) error {
	// Zabbix has a limit on value size
	if len(value) > 65535 {
		return fmt.Errorf("item value too long (max 65535 characters)")
	}
	
	return nil
}