package dns

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPassiveMetricsTracker_IncrementCounters(t *testing.T) {
	mt := NewPassiveMetricsTracker()
	
	// Test total packets increment
	mt.IncrementTotalPackets()
	mt.IncrementTotalPackets()
	mt.IncrementTotalPackets()
	
	metrics := mt.GetMetrics()
	assert.Equal(t, uint64(3), metrics.TotalPackets)
	
	// Test dropped packets increment
	mt.IncrementDroppedPackets()
	mt.IncrementDroppedPackets()
	
	metrics = mt.GetMetrics()
	assert.Equal(t, uint64(2), metrics.DroppedPackets)
	assert.InDelta(t, 66.67, metrics.DropRatePercent, 0.01)
}

func TestPassiveMetricsTracker_SuspensionTracking(t *testing.T) {
	mt := NewPassiveMetricsTracker()
	
	// Record suspension events
	mt.RecordSuspensionEvent()
	mt.RecordSuspensionEvent()
	mt.RecordSuspensionEvent()
	
	metrics := mt.GetMetrics()
	assert.Equal(t, uint64(3), metrics.SuspensionEvents)
	
	// Track current suspensions
	mt.IncrementCurrentSuspensions()
	mt.IncrementCurrentSuspensions()
	
	metrics = mt.GetMetrics()
	assert.Equal(t, int32(2), metrics.CurrentSuspensions)
	
	mt.DecrementCurrentSuspensions()
	
	metrics = mt.GetMetrics()
	assert.Equal(t, int32(1), metrics.CurrentSuspensions)
}

func TestPassiveMetricsTracker_BufferResizeEvents(t *testing.T) {
	mt := NewPassiveMetricsTracker()
	
	mt.RecordBufferResizeEvent()
	mt.RecordBufferResizeEvent()
	
	metrics := mt.GetMetrics()
	assert.Equal(t, uint64(2), metrics.BufferResizeEvents)
}

func TestPassiveMetricsTracker_LastBackpressureTime(t *testing.T) {
	mt := NewPassiveMetricsTracker()
	
	beforeUpdate := time.Now()
	time.Sleep(10 * time.Millisecond)
	
	mt.UpdateLastBackpressureTime()
	
	time.Sleep(10 * time.Millisecond)
	afterUpdate := time.Now()
	
	metrics := mt.GetMetrics()
	assert.True(t, metrics.LastBackpressureTime.After(beforeUpdate))
	assert.True(t, metrics.LastBackpressureTime.Before(afterUpdate))
}

func TestPassiveMetricsTracker_ConcurrentAccess(t *testing.T) {
	mt := NewPassiveMetricsTracker()
	
	var wg sync.WaitGroup
	iterations := 1000
	goroutines := 10
	
	// Concurrent increments
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				mt.IncrementTotalPackets()
				if j%10 == 0 {
					mt.IncrementDroppedPackets()
				}
				if j%50 == 0 {
					mt.RecordSuspensionEvent()
				}
				if j%100 == 0 {
					mt.RecordBufferResizeEvent()
				}
			}
		}()
	}
	
	// Concurrent reads
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = mt.GetMetrics()
				time.Sleep(time.Microsecond)
			}
		}()
	}
	
	wg.Wait()
	
	metrics := mt.GetMetrics()
	expectedTotal := uint64(goroutines * iterations)
	expectedDropped := uint64(goroutines * (iterations / 10))
	expectedSuspensions := uint64(goroutines * (iterations / 50))
	expectedResizes := uint64(goroutines * (iterations / 100))
	
	assert.Equal(t, expectedTotal, metrics.TotalPackets)
	assert.Equal(t, expectedDropped, metrics.DroppedPackets)
	assert.Equal(t, expectedSuspensions, metrics.SuspensionEvents)
	assert.Equal(t, expectedResizes, metrics.BufferResizeEvents)
}

func TestPassiveMetricsTracker_GetPacketCounts(t *testing.T) {
	mt := NewPassiveMetricsTracker()
	
	for i := 0; i < 100; i++ {
		mt.IncrementTotalPackets()
		if i%5 == 0 {
			mt.IncrementDroppedPackets()
		}
	}
	
	total, dropped := mt.GetPacketCounts()
	assert.Equal(t, uint64(100), total)
	assert.Equal(t, uint64(20), dropped)
}

func TestPassiveMetricsTracker_DropRateCalculation(t *testing.T) {
	tests := []struct {
		name            string
		totalPackets    int
		droppedPackets  int
		expectedRate    float64
	}{
		{
			name:           "no packets",
			totalPackets:   0,
			droppedPackets: 0,
			expectedRate:   0,
		},
		{
			name:           "no drops",
			totalPackets:   100,
			droppedPackets: 0,
			expectedRate:   0,
		},
		{
			name:           "10% drop rate",
			totalPackets:   100,
			droppedPackets: 10,
			expectedRate:   10,
		},
		{
			name:           "50% drop rate",
			totalPackets:   100,
			droppedPackets: 50,
			expectedRate:   50,
		},
		{
			name:           "100% drop rate",
			totalPackets:   100,
			droppedPackets: 100,
			expectedRate:   100,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mt := NewPassiveMetricsTracker()
			
			for i := 0; i < tt.totalPackets; i++ {
				mt.IncrementTotalPackets()
			}
			for i := 0; i < tt.droppedPackets; i++ {
				mt.IncrementDroppedPackets()
			}
			
			metrics := mt.GetMetrics()
			assert.InDelta(t, tt.expectedRate, metrics.DropRatePercent, 0.01)
		})
	}
}