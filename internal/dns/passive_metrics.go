package dns

import (
	"sync"
	"sync/atomic"
	"time"
)

type PassiveMetricsTracker interface {
	IncrementTotalPackets()
	IncrementDroppedPackets()
	RecordSuspensionEvent()
	RecordBufferResizeEvent()
	IncrementCurrentSuspensions()
	DecrementCurrentSuspensions()
	UpdateLastBackpressureTime()
	GetMetrics() PassiveMetrics
	GetPacketCounts() (total, dropped uint64)
}

type PassiveMetrics struct {
	TotalPackets         uint64
	DroppedPackets       uint64
	SuspensionEvents     uint64
	BufferResizeEvents   uint64
	CurrentSuspensions   int32
	LastBackpressureTime time.Time
	DropRatePercent      float64
}

type passiveMetricsTracker struct {
	totalPackets         atomic.Uint64
	droppedPackets       atomic.Uint64
	suspensionEvents     atomic.Uint64
	bufferResizeEvents   atomic.Uint64
	currentSuspensions   atomic.Int32
	
	mu                   sync.RWMutex
	lastBackpressureTime time.Time
}

func NewPassiveMetricsTracker() PassiveMetricsTracker {
	return &passiveMetricsTracker{}
}

func (mt *passiveMetricsTracker) IncrementTotalPackets() {
	mt.totalPackets.Add(1)
}

func (mt *passiveMetricsTracker) IncrementDroppedPackets() {
	mt.droppedPackets.Add(1)
	mt.UpdateLastBackpressureTime()
}

func (mt *passiveMetricsTracker) RecordSuspensionEvent() {
	mt.suspensionEvents.Add(1)
}

func (mt *passiveMetricsTracker) RecordBufferResizeEvent() {
	mt.bufferResizeEvents.Add(1)
}

func (mt *passiveMetricsTracker) IncrementCurrentSuspensions() {
	mt.currentSuspensions.Add(1)
}

func (mt *passiveMetricsTracker) DecrementCurrentSuspensions() {
	mt.currentSuspensions.Add(-1)
}

func (mt *passiveMetricsTracker) UpdateLastBackpressureTime() {
	mt.mu.Lock()
	mt.lastBackpressureTime = time.Now()
	mt.mu.Unlock()
}

func (mt *passiveMetricsTracker) GetMetrics() PassiveMetrics {
	total := mt.totalPackets.Load()
	dropped := mt.droppedPackets.Load()
	
	mt.mu.RLock()
	lastBackpressure := mt.lastBackpressureTime
	mt.mu.RUnlock()
	
	metrics := PassiveMetrics{
		TotalPackets:         total,
		DroppedPackets:       dropped,
		SuspensionEvents:     mt.suspensionEvents.Load(),
		BufferResizeEvents:   mt.bufferResizeEvents.Load(),
		CurrentSuspensions:   mt.currentSuspensions.Load(),
		LastBackpressureTime: lastBackpressure,
	}
	
	if total > 0 {
		metrics.DropRatePercent = float64(dropped) / float64(total) * 100
	}
	
	return metrics
}

func (mt *passiveMetricsTracker) GetPacketCounts() (total, dropped uint64) {
	return mt.totalPackets.Load(), mt.droppedPackets.Load()
}