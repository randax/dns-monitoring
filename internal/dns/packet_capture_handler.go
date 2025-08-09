package dns

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
)

type PacketCaptureHandler interface {
	Start(ctx context.Context) error
	Stop()
	GetPacketChannel() <-chan gopacket.Packet
	GetActiveInterfaces() []string
	AddInterface(ifaceName string, bpfFilter string) error
	RemoveInterface(ifaceName string) error
	GetInterfaceStats(ifaceName string) (*InterfaceStats, error)
	GetAllStats() map[string]*InterfaceStats
}

type CaptureConfig struct {
	Interfaces     []string
	BPFFilter      string
	SnapLen        int
	BufferSize     int
	ErrorChannel   chan<- error
	OnPacketHooks  PacketHooks
	StatsInterval  time.Duration
}

type PacketHooks struct {
	OnPacketReceived func()
	OnPacketDropped  func()
}

type InterfaceStats struct {
	Interface        string
	PacketsReceived  uint64
	PacketsDropped   uint64
	PacketsIfDropped uint64
	PacketsProcessed uint64
	LastUpdate       time.Time
	Active           bool
}

type interfaceHandle struct {
	name      string
	handle    *pcap.Handle
	stats     *InterfaceStats
	cancel    context.CancelFunc
	running   atomic.Bool
	bpfFilter string
}

type packetCaptureHandler struct {
	mu              sync.RWMutex
	config          CaptureConfig
	interfaces      map[string]*interfaceHandle
	packetChan      chan gopacket.Packet
	statsUpdateChan chan statsUpdate
	running         atomic.Bool
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

type statsUpdate struct {
	interfaceName string
	stats         *CaptureStats
}

func NewPacketCaptureHandler(config CaptureConfig) PacketCaptureHandler {
	if config.BufferSize <= 0 {
		config.BufferSize = 10000
	}
	if config.SnapLen <= 0 {
		config.SnapLen = 65535
	}
	if config.StatsInterval <= 0 {
		config.StatsInterval = 5 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	pch := &packetCaptureHandler{
		config:          config,
		interfaces:      make(map[string]*interfaceHandle),
		packetChan:      make(chan gopacket.Packet, config.BufferSize),
		statsUpdateChan: make(chan statsUpdate, 100),
		ctx:             ctx,
		cancel:          cancel,
	}

	for _, ifaceName := range config.Interfaces {
		_ = pch.AddInterface(ifaceName, config.BPFFilter)
	}

	return pch
}

func (pch *packetCaptureHandler) Start(ctx context.Context) error {
	if pch.running.Swap(true) {
		return fmt.Errorf("packet capture is already running")
	}

	if err := checkPrivileges(); err != nil {
		pch.running.Store(false)
		return fmt.Errorf("insufficient privileges: %w", err)
	}

	pch.mu.RLock()
	interfaces := make([]*interfaceHandle, 0, len(pch.interfaces))
	for _, iface := range pch.interfaces {
		interfaces = append(interfaces, iface)
	}
	pch.mu.RUnlock()

	if len(interfaces) == 0 {
		if err := pch.autoDetectInterfaces(); err != nil {
			pch.running.Store(false)
			return fmt.Errorf("no interfaces configured and auto-detection failed: %w", err)
		}
	}

	pch.wg.Add(1)
	go pch.statsAggregator()

	for _, iface := range interfaces {
		if err := pch.startInterfaceCapture(iface); err != nil {
			pch.sendError(fmt.Sprintf("failed to start capture on %s: %v", iface.name, err))
		}
	}

	go func() {
		select {
		case <-ctx.Done():
			pch.Stop()
		case <-pch.ctx.Done():
		}
	}()

	return nil
}

func (pch *packetCaptureHandler) Stop() {
	if !pch.running.Swap(false) {
		return
	}

	pch.cancel()

	done := make(chan struct{})
	go func() {
		pch.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		pch.sendError("timeout waiting for capture to stop")
	}

	pch.mu.Lock()
	for _, iface := range pch.interfaces {
		if iface.handle != nil {
			iface.handle.Close()
		}
	}
	pch.mu.Unlock()

	close(pch.packetChan)
	close(pch.statsUpdateChan)
}

func (pch *packetCaptureHandler) GetPacketChannel() <-chan gopacket.Packet {
	return pch.packetChan
}

func (pch *packetCaptureHandler) GetActiveInterfaces() []string {
	pch.mu.RLock()
	defer pch.mu.RUnlock()

	interfaces := make([]string, 0, len(pch.interfaces))
	for name, iface := range pch.interfaces {
		if iface.running.Load() {
			interfaces = append(interfaces, name)
		}
	}
	return interfaces
}

func (pch *packetCaptureHandler) AddInterface(ifaceName string, bpfFilter string) error {
	pch.mu.Lock()
	defer pch.mu.Unlock()

	if _, exists := pch.interfaces[ifaceName]; exists {
		return fmt.Errorf("interface %s already added", ifaceName)
	}

	handle, err := openInterface(ifaceName, bpfFilter, pch.config.SnapLen)
	if err != nil {
		return fmt.Errorf("failed to open interface %s: %w", ifaceName, err)
	}

	_, cancel := context.WithCancel(pch.ctx)

	iface := &interfaceHandle{
		name:      ifaceName,
		handle:    handle,
		bpfFilter: bpfFilter,
		stats: &InterfaceStats{
			Interface:  ifaceName,
			LastUpdate: time.Now(),
			Active:     false,
		},
		cancel: cancel,
	}

	pch.interfaces[ifaceName] = iface

	if pch.running.Load() {
		if err := pch.startInterfaceCapture(iface); err != nil {
			delete(pch.interfaces, ifaceName)
			handle.Close()
			return err
		}
	}

	return nil
}

func (pch *packetCaptureHandler) RemoveInterface(ifaceName string) error {
	pch.mu.Lock()
	defer pch.mu.Unlock()

	iface, exists := pch.interfaces[ifaceName]
	if !exists {
		return fmt.Errorf("interface %s not found", ifaceName)
	}

	iface.cancel()

	timeout := time.After(5 * time.Second)
	for iface.running.Load() {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for interface %s to stop", ifaceName)
		case <-time.After(100 * time.Millisecond):
		}
	}

	iface.handle.Close()
	delete(pch.interfaces, ifaceName)

	return nil
}

func (pch *packetCaptureHandler) GetInterfaceStats(ifaceName string) (*InterfaceStats, error) {
	pch.mu.RLock()
	defer pch.mu.RUnlock()

	iface, exists := pch.interfaces[ifaceName]
	if !exists {
		return nil, fmt.Errorf("interface %s not found", ifaceName)
	}

	// Use atomic loads for fields that are atomically written
	statsCopy := InterfaceStats{
		Interface:        iface.stats.Interface,
		PacketsReceived:  atomic.LoadUint64(&iface.stats.PacketsReceived),
		PacketsDropped:   atomic.LoadUint64(&iface.stats.PacketsDropped),
		PacketsIfDropped: atomic.LoadUint64(&iface.stats.PacketsIfDropped),
		PacketsProcessed: atomic.LoadUint64(&iface.stats.PacketsProcessed),
		LastUpdate:       iface.stats.LastUpdate,
		Active:           iface.stats.Active,
	}
	return &statsCopy, nil
}

func (pch *packetCaptureHandler) GetAllStats() map[string]*InterfaceStats {
	pch.mu.RLock()
	defer pch.mu.RUnlock()

	stats := make(map[string]*InterfaceStats)
	for name, iface := range pch.interfaces {
		statsCopy := *iface.stats
		stats[name] = &statsCopy
	}
	return stats
}

func (pch *packetCaptureHandler) startInterfaceCapture(iface *interfaceHandle) error {
	if iface.running.Load() {
		return fmt.Errorf("interface %s already running", iface.name)
	}

	ctx, cancel := context.WithCancel(pch.ctx)
	iface.cancel = cancel

	pch.wg.Add(1)
	go pch.captureFromInterface(ctx, iface)

	return nil
}

func (pch *packetCaptureHandler) captureFromInterface(ctx context.Context, iface *interfaceHandle) {
	defer pch.wg.Done()

	iface.running.Store(true)
	iface.stats.Active = true
	defer func() {
		iface.running.Store(false)
		iface.stats.Active = false
	}()

	localChan := make(chan gopacket.Packet, 1000)
	errChan := make(chan error, 1)

	go func() {
		errChan <- capturePackets(ctx, iface.handle, localChan)
	}()

	ticker := time.NewTicker(pch.config.StatsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-errChan:
			if err != nil && err != context.Canceled {
				pch.sendError(fmt.Sprintf("error capturing from interface %s: %v", iface.name, err))
			}
			return
		case packet := <-localChan:
			atomic.AddUint64(&iface.stats.PacketsProcessed, 1)

			if pch.config.OnPacketHooks.OnPacketReceived != nil {
				pch.config.OnPacketHooks.OnPacketReceived()
			}

			select {
			case pch.packetChan <- packet:
			case <-ctx.Done():
				return
			default:
				atomic.AddUint64(&iface.stats.PacketsDropped, 1)
				if pch.config.OnPacketHooks.OnPacketDropped != nil {
					pch.config.OnPacketHooks.OnPacketDropped()
				}
			}
		case <-ticker.C:
			if stats, err := getCaptureStats(iface.handle); err == nil {
				select {
				case pch.statsUpdateChan <- statsUpdate{
					interfaceName: iface.name,
					stats:         stats,
				}:
				case <-ctx.Done():
					return
				default:
				}
			}
		}
	}
}

