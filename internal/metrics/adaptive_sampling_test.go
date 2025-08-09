package metrics

import (
	"testing"
	"time"
	
	"github.com/randax/dns-monitoring/internal/config"
)

func TestNewAdaptiveSampler(t *testing.T) {
	tests := []struct {
		name   string
		config *config.AdaptiveSamplingConfig
	}{
		{
			name:   "default config",
			config: nil,
		},
		{
			name:   "custom config",
			config: &config.AdaptiveSamplingConfig{
				Enabled:          true,
				MinSampleRate:    0.01,
				MaxSampleRate:    0.5,
				TargetSampleRate: 0.2,
				MemoryLimitMB:    50,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sampler := NewAdaptiveSampler(tt.config)
			if sampler == nil {
				t.Fatal("Expected non-nil sampler")
			}
			
			if tt.config != nil {
				if sampler.minRate != tt.config.MinSampleRate {
					t.Errorf("MinRate = %f, want %f", sampler.minRate, tt.config.MinSampleRate)
				}
				if sampler.maxRate != tt.config.MaxSampleRate {
					t.Errorf("MaxRate = %f, want %f", sampler.maxRate, tt.config.MaxSampleRate)
				}
			}
		})
	}
}

func TestShouldSample(t *testing.T) {
	config := &config.AdaptiveSamplingConfig{
		Enabled:          true,
		MinSampleRate:    0.0,
		MaxSampleRate:    1.0,
		TargetSampleRate: 0.5,
		MemoryLimitMB:    100,
	}
	
	sampler := NewAdaptiveSampler(config)
	
	totalQueries := 10000
	sampled := 0
	
	for i := 0; i < totalQueries; i++ {
		if sampler.ShouldSample() {
			sampled++
		}
	}
	
	effectiveRate := float64(sampled) / float64(totalQueries)
	
	if effectiveRate < 0.45 || effectiveRate > 0.55 {
		t.Errorf("Effective sampling rate = %f, expected ~0.5", effectiveRate)
	}
	
	if sampler.queryCount.Load() != uint64(totalQueries) {
		t.Errorf("Query count = %d, expected %d", sampler.queryCount.Load(), totalQueries)
	}
	
	if sampler.sampleCount.Load() != uint64(sampled) {
		t.Errorf("Sample count = %d, expected %d", sampler.sampleCount.Load(), sampled)
	}
}

func TestAdaptiveSamplingRateAdjustment(t *testing.T) {
	cfg := &config.AdaptiveSamplingConfig{
		Enabled:          true,
		MinSampleRate:    0.001,
		MaxSampleRate:    1.0,
		TargetSampleRate: 0.1,
		MemoryLimitMB:    100,
		AdjustInterval:   1 * time.Second,
	}
	
	sampler := NewAdaptiveSampler(cfg)
	
	tests := []struct {
		name        string
		qps         float64
		wantMinRate float64
		wantMaxRate float64
	}{
		{
			name:        "low volume",
			qps:         5,
			wantMinRate: 0.9,
			wantMaxRate: 1.0,
		},
		{
			name:        "medium volume",
			qps:         100,
			wantMinRate: 0.3,
			wantMaxRate: 1.0,
		},
		{
			name:        "high volume",
			qps:         5000,
			wantMinRate: 0.001,
			wantMaxRate: 0.1,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sampler.lastAdjustment = time.Now().Add(-2 * time.Second)
			
			sampler.AdjustSamplingRate(tt.qps)
			
			if sampler.currentRate < tt.wantMinRate || sampler.currentRate > tt.wantMaxRate {
				t.Errorf("Adjusted rate = %f, want between %f and %f for QPS %f",
					sampler.currentRate, tt.wantMinRate, tt.wantMaxRate, tt.qps)
			}
		})
	}
}

func TestMemoryPressureAdjustment(t *testing.T) {
	config := &config.AdaptiveSamplingConfig{
		Enabled:                 true,
		MinSampleRate:           0.001,
		MaxSampleRate:           1.0,
		TargetSampleRate:        0.5,
		MemoryLimitMB:           100,
		MemoryPressureThreshold: 0.8,
	}
	
	sampler := NewAdaptiveSampler(config)
	
	sampler.UpdateMemoryUsage(uint64(90 * 1024 * 1024))
	
	baseRate := sampler.calculateOptimalRate(1000)
	
	if baseRate > 0.2 {
		t.Errorf("Under memory pressure, rate should be reduced: got %f", baseRate)
	}
	
	sampler.UpdateMemoryUsage(uint64(30 * 1024 * 1024))
	
	lowPressureRate := sampler.calculateOptimalRate(1000)
	
	if lowPressureRate <= baseRate {
		t.Errorf("With low memory pressure, rate should increase: got %f vs %f", 
			lowPressureRate, baseRate)
	}
}

