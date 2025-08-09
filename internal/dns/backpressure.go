package dns

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/gopacket"
)

type BackpressureManager interface {
	IsSuspended() bool
	HandleDroppedPacket()
	ResetConsecutiveDrops()
	TryResizeBuffer(currentBuffer chan gopacket.Packet) chan gopacket.Packet
	GetStats() BackpressureStats
}

type BackpressureStats struct {
	Suspended         bool
	CurrentBufferSize int
	DropCount         uint64
	LastDropTime      time.Time
	SuspendUntil      time.Time
}

type backpressureManager struct {
	mu                sync.RWMutex
	dropCount         uint64
	lastDropTime      time.Time
	suspended         bool
	suspendUntil      time.Time
	consecutiveDrops  int
	adaptiveThreshold int
	currentBufferSize int
	maxBufferSize     int
	minBufferSize     int
	
	errorChan    chan<- error
	metricsHooks BackpressureMetricsHooks
}

type BackpressureMetricsHooks struct {
	OnPacketDropped      func()
	OnSuspensionStarted  func(duration time.Duration)
	OnSuspensionEnded    func()
	OnBufferResized      func(oldSize, newSize int)
}

type BackpressureConfig struct {
	InitialBufferSize  int
	AdaptiveThreshold  int
	MaxBufferSize      int
	MinBufferSize      int
	ErrorChannel       chan<- error
	MetricsHooks       BackpressureMetricsHooks
}

func NewBackpressureManager(config BackpressureConfig) BackpressureManager {
	if config.AdaptiveThreshold == 0 {
		config.AdaptiveThreshold = 10
	}
	if config.MaxBufferSize == 0 {
		config.MaxBufferSize = config.InitialBufferSize * 4
	}
	if config.MinBufferSize == 0 {
		config.MinBufferSize = config.InitialBufferSize / 2
	}
	
	return &backpressureManager{
		adaptiveThreshold: config.AdaptiveThreshold,
		currentBufferSize: config.InitialBufferSize,
		maxBufferSize:     config.MaxBufferSize,
		minBufferSize:     config.MinBufferSize,
		errorChan:         config.ErrorChannel,
		metricsHooks:      config.MetricsHooks,
	}
}

func (bm *backpressureManager) IsSuspended() bool {
	bm.mu.RLock()
	
	if !bm.suspended {
		bm.mu.RUnlock()
		return false
	}
	
	if time.Now().After(bm.suspendUntil) {
		// Upgrade to write lock to update state
		bm.mu.RUnlock()
		bm.mu.Lock()
		defer bm.mu.Unlock()
		
		// Double-check after acquiring write lock
		if bm.suspended && time.Now().After(bm.suspendUntil) {
			bm.suspended = false
			bm.consecutiveDrops = 0
			
			if bm.metricsHooks.OnSuspensionEnded != nil {
				bm.metricsHooks.OnSuspensionEnded()
			}
			
			bm.sendError("resuming packet capture after backpressure suspension")
			return false
		}
		
		return bm.suspended
	}
	
	bm.mu.RUnlock()
	return true
}

func (bm *backpressureManager) HandleDroppedPacket() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	
	now := time.Now()
	bm.dropCount++
	bm.lastDropTime = now
	bm.consecutiveDrops++
	
	if bm.metricsHooks.OnPacketDropped != nil {
		bm.metricsHooks.OnPacketDropped()
	}
	
	if bm.consecutiveDrops >= bm.adaptiveThreshold && !bm.suspended {
		bm.suspendCapture()
	}
	
	if bm.dropCount%100 == 0 {
		bm.sendError(fmt.Sprintf("packet buffer full, dropped %d packets total", bm.dropCount))
	}
}

func (bm *backpressureManager) ResetConsecutiveDrops() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	
	if bm.consecutiveDrops > 0 {
		bm.consecutiveDrops = 0
		
		if bm.currentBufferSize > bm.minBufferSize && time.Since(bm.lastDropTime) > 30*time.Second {
			// Buffer shrinking is handled by TryResizeBuffer when appropriate
		}
	}
}

func (bm *backpressureManager) TryResizeBuffer(currentBuffer chan gopacket.Packet) chan gopacket.Packet {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	
	currentSize := bm.currentBufferSize
	bufferUsage := float64(len(currentBuffer)) / float64(cap(currentBuffer))
	
	// Grow buffer if under pressure
	if bm.consecutiveDrops >= bm.adaptiveThreshold && currentSize < bm.maxBufferSize {
		newSize := currentSize * 2
		if newSize > bm.maxBufferSize {
			newSize = bm.maxBufferSize
		}
		
		newBuffer := make(chan gopacket.Packet, newSize)
		bm.transferBuffer(currentBuffer, newBuffer)
		
		bm.currentBufferSize = newSize
		bm.consecutiveDrops = 0
		
		if bm.metricsHooks.OnBufferResized != nil {
			bm.metricsHooks.OnBufferResized(currentSize, newSize)
		}
		
		bm.sendError(fmt.Sprintf("increased buffer size from %d to %d due to backpressure", currentSize, newSize))
		return newBuffer
	}
	
	// Shrink buffer if underutilized
	if bufferUsage < 0.25 && time.Since(bm.lastDropTime) > 30*time.Second && currentSize > bm.minBufferSize {
		newSize := currentSize / 2
		if newSize < bm.minBufferSize {
			newSize = bm.minBufferSize
		}
		
		if newSize < currentSize && len(currentBuffer) <= newSize {
			newBuffer := make(chan gopacket.Packet, newSize)
			bm.transferBuffer(currentBuffer, newBuffer)
			
			bm.currentBufferSize = newSize
			
			if bm.metricsHooks.OnBufferResized != nil {
				bm.metricsHooks.OnBufferResized(currentSize, newSize)
			}
			
			bm.sendError(fmt.Sprintf("reduced buffer size from %d to %d due to low utilization", currentSize, newSize))
			return newBuffer
		}
	}
	
	return currentBuffer
}

func (bm *backpressureManager) GetStats() BackpressureStats {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	
	return BackpressureStats{
		Suspended:         bm.suspended,
		CurrentBufferSize: bm.currentBufferSize,
		DropCount:         bm.dropCount,
		LastDropTime:      bm.lastDropTime,
		SuspendUntil:      bm.suspendUntil,
	}
}

func (bm *backpressureManager) suspendCapture() {
	bufferFullness := 0.75 // Assume high fullness when suspending
	
	baseDuration := time.Duration(10+int(990*bufferFullness)) * time.Millisecond
	
	suspensionDuration := baseDuration
	if suspensionDuration > 10*time.Second {
		suspensionDuration = 10 * time.Second
	}
	
	bm.suspended = true
	bm.suspendUntil = time.Now().Add(suspensionDuration)
	
	if bm.metricsHooks.OnSuspensionStarted != nil {
		bm.metricsHooks.OnSuspensionStarted(suspensionDuration)
	}
	
	bm.sendError(fmt.Sprintf("suspending packet capture for %v due to sustained backpressure", suspensionDuration))
}

func (bm *backpressureManager) transferBuffer(oldBuffer, newBuffer chan gopacket.Packet) {
	go func() {
		close(oldBuffer)
		for packet := range oldBuffer {
			select {
			case newBuffer <- packet:
			default:
				if bm.metricsHooks.OnPacketDropped != nil {
					bm.metricsHooks.OnPacketDropped()
				}
			}
		}
	}()
}

func (bm *backpressureManager) sendError(msg string) {
	if bm.errorChan != nil {
		select {
		case bm.errorChan <- fmt.Errorf(msg):
		default:
		}
	}
}