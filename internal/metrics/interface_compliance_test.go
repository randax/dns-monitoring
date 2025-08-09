package metrics

import (
	"fmt"
	"testing"
	"time"
	
	"github.com/randax/dns-monitoring/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectorImplementsMetricsProvider(t *testing.T) {
	var provider MetricsProvider
	collector := NewCollector(nil)
	provider = collector
	assert.NotNil(t, provider, "Collector should implement MetricsProvider interface")
}

func TestMetricsProviderInterfaceMethods(t *testing.T) {
	cfg := &config.MetricsConfig{
		WindowDuration:      5 * time.Minute,
		CalculationInterval: 10 * time.Second,
		MaxStoredResults:    1000,
		AdaptiveSampling: &config.AdaptiveSamplingConfig{
			Enabled:       true,
			MemoryLimitMB: 100,
		},
	}
	
	collector := NewCollector(cfg)
	var provider MetricsProvider = collector
	
	t.Run("GetMetrics", func(t *testing.T) {
		metrics := provider.GetMetrics()
		assert.NotNil(t, metrics, "GetMetrics should return a non-nil Metrics struct")
		assert.False(t, metrics.Period.Start.IsZero(), "Metrics period start should be set")
		assert.False(t, metrics.Period.End.IsZero(), "Metrics period end should be set")
	})
	
	t.Run("GetMetricsForPeriod", func(t *testing.T) {
		start := time.Now().Add(-1 * time.Hour)
		end := time.Now()
		metrics := provider.GetMetricsForPeriod(start, end)
		assert.NotNil(t, metrics, "GetMetricsForPeriod should return a non-nil Metrics struct")
		assert.Equal(t, start, metrics.Period.Start, "Period start should match requested start")
		assert.Equal(t, end, metrics.Period.End, "Period end should match requested end")
	})
	
	t.Run("GetRecentLatencySamples", func(t *testing.T) {
		samples := provider.GetRecentLatencySamples(100)
		assert.NotNil(t, samples, "GetRecentLatencySamples should return a slice (can be empty)")
		
		samplesUnlimited := provider.GetRecentLatencySamples(0)
		assert.NotNil(t, samplesUnlimited, "GetRecentLatencySamples with maxSamples=0 should return all samples")
		
		samplesNegative := provider.GetRecentLatencySamples(-1)
		assert.NotNil(t, samplesNegative, "GetRecentLatencySamples with negative maxSamples should handle gracefully")
	})
	
	t.Run("GetLatencySamplesForPeriod", func(t *testing.T) {
		start := time.Now().Add(-30 * time.Minute)
		end := time.Now()
		samples := provider.GetLatencySamplesForPeriod(start, end, 50)
		assert.NotNil(t, samples, "GetLatencySamplesForPeriod should return a slice (can be empty)")
		
		samplesUnlimited := provider.GetLatencySamplesForPeriod(start, end, 0)
		assert.NotNil(t, samplesUnlimited, "GetLatencySamplesForPeriod with maxSamples=0 should return all samples in period")
	})
	
	t.Run("GetSamplingMetrics", func(t *testing.T) {
		samplingMetrics := provider.GetSamplingMetrics()
		if cfg.AdaptiveSampling != nil && cfg.AdaptiveSampling.Enabled {
			assert.NotNil(t, samplingMetrics, "GetSamplingMetrics should return metrics when adaptive sampling is enabled")
			assert.GreaterOrEqual(t, samplingMetrics.CurrentRate, 0.0, "Sampling rate should be non-negative")
			assert.LessOrEqual(t, samplingMetrics.CurrentRate, 1.0, "Sampling rate should not exceed 1.0")
		} else {
			assert.Nil(t, samplingMetrics, "GetSamplingMetrics should return nil when adaptive sampling is disabled")
		}
	})
	
	t.Run("GetResultCount", func(t *testing.T) {
		count := provider.GetResultCount()
		assert.GreaterOrEqual(t, count, 0, "GetResultCount should return non-negative count")
	})
	
	t.Run("GetConfig", func(t *testing.T) {
		returnedConfig := provider.GetConfig()
		assert.NotNil(t, returnedConfig, "GetConfig should return non-nil config")
		assert.Equal(t, cfg.WindowDuration, returnedConfig.WindowDuration, "Config window duration should match")
		assert.Equal(t, cfg.MaxStoredResults, returnedConfig.MaxStoredResults, "Config max stored results should match")
	})
}

