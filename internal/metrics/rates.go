package metrics

import (
	"github.com/miekg/dns"
	internal_dns "github.com/randax/dns-monitoring/internal/dns"
)

func calculateRateMetrics(results []internal_dns.Result) RateMetrics {
	if len(results) == 0 {
		return RateMetrics{}
	}
	
	var successQueries, errorQueries, networkErrors int64
	totalQueries := int64(len(results))
	
	for _, r := range results {
		if r.Error != nil {
			networkErrors++
			errorQueries++
		} else if r.ResponseCode == dns.RcodeSuccess {
			successQueries++
		} else {
			errorQueries++
		}
	}
	
	successRate := float64(successQueries) / float64(totalQueries) * 100
	errorRate := float64(errorQueries) / float64(totalQueries) * 100
	
	return RateMetrics{
		SuccessRate:    successRate,
		ErrorRate:      errorRate,
		TotalQueries:   totalQueries,
		SuccessQueries: successQueries,
		ErrorQueries:   errorQueries,
		NetworkErrors:  networkErrors,
	}
}

func calculateResponseCodeDistribution(results []internal_dns.Result) ResponseCodeDistribution {
	dist := ResponseCodeDistribution{}
	
	for _, r := range results {
		dist.Total++
		
		if r.Error != nil {
			dist.Other++
			continue
		}
		
		switch r.ResponseCode {
		case dns.RcodeSuccess:
			dist.NoError++
		case dns.RcodeNameError:
			dist.NXDomain++
		case dns.RcodeServerFailure:
			dist.ServFail++
		case dns.RcodeFormatError:
			dist.FormErr++
		case dns.RcodeNotImplemented:
			dist.NotImpl++
		case dns.RcodeRefused:
			dist.Refused++
		default:
			dist.Other++
		}
	}
	
	return dist
}

func (r *ResponseCodeDistribution) GetPercentages() map[string]float64 {
	if r.Total == 0 {
		return map[string]float64{}
	}
	
	total := float64(r.Total)
	return map[string]float64{
		"NoError":  float64(r.NoError) / total * 100,
		"NXDomain": float64(r.NXDomain) / total * 100,
		"ServFail": float64(r.ServFail) / total * 100,
		"FormErr":  float64(r.FormErr) / total * 100,
		"NotImpl":  float64(r.NotImpl) / total * 100,
		"Refused":  float64(r.Refused) / total * 100,
		"Other":    float64(r.Other) / total * 100,
	}
}

func calculateRateMetricsForWindow(results []internal_dns.Result, windowStart, windowEnd int64) RateMetrics {
	var filtered []internal_dns.Result
	
	for _, r := range results {
		timestamp := r.Timestamp.Unix()
		if timestamp >= windowStart && timestamp <= windowEnd {
			filtered = append(filtered, r)
		}
	}
	
	return calculateRateMetrics(filtered)
}

func calculateMovingAverageRate(results []internal_dns.Result, windowSizeSeconds int) float64 {
	if len(results) == 0 {
		return 0
	}
	
	latestTime := results[len(results)-1].Timestamp.Unix()
	windowStart := latestTime - int64(windowSizeSeconds)
	
	var count int
	for i := len(results) - 1; i >= 0; i-- {
		if results[i].Timestamp.Unix() < windowStart {
			break
		}
		count++
	}
	
	return float64(count) / float64(windowSizeSeconds)
}

func isSuccessResponse(code int) bool {
	return code == dns.RcodeSuccess
}

func isErrorResponse(code int) bool {
	return code != dns.RcodeSuccess
}

func categorizeResponseCode(code int) string {
	switch code {
	case dns.RcodeSuccess:
		return "Success"
	case dns.RcodeNameError:
		return "NXDomain"
	case dns.RcodeServerFailure:
		return "ServerFailure"
	case dns.RcodeFormatError:
		return "FormatError"
	case dns.RcodeNotImplemented:
		return "NotImplemented"
	case dns.RcodeRefused:
		return "Refused"
	default:
		return "Other"
	}
}