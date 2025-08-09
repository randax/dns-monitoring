package dns

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
	"github.com/randax/dns-monitoring/internal/config"
)

type PassiveEngine struct {
	cfg             *config.Config
	resultChan      chan Result
	packetBuffer    chan gopacket.Packet
	matcher         QueryMatcher
	workers         int
	mu              sync.RWMutex
	running         bool
	cancel          context.CancelFunc
	handlers        []*pcap.Handle
	cacheAnalyzer   *CacheAnalyzer
	networkAnalyzer *NetworkAnalyzer
	
	// Simple metrics tracking
	totalPackets   uint64
	droppedPackets uint64
	matchedQueries uint64
}

func NewPassiveEngine(cfg *config.Config) (*PassiveEngine, error) {
	if !cfg.Passive.Enabled {
		return nil, fmt.Errorf("passive monitoring is not enabled")
	}

	// Use simple query matcher with transaction ID only
	matcher := NewSimpleQueryMatcher(cfg.Passive.MatchTimeout)
	
	pe := &PassiveEngine{
		cfg:          cfg,
		resultChan:   make(chan Result, cfg.Passive.BufferSize),
		packetBuffer: make(chan gopacket.Packet, cfg.Passive.BufferSize),
		matcher:      matcher,
		workers:      cfg.Passive.Workers,
		handlers:     make([]*pcap.Handle, 0),
	}
	
	// Initialize analyzers if enabled
	if cfg.Cache.Enabled {
		pe.cacheAnalyzer = NewCacheAnalyzer(&cfg.Cache)
	}
	
	if cfg.Network.Enabled {
		pe.networkAnalyzer = NewNetworkAnalyzer(cfg.Network.SampleSize)
	}
	
	return pe, nil
}

func (pe *PassiveEngine) Run(ctx context.Context) (<-chan Result, error) {
	pe.mu.Lock()
	if pe.running {
		pe.mu.Unlock()
		return nil, fmt.Errorf("passive engine is already running")
	}
	pe.running = true
	pe.mu.Unlock()

	// Create a cancellable context
	ctx, cancel := context.WithCancel(ctx)
	pe.cancel = cancel

	// Start packet capture for each interface
	for _, iface := range pe.cfg.Passive.Interfaces {
		handle, err := openInterface(iface, pe.cfg.Passive.BPF, pe.cfg.Passive.SnapLen)
		if err != nil {
			pe.shutdown()
			return nil, fmt.Errorf("failed to open interface %s: %w", iface, err)
		}
		pe.handlers = append(pe.handlers, handle)
		
		// Start capture goroutine for this interface
		go pe.captureFromInterface(ctx, handle)
	}

	var wg sync.WaitGroup

	// Start packet processing workers
	for i := 0; i < pe.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pe.processPackets(ctx)
		}()
	}

	// Start query matcher cleanup
	wg.Add(1)
	go func() {
		defer wg.Done()
		pe.matcher.CleanupExpired(ctx)
	}()

	// Cleanup goroutine
	go func() {
		<-ctx.Done()
		pe.shutdown()
		wg.Wait()
		close(pe.resultChan)
		close(pe.packetBuffer)
	}()

	return pe.resultChan, nil
}

func (pe *PassiveEngine) captureFromInterface(ctx context.Context, handle *pcap.Handle) {
	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	
	for {
		select {
		case <-ctx.Done():
			return
		case packet, ok := <-packetSource.Packets():
			if !ok {
				return
			}
			
			if packet == nil {
				continue
			}
			
			pe.mu.Lock()
			pe.totalPackets++
			pe.mu.Unlock()
			
			// Try to forward packet to processing buffer
			select {
			case pe.packetBuffer <- packet:
			case <-ctx.Done():
				return
			default:
				// Buffer full, drop packet
				pe.mu.Lock()
				pe.droppedPackets++
				pe.mu.Unlock()
			}
		}
	}
}


func (pe *PassiveEngine) processPackets(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case packet, ok := <-pe.packetBuffer:
			if !ok {
				return
			}

			// Simple packet processing without TCP reassembly
			result, isQuery, err := parseDNSPacket(packet)
			if err != nil {
				continue
			}

			if isQuery {
				pe.matcher.AddQuery(*result)
			} else {
				if matchedResult := pe.matcher.MatchResponse(*result); matchedResult != nil {
					pe.mu.Lock()
					pe.matchedQueries++
					pe.mu.Unlock()
					
					// Apply cache analysis if enabled
					if pe.cacheAnalyzer != nil {
						pe.cacheAnalyzer.AnalyzeResult(matchedResult, pe.cfg.Cache)
					}
					
					// Apply network analysis if enabled
					if pe.networkAnalyzer != nil {
						pe.networkAnalyzer.AnalyzeResult(matchedResult, pe.cfg.Network)
					}
					
					select {
					case pe.resultChan <- *matchedResult:
					case <-ctx.Done():
						return
					default:
					}
				}
			}
		}
	}
}

func (pe *PassiveEngine) shutdown() {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	if pe.cancel != nil {
		pe.cancel()
	}
	
	// Close all pcap handles
	for _, handle := range pe.handlers {
		handle.Close()
	}
	
	pe.running = false
}

