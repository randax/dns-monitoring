package metrics

import (
	"time"

	"github.com/miekg/dns"
	internal_dns "github.com/randax/dns-monitoring/internal/dns"
)

func calculateThroughputMetrics(results []internal_dns.Result, start, end time.Time) ThroughputMetrics {
	if len(results) == 0 {
		return ThroughputMetrics{}
	}
	
	duration := end.Sub(start)
	if duration <= 0 {
		duration = time.Second
	}
	
	totalQueries := int64(len(results))
	durationSeconds := duration.Seconds()
	averageQPS := float64(totalQueries) / durationSeconds
	
	currentQPS := calculateCurrentQPS(results, 60)
	peakQPS := calculatePeakQPS(results, 1)
	
	byProtocol := calculateQPSByProtocol(results, duration)
	byServer := calculateQPSByServer(results, duration)
	
	return ThroughputMetrics{
		CurrentQPS:   currentQPS,
		AverageQPS:   averageQPS,
		PeakQPS:      peakQPS,
		TotalQueries: totalQueries,
		Duration:     duration,
		ByProtocol:   byProtocol,
		ByServer:     byServer,
	}
}

func calculateQueryTypeDistribution(results []internal_dns.Result) QueryTypeDistribution {
	dist := QueryTypeDistribution{}
	
	for _, r := range results {
		dist.Total++
		
		queryType := internal_dns.StringToQueryType(r.QueryType)
		switch queryType {
		case dns.TypeA:
			dist.A++
		case dns.TypeAAAA:
			dist.AAAA++
		case dns.TypeCNAME:
			dist.CNAME++
		case dns.TypeMX:
			dist.MX++
		case dns.TypeTXT:
			dist.TXT++
		case dns.TypeNS:
			dist.NS++
		case dns.TypeSOA:
			dist.SOA++
		case dns.TypePTR:
			dist.PTR++
		default:
			dist.Other++
		}
	}
	
	return dist
}

func calculateCurrentQPS(results []internal_dns.Result, windowSeconds int) float64 {
	if len(results) == 0 {
		return 0
	}
	
	now := time.Now()
	windowStart := now.Add(-time.Duration(windowSeconds) * time.Second)
	
	var count int
	for i := len(results) - 1; i >= 0; i-- {
		if results[i].Timestamp.Before(windowStart) {
			break
		}
		count++
	}
	
	if count == 0 {
		return 0
	}
	
	actualDuration := now.Sub(results[len(results)-1].Timestamp).Seconds()
	if actualDuration < 1 {
		actualDuration = 1
	}
	
	return float64(count) / actualDuration
}

func calculatePeakQPS(results []internal_dns.Result, windowSeconds int) float64 {
	if len(results) == 0 {
		return 0
	}
	
	buckets := make(map[int64]int)
	
	for _, r := range results {
		bucket := r.Timestamp.Unix() / int64(windowSeconds)
		buckets[bucket]++
	}
	
	var maxQPS float64
	for _, count := range buckets {
		qps := float64(count) / float64(windowSeconds)
		if qps > maxQPS {
			maxQPS = qps
		}
	}
	
	return maxQPS
}

func calculateQPSByProtocol(results []internal_dns.Result, duration time.Duration) map[string]float64 {
	protocolCounts := make(map[string]int)
	
	for _, r := range results {
		protocol := string(r.Protocol)
		if protocol == "" {
			protocol = "UDP"
		}
		protocolCounts[protocol]++
	}
	
	qpsByProtocol := make(map[string]float64)
	durationSeconds := duration.Seconds()
	
	for protocol, count := range protocolCounts {
		qpsByProtocol[protocol] = float64(count) / durationSeconds
	}
	
	return qpsByProtocol
}

func calculateQPSByServer(results []internal_dns.Result, duration time.Duration) map[string]float64 {
	serverCounts := make(map[string]int)
	
	for _, r := range results {
		server := r.Server
		if server == "" {
			server = "unknown"
		}
		serverCounts[server]++
	}
	
	qpsByServer := make(map[string]float64)
	durationSeconds := duration.Seconds()
	
	for server, count := range serverCounts {
		qpsByServer[server] = float64(count) / durationSeconds
	}
	
	return qpsByServer
}

func (q *QueryTypeDistribution) GetPercentages() map[string]float64 {
	if q.Total == 0 {
		return map[string]float64{}
	}
	
	total := float64(q.Total)
	return map[string]float64{
		"A":     float64(q.A) / total * 100,
		"AAAA":  float64(q.AAAA) / total * 100,
		"CNAME": float64(q.CNAME) / total * 100,
		"MX":    float64(q.MX) / total * 100,
		"TXT":   float64(q.TXT) / total * 100,
		"NS":    float64(q.NS) / total * 100,
		"SOA":   float64(q.SOA) / total * 100,
		"PTR":   float64(q.PTR) / total * 100,
		"Other": float64(q.Other) / total * 100,
	}
}

func calculateMovingAverageQPS(results []internal_dns.Result, windowSize time.Duration, sampleInterval time.Duration) []float64 {
	if len(results) == 0 {
		return []float64{}
	}
	
	start := results[0].Timestamp
	end := results[len(results)-1].Timestamp
	
	var movingAverage []float64
	
	for current := start; current.Before(end); current = current.Add(sampleInterval) {
		windowStart := current.Add(-windowSize)
		windowEnd := current
		
		var count int
		for _, r := range results {
			if r.Timestamp.After(windowStart) && r.Timestamp.Before(windowEnd) {
				count++
			}
		}
		
		qps := float64(count) / windowSize.Seconds()
		movingAverage = append(movingAverage, qps)
	}
	
	return movingAverage
}