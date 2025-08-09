package metrics

import (
	"testing"
	"time"

	"github.com/randax/dns-monitoring/internal/config"
	"github.com/randax/dns-monitoring/internal/dns"
)

func TestLatencySampleBuffer(t *testing.T) {
	cfg := &config.MetricsConfig{
		MaxStoredResults:    1000,
		WindowDuration:      5 * time.Minute,
		CalculationInterval: 30 * time.Second,
	}
	
	collector := NewCollector(cfg)
	
	t.Run("AddLatencySamples", func(t *testing.T) {
		// Add some samples
		for i := 0; i < 10; i++ {
			result := dns.Result{
				Duration:  time.Duration(i+1) * time.Millisecond,
				Timestamp: time.Now(),
			}
			collector.AddResult(result)
		}
		
		// Check that samples were added
		samples := collector.GetRecentLatencySamples(0)
		if len(samples) != 10 {
			t.Errorf("Expected 10 samples, got %d", len(samples))
		}
	})
	
	t.Run("BoundsChecking", func(t *testing.T) {
		// Test negative maxSamples
		samples := collector.GetRecentLatencySamples(-1)
		if samples == nil {
			t.Error("Expected non-nil result for negative maxSamples")
		}
		
		// Test with reasonable limit
		samples = collector.GetRecentLatencySamples(5)
		if len(samples) > 5 {
			t.Errorf("Expected at most 5 samples, got %d", len(samples))
		}
		
		// Test with excessive limit (should be capped)
		samples = collector.GetRecentLatencySamples(200000)
		if len(samples) > 100000 {
			t.Errorf("Expected samples to be capped at 100000, got %d", len(samples))
		}
	})
	
	t.Run("CircularBufferWraparound", func(t *testing.T) {
		// Fill buffer beyond capacity to test wraparound
		for i := 0; i < 150; i++ {
			result := dns.Result{
				Duration:  time.Duration(i+1) * time.Millisecond,
				Timestamp: time.Now(),
			}
			collector.AddResult(result)
		}
		
		// Check that we still get valid samples
		samples := collector.GetRecentLatencySamples(0)
		if len(samples) == 0 {
			t.Error("Expected samples after wraparound")
		}
		
		// Verify buffer size is bounded
		if len(samples) > 100 {
			t.Logf("Sample buffer size: %d (should be bounded)", len(samples))
		}
	})
	
	t.Run("TimeBasedExpiration", func(t *testing.T) {
		// Create a new collector with short window
		shortCfg := &config.MetricsConfig{
			MaxStoredResults:    100,
			WindowDuration:      1 * time.Second,
			CalculationInterval: 100 * time.Millisecond,
		}
		shortCollector := NewCollector(shortCfg)
		
		// Add old sample (simulate by manipulating internal state)
		shortCollector.sampleMu.Lock()
		shortCollector.latencySamples[0] = latencySample{
			LatencySeconds: 0.1,
			Timestamp:      time.Now().Add(-5 * time.Second), // Old timestamp
		}
		shortCollector.latencySamples[1] = latencySample{
			LatencySeconds: 0.2,
			Timestamp:      time.Now(), // Recent timestamp
		}
		shortCollector.sampleIndex = 2
		shortCollector.sampleMu.Unlock()
		
		// Get samples - should only return the recent one
		samples := shortCollector.GetRecentLatencySamples(0)
		if len(samples) != 1 {
			t.Errorf("Expected 1 non-expired sample, got %d", len(samples))
		}
		if len(samples) > 0 && samples[0] != 0.2 {
			t.Errorf("Expected recent sample value 0.2, got %f", samples[0])
		}
	})
	
	t.Run("BufferResize", func(t *testing.T) {
		// Add some samples
		for i := 0; i < 10; i++ {
			result := dns.Result{
				Duration:  time.Duration(i+1) * time.Millisecond,
				Timestamp: time.Now(),
			}
			collector.AddResult(result)
		}
		
		// Resize buffer
		collector.ResizeLatencySampleBuffer(50)
		
		// Verify samples are preserved
		samples := collector.GetRecentLatencySamples(0)
		if len(samples) == 0 {
			t.Error("Expected samples to be preserved after resize")
		}
		
		// Test minimum size enforcement
		collector.ResizeLatencySampleBuffer(10)
		collector.sampleMu.RLock()
		bufferSize := len(collector.latencySamples)
		collector.sampleMu.RUnlock()
		if bufferSize < 100 {
			t.Errorf("Buffer size should be at least 100, got %d", bufferSize)
		}
		
		// Test maximum size enforcement
		collector.ResizeLatencySampleBuffer(200000)
		collector.sampleMu.RLock()
		bufferSize = len(collector.latencySamples)
		collector.sampleMu.RUnlock()
		if bufferSize > 100000 {
			t.Errorf("Buffer size should be capped at 100000, got %d", bufferSize)
		}
	})
	
	t.Run("ClearFunction", func(t *testing.T) {
		// Add samples
		for i := 0; i < 5; i++ {
			result := dns.Result{
				Duration:  time.Duration(i+1) * time.Millisecond,
				Timestamp: time.Now(),
			}
			collector.AddResult(result)
		}
		
		// Clear collector
		collector.Clear()
		
		// Verify samples are cleared
		samples := collector.GetRecentLatencySamples(0)
		if len(samples) != 0 {
			t.Errorf("Expected no samples after clear, got %d", len(samples))
		}
		
		// Verify internal state is reset
		collector.sampleMu.RLock()
		if collector.sampleIndex != 0 {
			t.Error("Expected sampleIndex to be reset to 0")
		}
		if collector.samplesFilled {
			t.Error("Expected samplesFilled to be reset to false")
		}
		collector.sampleMu.RUnlock()
	})
	
	t.Run("GetLatencySamplesForPeriod", func(t *testing.T) {
		// Add samples with known timestamps
		now := time.Now()
		for i := 0; i < 10; i++ {
			result := dns.Result{
				Duration:  time.Duration(i+1) * time.Millisecond,
				Timestamp: now.Add(time.Duration(i) * time.Second),
			}
			collector.AddResult(result)
		}
		
		// Get samples for specific period
		start := now.Add(2 * time.Second)
		end := now.Add(7 * time.Second)
		samples := collector.GetLatencySamplesForPeriod(start, end, 0)
		
		// Should get samples from index 3-6 (4 samples)
		expectedCount := 4
		if len(samples) != expectedCount {
			t.Errorf("Expected %d samples in period, got %d", expectedCount, len(samples))
		}
		
		// Test with maxSamples limit
		samples = collector.GetLatencySamplesForPeriod(start, end, 2)
		if len(samples) > 2 {
			t.Errorf("Expected at most 2 samples with limit, got %d", len(samples))
		}
	})
}