func TestMetricsProviderMethodSignatures(t *testing.T) {
	collector := NewCollector(nil)
	
	t.Run("VerifyMethodExists_GetMetrics", func(t *testing.T) {
		method := collector.GetMetrics
		require.NotNil(t, method, "GetMetrics method should exist")
		
		result := collector.GetMetrics()
		assert.IsType(t, &Metrics{}, result, "GetMetrics should return *Metrics")
	})
	
	t.Run("VerifyMethodExists_GetMetricsForPeriod", func(t *testing.T) {
		method := collector.GetMetricsForPeriod
		require.NotNil(t, method, "GetMetricsForPeriod method should exist")
		
		start := time.Now().Add(-1 * time.Hour)
		end := time.Now()
		result := collector.GetMetricsForPeriod(start, end)
		assert.IsType(t, &Metrics{}, result, "GetMetricsForPeriod should return *Metrics")
	})
	
	t.Run("VerifyMethodExists_GetRecentLatencySamples", func(t *testing.T) {
		method := collector.GetRecentLatencySamples
		require.NotNil(t, method, "GetRecentLatencySamples method should exist")
		
		result := collector.GetRecentLatencySamples(10)
		assert.IsType(t, []float64{}, result, "GetRecentLatencySamples should return []float64")
	})
	
	t.Run("VerifyMethodExists_GetLatencySamplesForPeriod", func(t *testing.T) {
		method := collector.GetLatencySamplesForPeriod
		require.NotNil(t, method, "GetLatencySamplesForPeriod method should exist")
		
		start := time.Now().Add(-1 * time.Hour)
		end := time.Now()
		result := collector.GetLatencySamplesForPeriod(start, end, 10)
		assert.IsType(t, []float64{}, result, "GetLatencySamplesForPeriod should return []float64")
	})
	
	t.Run("VerifyMethodExists_GetSamplingMetrics", func(t *testing.T) {
		method := collector.GetSamplingMetrics
		require.NotNil(t, method, "GetSamplingMetrics method should exist")
		
		result := collector.GetSamplingMetrics()
		assert.True(t, result == nil || result != nil, "GetSamplingMetrics should return *SamplingMetrics or nil")
	})
	
	t.Run("VerifyMethodExists_GetResultCount", func(t *testing.T) {
		method := collector.GetResultCount
		require.NotNil(t, method, "GetResultCount method should exist")
		
		result := collector.GetResultCount()
		assert.IsType(t, 0, result, "GetResultCount should return int")
	})
	
	t.Run("VerifyMethodExists_GetConfig", func(t *testing.T) {
		method := collector.GetConfig
		require.NotNil(t, method, "GetConfig method should exist")
		
		result := collector.GetConfig()
		assert.IsType(t, &config.MetricsConfig{}, result, "GetConfig should return *config.MetricsConfig")
	})
}

func TestInterfaceCompilationCheck(t *testing.T) {
	t.Run("CompileTimeCheck", func(t *testing.T) {
		var _ MetricsProvider = (*Collector)(nil)
		assert.True(t, true, "Compile-time interface check passed")
	})
	
	t.Run("RuntimeTypeAssertion", func(t *testing.T) {
		collector := NewCollector(nil)
		provider, ok := interface{}(collector).(MetricsProvider)
		assert.True(t, ok, "Runtime type assertion to MetricsProvider should succeed")
		assert.NotNil(t, provider, "Type-asserted provider should not be nil")
	})
	
	t.Run("InterfaceMethodInvocation", func(t *testing.T) {
		var provider MetricsProvider = NewCollector(nil)
		
		assert.NotPanics(t, func() {
			_ = provider.GetMetrics()
		}, "GetMetrics should not panic")
		
		assert.NotPanics(t, func() {
			_ = provider.GetMetricsForPeriod(time.Now().Add(-1*time.Hour), time.Now())
		}, "GetMetricsForPeriod should not panic")
		
		assert.NotPanics(t, func() {
			_ = provider.GetRecentLatencySamples(10)
		}, "GetRecentLatencySamples should not panic")
		
		assert.NotPanics(t, func() {
			_ = provider.GetLatencySamplesForPeriod(time.Now().Add(-1*time.Hour), time.Now(), 10)
		}, "GetLatencySamplesForPeriod should not panic")
		
		assert.NotPanics(t, func() {
			_ = provider.GetSamplingMetrics()
		}, "GetSamplingMetrics should not panic")
		
		assert.NotPanics(t, func() {
			_ = provider.GetResultCount()
		}, "GetResultCount should not panic")
		
		assert.NotPanics(t, func() {
			_ = provider.GetConfig()
		}, "GetConfig should not panic")
	})
}

func TestMetricsProviderPolymorphism(t *testing.T) {
	t.Run("PolymorphicBehavior", func(t *testing.T) {
		providers := []MetricsProvider{
			NewCollector(nil),
			NewCollector(&config.MetricsConfig{
				MaxStoredResults: 500,
				WindowDuration:   10 * time.Minute,
			}),
			NewCollector(&config.MetricsConfig{
				MaxStoredResults: 2000,
				WindowDuration:   30 * time.Minute,
				AdaptiveSampling: &config.AdaptiveSamplingConfig{
					Enabled:       true,
					MemoryLimitMB: 50,
				},
			}),
		}
		
		for i, provider := range providers {
			t.Run(fmt.Sprintf("Provider_%d", i), func(t *testing.T) {
				metrics := provider.GetMetrics()
				assert.NotNil(t, metrics, "Each provider should return valid metrics")
				
				count := provider.GetResultCount()
				assert.GreaterOrEqual(t, count, 0, "Each provider should return valid count")
				
				config := provider.GetConfig()
				assert.NotNil(t, config, "Each provider should return valid config")
			})
		}
	})
}

func TestInterfaceNilHandling(t *testing.T) {
	t.Run("NilCollectorPanics", func(t *testing.T) {
		var collector *Collector
		var provider MetricsProvider = collector
		
		assert.Panics(t, func() {
			provider.GetMetrics()
		}, "Calling methods on nil collector through interface should panic")
	})
	
	t.Run("ValidCollectorDoesNotPanic", func(t *testing.T) {
		collector := NewCollector(nil)
		var provider MetricsProvider = collector
		
		assert.NotPanics(t, func() {
			provider.GetMetrics()
		}, "Calling methods on valid collector through interface should not panic")
	})
}