func TestGetMetrics(t *testing.T) {
	config := DefaultAdaptiveSamplingConfig()
	sampler := NewAdaptiveSampler(config)
	
	for i := 0; i < 1000; i++ {
		sampler.ShouldSample()
	}
	
	sampler.UpdateMemoryUsage(50 * 1024 * 1024)
	
	metrics := sampler.GetMetrics()
	
	if metrics.QueriesPerSecond <= 0 {
		t.Error("Expected positive QPS in metrics")
	}
	
	if metrics.MemoryUsedBytes != 50*1024*1024 {
		t.Errorf("Memory used = %d, expected %d", metrics.MemoryUsedBytes, 50*1024*1024)
	}
	
	if metrics.MemoryLimitBytes != uint64(config.MemoryLimitMB*1024*1024) {
		t.Errorf("Memory limit = %d, expected %d", 
			metrics.MemoryLimitBytes, uint64(config.MemoryLimitMB*1024*1024))
	}
	
	if metrics.CurrentRate < 0 || metrics.CurrentRate > 1 {
		t.Errorf("Invalid current rate: %f", metrics.CurrentRate)
	}
}

func TestOptimalBufferSize(t *testing.T) {
	config := &config.AdaptiveSamplingConfig{
		Enabled:          true,
		MemoryLimitMB:    10,
		TargetSampleRate: 0.1,
	}
	
	sampler := NewAdaptiveSampler(config)
	
	size := sampler.GetOptimalBufferSize()
	
	if size < 1000 {
		t.Errorf("Buffer size too small: %d", size)
	}
	
	maxPossible := int(10 * 1024 * 1024 / 24)
	if size > maxPossible {
		t.Errorf("Buffer size exceeds memory limit: %d > %d", size, maxPossible)
	}
}

func TestSetMemoryLimit(t *testing.T) {
	sampler := NewAdaptiveSampler(nil)
	
	tests := []struct {
		input    float64
		expected uint64
	}{
		{0.5, 1 * 1024 * 1024},
		{50, 50 * 1024 * 1024},
		{20000, 10000 * 1024 * 1024},
		{-10, 1 * 1024 * 1024},
	}
	
	for _, tt := range tests {
		sampler.SetMemoryLimit(tt.input)
		if sampler.memoryLimit != tt.expected {
			t.Errorf("SetMemoryLimit(%f): got %d, want %d", 
				tt.input, sampler.memoryLimit, tt.expected)
		}
	}
}

func TestReset(t *testing.T) {
	sampler := NewAdaptiveSampler(nil)
	
	for i := 0; i < 100; i++ {
		sampler.ShouldSample()
	}
	
	sampler.UpdateMemoryUsage(1000)
	sampler.AdjustSamplingRate(100)
	
	sampler.Reset()
	
	if sampler.queryCount.Load() != 0 {
		t.Errorf("Query count not reset: %d", sampler.queryCount.Load())
	}
	
	if sampler.sampleCount.Load() != 0 {
		t.Errorf("Sample count not reset: %d", sampler.sampleCount.Load())
	}
	
	metrics := sampler.GetMetrics()
	if metrics.AdjustmentCount != 0 {
		t.Errorf("Metrics not reset: adjustment count = %d", metrics.AdjustmentCount)
	}
}

func BenchmarkShouldSample(b *testing.B) {
	sampler := NewAdaptiveSampler(&config.AdaptiveSamplingConfig{
		Enabled:          true,
		TargetSampleRate: 0.1,
	})
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			sampler.ShouldSample()
		}
	})
}

func BenchmarkAdjustSamplingRate(b *testing.B) {
	sampler := NewAdaptiveSampler(nil)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sampler.lastAdjustment = time.Now().Add(-time.Minute)
		sampler.AdjustSamplingRate(float64(i % 10000))
	}
}