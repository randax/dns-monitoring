package metrics

import (
	"fmt"
	"time"

	internal_dns "github.com/randax/dns-monitoring/internal/dns"
)

func FilterResultsByTimeWindow(results []internal_dns.Result, start, end time.Time) []internal_dns.Result {
	var filtered []internal_dns.Result
	
	for _, r := range results {
		if r.Timestamp.After(start) && r.Timestamp.Before(end) {
			filtered = append(filtered, r)
		}
	}
	
	return filtered
}

func FilterResultsByDuration(results []internal_dns.Result, duration time.Duration) []internal_dns.Result {
	if len(results) == 0 {
		return results
	}
	
	end := time.Now()
	start := end.Add(-duration)
	
	return FilterResultsByTimeWindow(results, start, end)
}

func GroupResultsBySecond(results []internal_dns.Result) map[int64][]internal_dns.Result {
	grouped := make(map[int64][]internal_dns.Result)
	
	for _, r := range results {
		second := r.Timestamp.Unix()
		grouped[second] = append(grouped[second], r)
	}
	
	return grouped
}

func GroupResultsByMinute(results []internal_dns.Result) map[int64][]internal_dns.Result {
	grouped := make(map[int64][]internal_dns.Result)
	
	for _, r := range results {
		minute := r.Timestamp.Unix() / 60
		grouped[minute] = append(grouped[minute], r)
	}
	
	return grouped
}

func CalculateMovingAverage(values []float64, windowSize int) []float64 {
	if len(values) <= windowSize {
		return values
	}
	
	result := make([]float64, 0, len(values)-windowSize+1)
	
	var windowSum float64
	for i := 0; i < windowSize; i++ {
		windowSum += values[i]
	}
	result = append(result, windowSum/float64(windowSize))
	
	for i := windowSize; i < len(values); i++ {
		windowSum = windowSum - values[i-windowSize] + values[i]
		result = append(result, windowSum/float64(windowSize))
	}
	
	return result
}

func CalculateExponentialMovingAverage(values []float64, alpha float64) []float64 {
	if len(values) == 0 {
		return []float64{}
	}
	
	ema := make([]float64, len(values))
	ema[0] = values[0]
	
	for i := 1; i < len(values); i++ {
		ema[i] = alpha*values[i] + (1-alpha)*ema[i-1]
	}
	
	return ema
}

func FormatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.2fµs", float64(d.Nanoseconds())/1000)
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1e6)
	}
	if d < time.Minute {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.2fm", d.Minutes())
	}
	return fmt.Sprintf("%.2fh", d.Hours())
}

func FormatPercentage(value float64) string {
	return fmt.Sprintf("%.2f%%", value)
}

func FormatQPS(qps float64) string {
	if qps < 1 {
		return fmt.Sprintf("%.3f qps", qps)
	}
	if qps < 1000 {
		return fmt.Sprintf("%.1f qps", qps)
	}
	if qps < 1000000 {
		return fmt.Sprintf("%.1fK qps", qps/1000)
	}
	return fmt.Sprintf("%.1fM qps", qps/1000000)
}

func ValidateMetricsConfig(config *MetricsConfig) error {
	if config == nil {
		return fmt.Errorf("metrics config is nil")
	}
	
	if config.WindowDuration <= 0 {
		return fmt.Errorf("window duration must be positive")
	}
	
	if config.MaxStoredResults <= 0 {
		return fmt.Errorf("max stored results must be positive")
	}
	
	if config.CalculationInterval <= 0 {
		return fmt.Errorf("calculation interval must be positive")
	}
	
	if config.PercentilePrecision < 0 || config.PercentilePrecision > 10 {
		return fmt.Errorf("percentile precision must be between 0 and 10")
	}
	
	return nil
}

func MergeMetrics(metrics ...*Metrics) *Metrics {
	if len(metrics) == 0 {
		return &Metrics{}
	}
	
	if len(metrics) == 1 {
		return metrics[0]
	}
	
	merged := &Metrics{
		Period: MetricsPeriod{
			Start: metrics[0].Period.Start,
			End:   metrics[0].Period.End,
		},
	}
	
	for _, m := range metrics {
		if m.Period.Start.Before(merged.Period.Start) {
			merged.Period.Start = m.Period.Start
		}
		if m.Period.End.After(merged.Period.End) {
			merged.Period.End = m.Period.End
		}
	}
	
	var totalQueries int64
	var totalSuccess int64
	var totalErrors int64
	
	for _, m := range metrics {
		totalQueries += m.Rates.TotalQueries
		totalSuccess += m.Rates.SuccessQueries
		totalErrors += m.Rates.ErrorQueries
		
		merged.ResponseCode.NoError += m.ResponseCode.NoError
		merged.ResponseCode.NXDomain += m.ResponseCode.NXDomain
		merged.ResponseCode.ServFail += m.ResponseCode.ServFail
		merged.ResponseCode.FormErr += m.ResponseCode.FormErr
		merged.ResponseCode.NotImpl += m.ResponseCode.NotImpl
		merged.ResponseCode.Refused += m.ResponseCode.Refused
		merged.ResponseCode.Other += m.ResponseCode.Other
		merged.ResponseCode.Total += m.ResponseCode.Total
		
		merged.QueryType.A += m.QueryType.A
		merged.QueryType.AAAA += m.QueryType.AAAA
		merged.QueryType.CNAME += m.QueryType.CNAME
		merged.QueryType.MX += m.QueryType.MX
		merged.QueryType.TXT += m.QueryType.TXT
		merged.QueryType.NS += m.QueryType.NS
		merged.QueryType.SOA += m.QueryType.SOA
		merged.QueryType.PTR += m.QueryType.PTR
		merged.QueryType.Other += m.QueryType.Other
		merged.QueryType.Total += m.QueryType.Total
	}
	
	if totalQueries > 0 {
		merged.Rates.TotalQueries = totalQueries
		merged.Rates.SuccessQueries = totalSuccess
		merged.Rates.ErrorQueries = totalErrors
		merged.Rates.SuccessRate = float64(totalSuccess) / float64(totalQueries) * 100
		merged.Rates.ErrorRate = float64(totalErrors) / float64(totalQueries) * 100
	}
	
	return merged
}