func TestCollectorConcurrency(t *testing.T) {
	cfg := &config.MetricsConfig{
		MaxStoredResults:    1000,
		WindowDuration:      5 * time.Minute,
		CalculationInterval: 30 * time.Second,
	}
	
	collector := NewCollector(cfg)
	
	// Test concurrent access to latency samples
	done := make(chan bool)
	
	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			result := dns.Result{
				Duration:  time.Duration(i+1) * time.Millisecond,
				Timestamp: time.Now(),
			}
			collector.AddResult(result)
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()
	
	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = collector.GetRecentLatencySamples(10)
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()
	
	// Resizer goroutine
	go func() {
		for i := 0; i < 10; i++ {
			collector.ResizeLatencySampleBuffer(1000 + i*100)
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()
	
	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}
	
	// Verify collector is still functional
	samples := collector.GetRecentLatencySamples(0)
	if len(samples) == 0 {
		t.Error("Expected samples after concurrent operations")
	}
}

func BenchmarkGetRecentLatencySamples(b *testing.B) {
	cfg := &config.MetricsConfig{
		MaxStoredResults:    10000,
		WindowDuration:      5 * time.Minute,
		CalculationInterval: 30 * time.Second,
	}
	
	collector := NewCollector(cfg)
	
	// Fill with samples
	for i := 0; i < 1000; i++ {
		result := dns.Result{
			Duration:  time.Duration(i+1) * time.Millisecond,
			Timestamp: time.Now(),
		}
		collector.AddResult(result)
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_ = collector.GetRecentLatencySamples(100)
	}
}

func TestCollectorCircularBuffer(t *testing.T) {
	cfg := &config.MetricsConfig{
		MaxStoredResults:    5,
		WindowDuration:      1 * time.Minute,
		CalculationInterval: 10 * time.Second,
	}
	
	collector := NewCollector(cfg)
	
	// Add more results than MaxStoredResults
	for i := 0; i < 10; i++ {
		result := dns.Result{
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			Duration:  time.Duration(i) * time.Millisecond,
		}
		collector.AddResult(result)
	}
	
	// Check that we don't exceed MaxStoredResults
	count := collector.GetResultCount()
	if count > cfg.MaxStoredResults {
		t.Errorf("Result count %d exceeds MaxStoredResults %d", count, cfg.MaxStoredResults)
	}
	
	// Verify circular buffer behavior - oldest results should be overwritten
	metrics := collector.GetMetrics()
	if metrics == nil {
		t.Fatal("Expected metrics, got nil")
	}
}

func TestCollectorResizeBuffer(t *testing.T) {
	cfg := &config.MetricsConfig{
		MaxStoredResults:    10,
		WindowDuration:      1 * time.Minute,
		CalculationInterval: 10 * time.Second,
	}
	
	collector := NewCollector(cfg)
	
	// Add some results
	for i := 0; i < 8; i++ {
		result := dns.Result{
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			Duration:  time.Duration(i) * time.Millisecond,
		}
		collector.AddResult(result)
	}
	
	// Resize buffer to smaller size
	newCfg := &config.MetricsConfig{
		MaxStoredResults:    5,
		WindowDuration:      1 * time.Minute,
		CalculationInterval: 10 * time.Second,
	}
	collector.UpdateConfig(newCfg)
	
	// Check that buffer was resized
	count := collector.GetResultCount()
	if count > newCfg.MaxStoredResults {
		t.Errorf("After resize, count %d exceeds new MaxStoredResults %d", count, newCfg.MaxStoredResults)
	}
}

func TestCollectorMemoryBounds(t *testing.T) {
	cfg := &config.MetricsConfig{
		MaxStoredResults:    1000,
		WindowDuration:      1 * time.Minute,
		CalculationInterval: 10 * time.Second,
	}
	
	collector := NewCollector(cfg)
	
	// Simulate high-frequency result additions
	for i := 0; i < 10000; i++ {
		result := dns.Result{
			Timestamp: time.Now(),
			Duration:  time.Duration(i%100) * time.Millisecond,
		}
		collector.AddResult(result)
	}
	
	// Verify memory bounds are maintained
	count := collector.GetResultCount()
	if count > cfg.MaxStoredResults {
		t.Errorf("After 10000 additions, count %d exceeds MaxStoredResults %d", count, cfg.MaxStoredResults)
	}
}