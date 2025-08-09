package dns

import (
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackpressureManager_IsSuspended(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*backpressureManager)
		expected bool
	}{
		{
			name: "not suspended initially",
			setup: func(bm *backpressureManager) {
				// No setup needed
			},
			expected: false,
		},
		{
			name: "suspended when set",
			setup: func(bm *backpressureManager) {
				bm.suspended = true
				bm.suspendUntil = time.Now().Add(1 * time.Hour)
			},
			expected: true,
		},
		{
			name: "not suspended after timeout",
			setup: func(bm *backpressureManager) {
				bm.suspended = true
				bm.suspendUntil = time.Now().Add(-1 * time.Second)
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bm := &backpressureManager{
				adaptiveThreshold: 10,
				currentBufferSize: 100,
				maxBufferSize:     400,
				minBufferSize:     50,
			}
			
			tt.setup(bm)
			result := bm.IsSuspended()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBackpressureManager_HandleDroppedPacket(t *testing.T) {
	var droppedCount int
	var suspensionStarted bool
	
	bm := NewBackpressureManager(BackpressureConfig{
		InitialBufferSize: 100,
		AdaptiveThreshold: 3,
		MetricsHooks: BackpressureMetricsHooks{
			OnPacketDropped: func() {
				droppedCount++
			},
			OnSuspensionStarted: func(duration time.Duration) {
				suspensionStarted = true
			},
		},
	})
	
	impl := bm.(*backpressureManager)
	
	// First drop
	bm.HandleDroppedPacket()
	assert.Equal(t, 1, droppedCount)
	assert.Equal(t, uint64(1), impl.dropCount)
	assert.Equal(t, 1, impl.consecutiveDrops)
	assert.False(t, suspensionStarted)
	
	// Second drop
	bm.HandleDroppedPacket()
	assert.Equal(t, 2, droppedCount)
	assert.Equal(t, 2, impl.consecutiveDrops)
	assert.False(t, suspensionStarted)
	
	// Third drop - should trigger suspension
	bm.HandleDroppedPacket()
	assert.Equal(t, 3, droppedCount)
	assert.Equal(t, 3, impl.consecutiveDrops)
	assert.True(t, suspensionStarted)
	assert.True(t, impl.suspended)
}

func TestBackpressureManager_TryResizeBuffer(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(*backpressureManager)
		bufferFullness int
		expectResize   bool
		expectedSize   int
	}{
		{
			name: "grow buffer under pressure",
			setup: func(bm *backpressureManager) {
				bm.consecutiveDrops = 10
				bm.currentBufferSize = 100
			},
			bufferFullness: 50,
			expectResize:   true,
			expectedSize:   200,
		},
		{
			name: "grow to max size",
			setup: func(bm *backpressureManager) {
				bm.consecutiveDrops = 10
				bm.currentBufferSize = 300
				bm.maxBufferSize = 400
			},
			bufferFullness: 50,
			expectResize:   true,
			expectedSize:   400,
		},
		{
			name: "no grow at max size",
			setup: func(bm *backpressureManager) {
				bm.consecutiveDrops = 10
				bm.currentBufferSize = 400
				bm.maxBufferSize = 400
				bm.lastDropTime = time.Now() // Recent drops, so no shrinking
			},
			bufferFullness: 50,
			expectResize:   false,
			expectedSize:   400,
		},
		{
			name: "shrink underutilized buffer",
			setup: func(bm *backpressureManager) {
				bm.consecutiveDrops = 0
				bm.currentBufferSize = 200
				bm.minBufferSize = 50
				bm.lastDropTime = time.Now().Add(-31 * time.Second)
			},
			bufferFullness: 10, // 10 out of 200 = 5% usage
			expectResize:   true,
			expectedSize:   100,
		},
		{
			name: "no shrink at min size",
			setup: func(bm *backpressureManager) {
				bm.consecutiveDrops = 0
				bm.currentBufferSize = 50
				bm.minBufferSize = 50
				bm.lastDropTime = time.Now().Add(-31 * time.Second)
			},
			bufferFullness: 10,
			expectResize:   false,
			expectedSize:   50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bm := &backpressureManager{
				adaptiveThreshold: 10,
				maxBufferSize:     400,
				minBufferSize:     50,
			}
			
			tt.setup(bm)
			
			// Create buffer with specified fullness
			// Use a fixed size for the test, not bm.currentBufferSize which changes
			initialSize := bm.currentBufferSize
			currentBuffer := make(chan gopacket.Packet, initialSize)
			for i := 0; i < tt.bufferFullness; i++ {
				currentBuffer <- gopacket.NewPacket([]byte{}, gopacket.DecodeFunc(func([]byte, gopacket.PacketBuilder) error { return nil }), gopacket.Default)
			}
			
			newBuffer := bm.TryResizeBuffer(currentBuffer)
			
			if tt.expectResize {
				assert.NotEqual(t, currentBuffer, newBuffer)
				assert.Equal(t, tt.expectedSize, cap(newBuffer))
				assert.Equal(t, tt.expectedSize, bm.currentBufferSize)
			} else {
				assert.Equal(t, currentBuffer, newBuffer)
				assert.Equal(t, tt.expectedSize, bm.currentBufferSize)
			}
		})
	}
}

func TestBackpressureManager_ResetConsecutiveDrops(t *testing.T) {
	bm := &backpressureManager{
		consecutiveDrops: 5,
	}
	
	bm.ResetConsecutiveDrops()
	assert.Equal(t, 0, bm.consecutiveDrops)
}

func TestBackpressureManager_GetStats(t *testing.T) {
	now := time.Now()
	bm := &backpressureManager{
		suspended:         true,
		currentBufferSize: 200,
		dropCount:         42,
		lastDropTime:      now,
		suspendUntil:      now.Add(1 * time.Hour),
	}
	
	stats := bm.GetStats()
	
	assert.True(t, stats.Suspended)
	assert.Equal(t, 200, stats.CurrentBufferSize)
	assert.Equal(t, uint64(42), stats.DropCount)
	assert.Equal(t, now, stats.LastDropTime)
	assert.Equal(t, now.Add(1*time.Hour), stats.SuspendUntil)
}

func TestBackpressureManager_ErrorChannelHandling(t *testing.T) {
	errorChan := make(chan error, 10)
	
	bm := NewBackpressureManager(BackpressureConfig{
		InitialBufferSize: 100,
		AdaptiveThreshold: 1,
		ErrorChannel:      errorChan,
	})
	
	// Trigger error by causing suspension
	bm.HandleDroppedPacket()
	
	// Should receive an error message
	select {
	case err := <-errorChan:
		require.NotNil(t, err)
		assert.Contains(t, err.Error(), "suspending packet capture")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected error message not received")
	}
}