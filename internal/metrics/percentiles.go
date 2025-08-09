package metrics

import (
	"math"
	"sort"
	"time"

	"github.com/randax/dns-monitoring/internal/dns"
)

func calculateLatencyMetrics(results []dns.Result) LatencyMetrics {
	if len(results) == 0 {
		return LatencyMetrics{}
	}
	
	latencies := make([]float64, 0, len(results))
	var sum float64
	var min, max float64 = math.MaxFloat64, 0
	
	for _, r := range results {
		latencyMs := float64(r.Duration.Milliseconds())
		latencies = append(latencies, latencyMs)
		sum += latencyMs
		
		if latencyMs < min {
			min = latencyMs
		}
		if latencyMs > max {
			max = latencyMs
		}
	}
	
	if len(latencies) == 0 {
		return LatencyMetrics{}
	}
	
	sort.Float64s(latencies)
	
	return LatencyMetrics{
		P50:  calculatePercentile(latencies, 50),
		P95:  calculatePercentile(latencies, 95),
		P99:  calculatePercentile(latencies, 99),
		P999: calculatePercentile(latencies, 99.9),
		Min:  min,
		Max:  max,
		Mean: sum / float64(len(latencies)),
	}
}

func calculatePercentile(sortedData []float64, percentile float64) float64 {
	if len(sortedData) == 0 {
		return 0
	}
	
	if percentile < 0 {
		percentile = 0
	} else if percentile > 100 {
		percentile = 100
	}
	
	if len(sortedData) == 1 {
		return sortedData[0]
	}
	
	if percentile == 100 {
		return sortedData[len(sortedData)-1]
	}
	
	rank := percentile / 100.0 * float64(len(sortedData)-1)
	
	lowerIndex := int(math.Floor(rank))
	upperIndex := int(math.Ceil(rank))
	
	if lowerIndex == upperIndex {
		return sortedData[lowerIndex]
	}
	
	lowerValue := sortedData[lowerIndex]
	upperValue := sortedData[upperIndex]
	
	interpolation := rank - float64(lowerIndex)
	return lowerValue + interpolation*(upperValue-lowerValue)
}

func calculatePercentiles(sortedData []float64, percentiles ...float64) []float64 {
	results := make([]float64, len(percentiles))
	
	for i, p := range percentiles {
		results[i] = calculatePercentile(sortedData, p)
	}
	
	return results
}

func durationToMilliseconds(d time.Duration) float64 {
	return float64(d.Nanoseconds()) / 1e6
}

func millisecondsToNanoseconds(ms float64) int64 {
	return int64(ms * 1e6)
}

func calculateStandardDeviation(data []float64, mean float64) float64 {
	if len(data) <= 1 {
		return 0
	}
	
	var sumSquares float64
	for _, v := range data {
		diff := v - mean
		sumSquares += diff * diff
	}
	
	variance := sumSquares / float64(len(data)-1)
	return math.Sqrt(variance)
}

func calculateMedian(sortedData []float64) float64 {
	return calculatePercentile(sortedData, 50)
}

func calculateInterquartileRange(sortedData []float64) float64 {
	if len(sortedData) < 4 {
		return 0
	}
	
	q1 := calculatePercentile(sortedData, 25)
	q3 := calculatePercentile(sortedData, 75)
	
	return q3 - q1
}