package metrics

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	
	"github.com/randax/dns-monitoring/internal/config"
)

type AdaptiveSampler struct {
	mu sync.RWMutex
	
	currentRate    float64
	targetRate     float64
	minRate        float64
	maxRate        float64
	
	queryCount     atomic.Uint64
	sampleCount    atomic.Uint64
	
	memoryLimit    uint64
	memoryUsed     atomic.Uint64
	
	lastAdjustment time.Time
	adjustInterval time.Duration
	
	windowStart    time.Time
	windowDuration time.Duration
	
	metrics        SamplingMetrics
}

func DefaultAdaptiveSamplingConfig() *config.AdaptiveSamplingConfig {
	return &config.AdaptiveSamplingConfig{
		Enabled:          true,
		MinSampleRate:    0.001,
		MaxSampleRate:    1.0,
		TargetSampleRate: 0.1,
		MemoryLimitMB:    100,
		AdjustInterval:   30 * time.Second,
		WindowDuration:   5 * time.Minute,
		HighVolumeQPS:    1000,
		LowVolumeQPS:     10,
		MemoryPressureThreshold: 0.8,
	}
}

func NewAdaptiveSampler(cfg *config.AdaptiveSamplingConfig) *AdaptiveSampler {
	if cfg == nil {
		cfg = DefaultAdaptiveSamplingConfig()
	}
	
	return &AdaptiveSampler{
		currentRate:    cfg.TargetSampleRate,
		targetRate:     cfg.TargetSampleRate,
		minRate:        cfg.MinSampleRate,
		maxRate:        cfg.MaxSampleRate,
		memoryLimit:    uint64(cfg.MemoryLimitMB * 1024 * 1024),
		lastAdjustment: time.Now(),
		adjustInterval: cfg.AdjustInterval,
		windowStart:    time.Now(),
		windowDuration: cfg.WindowDuration,
	}
}

func (s *AdaptiveSampler) ShouldSample() bool {
	s.queryCount.Add(1)
	
	s.mu.RLock()
	rate := s.currentRate
	s.mu.RUnlock()
	
	if rate >= 1.0 {
		s.sampleCount.Add(1)
		return true
	}
	
	if rate <= 0 {
		return false
	}
	
	shouldSample := fastRand() < rate
	if shouldSample {
		s.sampleCount.Add(1)
	}
	
	return shouldSample
}

func (s *AdaptiveSampler) AdjustSamplingRate(queryVolume float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if time.Since(s.lastAdjustment) < s.adjustInterval {
		return
	}
	
	oldRate := s.currentRate
	newRate := s.calculateOptimalRate(queryVolume)
	
	smoothingFactor := 0.3
	s.currentRate = oldRate*(1-smoothingFactor) + newRate*smoothingFactor
	
	if s.currentRate < s.minRate {
		s.currentRate = s.minRate
	}
	if s.currentRate > s.maxRate {
		s.currentRate = s.maxRate
	}
	
	s.lastAdjustment = time.Now()
	s.metrics.AdjustmentCount++
	s.metrics.LastAdjustment = time.Now()
	
	if elapsed := time.Since(s.windowStart); elapsed > s.windowDuration {
		s.queryCount.Store(0)
		s.sampleCount.Store(0)
		s.windowStart = time.Now()
	}
}

func (s *AdaptiveSampler) calculateOptimalRate(qps float64) float64 {
	memoryUsage := s.getMemoryUsage()
	memoryPressure := float64(memoryUsage) / float64(s.memoryLimit)
	
	targetSamplesPerSecond := 100.0
	if qps <= 10 {
		return 1.0
	} else if qps <= 100 {
		targetSamplesPerSecond = 50.0
	} else if qps <= 1000 {
		targetSamplesPerSecond = 100.0
	} else {
		targetSamplesPerSecond = 200.0
	}
	
	baseRate := targetSamplesPerSecond / qps
	if baseRate > 1.0 {
		baseRate = 1.0
	}
	
	if memoryPressure > 0.8 {
		pressureReduction := (memoryPressure - 0.8) * 2.5
		if pressureReduction > 0.9 {
			pressureReduction = 0.9
		}
		baseRate *= (1 - pressureReduction)
	} else if memoryPressure < 0.5 && baseRate < s.targetRate {
		increaseFacto := (0.5 - memoryPressure) * 0.5
		baseRate *= (1 + increaseFacto)
	}
	
	var systemMem runtime.MemStats
	runtime.ReadMemStats(&systemMem)
	systemPressure := float64(systemMem.Alloc) / float64(systemMem.Sys)
	if systemPressure > 0.9 {
		baseRate *= 0.5
	}
	
	return baseRate
}

func (s *AdaptiveSampler) UpdateMemoryUsage(bytes uint64) {
	s.memoryUsed.Store(bytes)
}

func (s *AdaptiveSampler) getMemoryUsage() uint64 {
	return s.memoryUsed.Load()
}

func (s *AdaptiveSampler) EstimateSampleMemory(sampleSize int) uint64 {
	const baseOverhead = 64
	const latencySampleSize = 24
	
	return uint64(sampleSize) * latencySampleSize + baseOverhead
}

func (s *AdaptiveSampler) GetMetrics() SamplingMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	queries := s.queryCount.Load()
	samples := s.sampleCount.Load()
	
	elapsed := time.Since(s.windowStart).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	
	effectiveRate := 0.0
	if queries > 0 {
		effectiveRate = float64(samples) / float64(queries)
	}
	
	return SamplingMetrics{
		CurrentRate:      s.currentRate,
		EffectiveRate:    effectiveRate,
		QueriesPerSecond: float64(queries) / elapsed,
		SamplesPerSecond: float64(samples) / elapsed,
		MemoryUsedBytes:  s.memoryUsed.Load(),
		MemoryLimitBytes: s.memoryLimit,
		DroppedSamples:   queries - samples,
		AdjustmentCount:  s.metrics.AdjustmentCount,
		LastAdjustment:   s.metrics.LastAdjustment,
	}
}

func (s *AdaptiveSampler) SetMemoryLimit(limitMB float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if limitMB < 1 {
		limitMB = 1
	}
	if limitMB > 10000 {
		limitMB = 10000
	}
	
	s.memoryLimit = uint64(limitMB * 1024 * 1024)
}

func (s *AdaptiveSampler) GetOptimalBufferSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	memoryPerSample := uint64(24)
	maxSamples := int(s.memoryLimit / memoryPerSample)
	
	qps := s.metrics.QueriesPerSecond
	if qps <= 0 {
		qps = 100
	}
	
	samplesPerMinute := int(qps * s.currentRate * 60)
	
	optimalSize := samplesPerMinute * 5
	
	if optimalSize < 1000 {
		optimalSize = 1000
	}
	if optimalSize > maxSamples {
		optimalSize = maxSamples
	}
	if optimalSize > 100000 {
		optimalSize = 100000
	}
	
	return optimalSize
}

var rngState uint64 = uint64(time.Now().UnixNano())

func fastRand() float64 {
	for {
		old := atomic.LoadUint64(&rngState)
		new := old*1664525 + 1013904223
		if atomic.CompareAndSwapUint64(&rngState, old, new) {
			return float64(new&0x7FFFFFFF) / float64(0x80000000)
		}
	}
}

func (s *AdaptiveSampler) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.queryCount.Store(0)
	s.sampleCount.Store(0)
	s.windowStart = time.Now()
	s.currentRate = s.targetRate
	s.metrics = SamplingMetrics{}
}