func (pch *packetCaptureHandler) statsAggregator() {
	defer pch.wg.Done()

	for {
		select {
		case <-pch.ctx.Done():
			return
		case update := <-pch.statsUpdateChan:
			pch.mu.RLock()
			iface, exists := pch.interfaces[update.interfaceName]
			pch.mu.RUnlock()

			if exists && update.stats != nil {
				atomic.StoreUint64(&iface.stats.PacketsReceived, uint64(update.stats.PacketsReceived))
				atomic.StoreUint64(&iface.stats.PacketsDropped, uint64(update.stats.PacketsDropped))
				atomic.StoreUint64(&iface.stats.PacketsIfDropped, uint64(update.stats.PacketsIfDropped))
				iface.stats.LastUpdate = time.Now()
			}
		}
	}
}

func (pch *packetCaptureHandler) autoDetectInterfaces() error {
	interfaces, err := getAvailableInterfaces()
	if err != nil {
		return err
	}

	for _, ifaceName := range interfaces {
		if err := pch.AddInterface(ifaceName, pch.config.BPFFilter); err != nil {
			pch.sendError(fmt.Sprintf("failed to add interface %s: %v", ifaceName, err))
		}
	}

	pch.mu.RLock()
	count := len(pch.interfaces)
	pch.mu.RUnlock()

	if count == 0 {
		return fmt.Errorf("no valid interfaces found")
	}

	return nil
}

func (pch *packetCaptureHandler) sendError(msg string) {
	if pch.config.ErrorChannel != nil {
		select {
		case pch.config.ErrorChannel <- fmt.Errorf(msg):
		default:
		}
	}
}