package dns

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/tcpassembly"
	"github.com/google/gopacket/tcpassembly/tcpreader"
	mdns "github.com/miekg/dns"
)

type ExtraPacketData struct {
	TransactionID uint16
	SourceIP      string
	SourcePort    uint16
	DestIP        string
	DestPort      uint16
}

type TCPStreamReassembler struct {
	assembler      *tcpassembly.Assembler
	streamFactory  *dnsStreamFactory
	bufferManager  *TCPBufferManager
	mu             sync.RWMutex
	flushInterval  time.Duration
	stopChan       chan struct{}
	wg             sync.WaitGroup
	// Resource limits
	maxStreams     int
	maxBufferSize  int
	currentStreams int32
	totalMemory    int64
}

type TCPBufferManager struct {
	buffers       map[string]*StreamBuffer
	mu            sync.RWMutex
	maxBuffers    int
	maxBufferSize int
	totalSize     int64
}

type StreamBuffer struct {
	buffer       []byte
	lastActivity time.Time
	flow         gopacket.Flow
	transport    gopacket.Flow
	size         int
}

type dnsStreamFactory struct {
	bufferManager *TCPBufferManager
	handler       DNSPacketHandler
	reassembler   *TCPStreamReassembler
}

type DNSPacketHandler func(*Result, bool, error)

// TCPReassemblyConfig contains configuration for TCP stream reassembly
// NOTE: TCP reassembly is disabled for the initial implementation
type TCPReassemblyConfig struct {
	Enabled         bool
	MaxStreams      int           // Maximum number of concurrent TCP streams
	MaxBufferSize   int           // Maximum buffer size per stream (bytes)
	MaxTotalMemory  int64         // Maximum total memory for all buffers (bytes)
	FlushInterval   time.Duration // How often to flush old streams
	StreamTimeout   time.Duration // Timeout for inactive streams
	CleanupInterval time.Duration // How often to clean up old buffers
}

type dnsStream struct {
	net, transport gopacket.Flow
	r              tcpreader.ReaderStream
	bufferManager  *TCPBufferManager
	handler        DNSPacketHandler
	reassembler    *TCPStreamReassembler
	streamKey      string
	closed         bool
	mu             sync.Mutex
}

func NewTCPStreamReassembler(handler DNSPacketHandler) *TCPStreamReassembler {
	return NewTCPStreamReassemblerWithConfig(handler, GetDefaultTCPReassemblyConfig())
}

func GetDefaultTCPReassemblyConfig() TCPReassemblyConfig {
	return TCPReassemblyConfig{
		Enabled:         false, // Disabled by default for initial implementation
		MaxStreams:      100,   // Reduced for initial implementation
		MaxBufferSize:   32768, // 32KB per stream (reduced)
		MaxTotalMemory:  10 * 1024 * 1024, // 10MB total (reduced)
		FlushInterval:   2 * time.Second,   // More frequent cleanup
		StreamTimeout:   10 * time.Second,  // Shorter timeout
		CleanupInterval: 5 * time.Second,   // More frequent cleanup
	}
}

func NewTCPStreamReassemblerWithConfig(handler DNSPacketHandler, config TCPReassemblyConfig) *TCPStreamReassembler {
	bufferManager := &TCPBufferManager{
		buffers:       make(map[string]*StreamBuffer),
		maxBuffers:    config.MaxStreams,
		maxBufferSize: config.MaxBufferSize,
	}
	
	reassembler := &TCPStreamReassembler{
		bufferManager: bufferManager,
		flushInterval: config.FlushInterval,
		stopChan:      make(chan struct{}),
		maxStreams:    config.MaxStreams,
		maxBufferSize: config.MaxBufferSize,
	}
	
	streamFactory := &dnsStreamFactory{
		bufferManager: bufferManager,
		handler:       handler,
		reassembler:   reassembler,
	}
	
	assembler := tcpassembly.NewAssembler(tcpassembly.NewStreamPool(streamFactory))
	
	reassembler.assembler = assembler
	reassembler.streamFactory = streamFactory
	
	return reassembler
}

func (r *TCPStreamReassembler) Start() {
	// Start the flush goroutine
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(r.flushInterval)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				r.mu.Lock()
				// Flush streams older than timeout
				r.assembler.FlushOlderThan(time.Now().Add(-r.flushInterval))
				r.mu.Unlock()
			case <-r.stopChan:
				return
			}
		}
	}()
	
	// Start the cleanup goroutine with more frequent intervals
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		// Clean up every second for aggressive memory management
		cleanupTicker := time.NewTicker(time.Second)
		defer cleanupTicker.Stop()
		
		for {
			select {
			case <-cleanupTicker.C:
				r.bufferManager.cleanupOldBuffers(r.flushInterval)
				r.enforceResourceLimits()
			case <-r.stopChan:
				return
			}
		}
	}()
}

func (r *TCPStreamReassembler) Stop() {
	close(r.stopChan)
	
	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		// Clean shutdown
	case <-time.After(5 * time.Second):
		// Force shutdown after timeout
	}
	
	// Final flush before stopping
	r.FlushAll()
}

func (r *TCPStreamReassembler) FlushAll() {
	r.mu.Lock()
	r.assembler.FlushAll()
	r.mu.Unlock()
}

func (r *TCPStreamReassembler) ProcessPacket(packet gopacket.Packet) {
	// Check if we're at capacity
	if atomic.LoadInt32(&r.currentStreams) >= int32(r.maxStreams) {
		// Drop packet if we're at max capacity
		return
	}
	
	if tcpLayer := packet.Layer(layers.LayerTypeTCP); tcpLayer != nil {
		tcp, _ := tcpLayer.(*layers.TCP)
		r.mu.Lock()
		r.assembler.AssembleWithTimestamp(
			packet.NetworkLayer().NetworkFlow(),
			tcp,
			packet.Metadata().Timestamp,
		)
		r.mu.Unlock()
	}
}

// enforceResourceLimits checks and enforces memory and stream limits
func (r *TCPStreamReassembler) enforceResourceLimits() {
	currentMem := atomic.LoadInt64(&r.totalMemory)
	currentStreamCount := atomic.LoadInt32(&r.currentStreams)
	
	// More aggressive limits: flush at 50% capacity
	if currentStreamCount > int32(float64(r.maxStreams)*0.5) || 
	   currentMem > int64(float64(r.maxBufferSize)*0.5) {
		r.mu.Lock()
		r.assembler.FlushOlderThan(time.Now().Add(-time.Second * 5))
		r.mu.Unlock()
	}
	
	// Hard limit: force flush all if at 90% capacity
	if currentStreamCount > int32(float64(r.maxStreams)*0.9) || 
	   currentMem > int64(float64(r.maxBufferSize)*0.9) {
		r.mu.Lock()
		r.assembler.FlushAll()
		r.mu.Unlock()
	}
}

// GetStats returns current resource usage statistics
func (r *TCPStreamReassembler) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"current_streams": atomic.LoadInt32(&r.currentStreams),
		"max_streams":     r.maxStreams,
		"total_memory":    atomic.LoadInt64(&r.totalMemory),
		"max_buffer_size": r.maxBufferSize,
	}
}

func (f *dnsStreamFactory) New(net, transport gopacket.Flow) tcpassembly.Stream {
	// Check if we can accept new streams
	if atomic.LoadInt32(&f.reassembler.currentStreams) >= int32(f.reassembler.maxStreams) {
		// Return a discarding stream if at capacity
		return &tcpreader.ReaderStream{}
	}
	
	streamKey := fmt.Sprintf("%s:%s->%s:%s",
		net.Src().String(),
		transport.Src().String(),
		net.Dst().String(),
		transport.Dst().String())
	
	s := &dnsStream{
		net:           net,
		transport:     transport,
		r:             tcpreader.NewReaderStream(),
		bufferManager: f.bufferManager,
		handler:       f.handler,
		reassembler:   f.reassembler,
		streamKey:     streamKey,
	}
	
	// Increment stream count
	atomic.AddInt32(&f.reassembler.currentStreams, 1)
	
	// Start stream processing with proper lifecycle management
	f.reassembler.wg.Add(1)
	go func() {
		defer f.reassembler.wg.Done()
		s.run()
	}()
	
	return &s.r
}

func (s *dnsStream) run() {
	defer func() {
		// Cleanup on exit
		s.cleanup()
		atomic.AddInt32(&s.reassembler.currentStreams, -1)
	}()
	
	buffer := &bytes.Buffer{}
	maxBufferSize := s.reassembler.maxBufferSize
	
	for {
		// Check if buffer is getting too large
		if buffer.Len() > maxBufferSize {
			// Buffer overflow protection
			buffer.Reset()
			continue
		}
		
		data := make([]byte, 4096)
		n, err := s.r.Read(data)
		if err != nil {
			return
		}
		
		if n > 0 {
			// Update memory usage
			atomic.AddInt64(&s.reassembler.totalMemory, int64(n))
			
			buffer.Write(data[:n])
			
			for buffer.Len() >= 2 {
				msgLength := binary.BigEndian.Uint16(buffer.Bytes()[:2])
				
				// Sanity check for message length
				if msgLength > 65535 || msgLength == 0 {
					// Invalid message length, reset buffer
					buffer.Reset()
					break
				}
				
				if buffer.Len() < int(msgLength)+2 {
					s.bufferManager.storeIncompleteMessage(s.streamKey, buffer.Bytes(), s.net, s.transport)
					break
				}
				
				dnsData := make([]byte, msgLength)
				buffer.Next(2)
				buffer.Read(dnsData)
				
				s.processDNSMessage(dnsData)
				
				// Update memory usage after processing
				atomic.AddInt64(&s.reassembler.totalMemory, -int64(msgLength+2))
			}
		}
	}
}

// cleanup ensures proper resource cleanup for the stream
func (s *dnsStream) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if !s.closed {
		s.closed = true
		s.bufferManager.removeBuffer(s.streamKey)
	}
}

func (s *dnsStream) processDNSMessage(dnsData []byte) {
	msg := new(mdns.Msg)
	if err := msg.Unpack(dnsData); err != nil {
		if s.handler != nil {
			s.handler(nil, false, fmt.Errorf("failed to unpack DNS message from TCP stream: %w", err))
		}
		return
	}
	
	srcIP, srcPort := s.net.Src().String(), s.transport.Src().String()
	dstIP, dstPort := s.net.Dst().String(), s.transport.Dst().String()
	
	isQuery := !msg.Response
	
	result := &Result{
		Timestamp: time.Now(),
		Protocol:  ProtocolTCP,
	}
	
	extraData := &ExtraPacketData{
		TransactionID: msg.Id,
		SourceIP:      srcIP,
		SourcePort:    parsePort(srcPort),
		DestIP:        dstIP,
		DestPort:      parsePort(dstPort),
	}
	
	if isQuery {
		result.Server = dstIP
		if len(msg.Question) > 0 {
			q := msg.Question[0]
			result.Domain = strings.TrimSuffix(q.Name, ".")
			result.QueryType = queryTypeToString(q.Qtype)
		}
	} else {
		result.Server = srcIP
		result.ResponseCode = msg.Rcode
		
		if len(msg.Question) > 0 {
			q := msg.Question[0]
			result.Domain = strings.TrimSuffix(q.Name, ".")
			result.QueryType = queryTypeToString(q.Qtype)
		}
		
		answers := []string{}
		for _, ans := range msg.Answer {
			answers = append(answers, extractAnswer(ans))
		}
		result.Answers = answers
		
		if msg.Rcode != mdns.RcodeSuccess {
			result.Error = fmt.Errorf("DNS error: %s", mdns.RcodeToString[msg.Rcode])
		}
	}
	
	result.Extra = extraData
	
	if s.handler != nil {
		s.handler(result, isQuery, nil)
	}
}

func (s *dnsStream) getStreamKey() string {
	return fmt.Sprintf("%s:%s->%s:%s",
		s.net.Src().String(),
		s.transport.Src().String(),
		s.net.Dst().String(),
		s.transport.Dst().String())
}

func (bm *TCPBufferManager) storeIncompleteMessage(key string, data []byte, net, transport gopacket.Flow) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	
	// Check buffer limits
	if len(bm.buffers) >= bm.maxBuffers {
		// Remove oldest buffer to make room
		var oldestKey string
		var oldestTime time.Time
		for k, v := range bm.buffers {
			if oldestTime.IsZero() || v.lastActivity.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.lastActivity
			}
		}
		if oldestKey != "" {
			atomic.AddInt64(&bm.totalSize, -int64(bm.buffers[oldestKey].size))
			delete(bm.buffers, oldestKey)
		}
	}
	
	// Check data size limit
	if len(data) > bm.maxBufferSize {
		// Truncate if too large
		data = data[:bm.maxBufferSize]
	}
	
	// Update total size
	if existing, ok := bm.buffers[key]; ok {
		atomic.AddInt64(&bm.totalSize, int64(len(data)-existing.size))
	} else {
		atomic.AddInt64(&bm.totalSize, int64(len(data)))
	}
	
	bm.buffers[key] = &StreamBuffer{
		buffer:       data,
		lastActivity: time.Now(),
		flow:         net,
		transport:    transport,
		size:         len(data),
	}
}

func (bm *TCPBufferManager) getBuffer(key string) *StreamBuffer {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	
	return bm.buffers[key]
}

func (bm *TCPBufferManager) removeBuffer(key string) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	
	if buf, ok := bm.buffers[key]; ok {
		atomic.AddInt64(&bm.totalSize, -int64(buf.size))
		delete(bm.buffers, key)
	}
}

func (bm *TCPBufferManager) cleanupOldBuffers(maxAge time.Duration) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	
	cutoff := time.Now().Add(-maxAge)
	for key, buf := range bm.buffers {
		if buf.lastActivity.Before(cutoff) {
			atomic.AddInt64(&bm.totalSize, -int64(buf.size))
			delete(bm.buffers, key)
		}
	}
}

// GetStats returns buffer manager statistics
func (bm *TCPBufferManager) GetStats() map[string]interface{} {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	
	return map[string]interface{}{
		"buffer_count":    len(bm.buffers),
		"max_buffers":     bm.maxBuffers,
		"total_size":      atomic.LoadInt64(&bm.totalSize),
		"max_buffer_size": bm.maxBufferSize,
	}
}

func parsePort(portStr string) uint16 {
	var port uint16
	fmt.Sscanf(portStr, "%d", &port)
	return port
}

func extractAnswer(rr mdns.RR) string {
	switch ans := rr.(type) {
	case *mdns.A:
		return ans.A.String()
	case *mdns.AAAA:
		return ans.AAAA.String()
	case *mdns.CNAME:
		return strings.TrimSuffix(ans.Target, ".")
	case *mdns.MX:
		return fmt.Sprintf("%d %s", ans.Preference, strings.TrimSuffix(ans.Mx, "."))
	case *mdns.NS:
		return strings.TrimSuffix(ans.Ns, ".")
	case *mdns.PTR:
		return strings.TrimSuffix(ans.Ptr, ".")
	case *mdns.TXT:
		return strings.Join(ans.Txt, " ")
	case *mdns.SOA:
		return fmt.Sprintf("%s %s", strings.TrimSuffix(ans.Ns, "."), strings.TrimSuffix(ans.Mbox, "."))
	case *mdns.SRV:
		return fmt.Sprintf("%d %d %d %s", ans.Priority, ans.Weight, ans.Port, strings.TrimSuffix(ans.Target, "."))
	default:
		return ""
	}
}

func parseDNSPacket(packet gopacket.Packet) (*Result, bool, error) {
	return parseDNSPacketWithReassembly(packet, false, nil)
}

func parseDNSPacketWithReassembly(packet gopacket.Packet, useReassembly bool, reassembler *TCPStreamReassembler) (*Result, bool, error) {
	networkLayer := packet.NetworkLayer()
	if networkLayer == nil {
		return nil, false, fmt.Errorf("no network layer found")
	}

	var srcIP, dstIP net.IP
	switch nl := networkLayer.(type) {
	case *layers.IPv4:
		srcIP = nl.SrcIP
		dstIP = nl.DstIP
	case *layers.IPv6:
		srcIP = nl.SrcIP
		dstIP = nl.DstIP
	default:
		return nil, false, fmt.Errorf("unsupported network layer type")
	}

	transportLayer := packet.TransportLayer()
	if transportLayer == nil {
		return nil, false, fmt.Errorf("no transport layer found")
	}

	var protocol Protocol
	var srcPort, dstPort uint16
	var dnsPayload []byte

	switch tl := transportLayer.(type) {
	case *layers.UDP:
		protocol = ProtocolUDP
		srcPort = uint16(tl.SrcPort)
		dstPort = uint16(tl.DstPort)
		dnsPayload = tl.Payload
	case *layers.TCP:
		protocol = ProtocolTCP
		srcPort = uint16(tl.SrcPort)
		dstPort = uint16(tl.DstPort)
		
		if useReassembly && reassembler != nil {
			reassembler.ProcessPacket(packet)
			return nil, false, nil
		}
		
		dnsPayload = tl.Payload
		
		if len(dnsPayload) < 2 {
			return nil, false, fmt.Errorf("TCP DNS payload too short: expected at least 2 bytes for length prefix, got %d", len(dnsPayload))
		}
		
		msgLength := binary.BigEndian.Uint16(dnsPayload[:2])
		
		if len(dnsPayload) < int(msgLength)+2 {
			return nil, false, fmt.Errorf("TCP DNS payload incomplete: length prefix indicates %d bytes, but only %d bytes available after prefix", msgLength, len(dnsPayload)-2)
		}
		
		actualPayloadLength := len(dnsPayload) - 2
		if int(msgLength) != actualPayloadLength {
			if int(msgLength) > actualPayloadLength {
				return nil, false, fmt.Errorf("TCP DNS payload incomplete: expected %d bytes, got %d bytes", msgLength, actualPayloadLength)
			}
			dnsPayload = dnsPayload[2 : msgLength+2]
		} else {
			dnsPayload = dnsPayload[2:]
		}
	default:
		return nil, false, fmt.Errorf("unsupported transport layer type")
	}

	if srcPort != 53 && dstPort != 53 {
		return nil, false, fmt.Errorf("not a DNS packet (ports: %d->%d)", srcPort, dstPort)
	}

	msg := new(mdns.Msg)
	if err := msg.Unpack(dnsPayload); err != nil {
		return nil, false, fmt.Errorf("failed to unpack DNS message: %w", err)
	}

	isQuery := !msg.Response
	
	result := &Result{
		Timestamp: packet.Metadata().Timestamp,
		Protocol:  protocol,
	}

	extraData := &ExtraPacketData{
		TransactionID: msg.Id,
		SourceIP:      srcIP.String(),
		SourcePort:    srcPort,
		DestIP:        dstIP.String(),
		DestPort:      dstPort,
	}

	if isQuery {
		result.Server = dstIP.String()
		if len(msg.Question) > 0 {
			q := msg.Question[0]
			result.Domain = strings.TrimSuffix(q.Name, ".")
			result.QueryType = queryTypeToString(q.Qtype)
		}
	} else {
		result.Server = srcIP.String()
		result.ResponseCode = msg.Rcode
		
		if len(msg.Question) > 0 {
			q := msg.Question[0]
			result.Domain = strings.TrimSuffix(q.Name, ".")
			result.QueryType = queryTypeToString(q.Qtype)
		}

		answers := []string{}
		for _, ans := range msg.Answer {
			answers = append(answers, extractAnswer(ans))
		}
		result.Answers = answers

		if msg.Rcode != mdns.RcodeSuccess {
			result.Error = fmt.Errorf("DNS error: %s", mdns.RcodeToString[msg.Rcode])
		}
	}

	result.Extra = extraData

	return result, isQuery, nil
}

func queryTypeToString(qtype uint16) string {
	switch qtype {
	case mdns.TypeA:
		return "A"
	case mdns.TypeAAAA:
		return "AAAA"
	case mdns.TypeCNAME:
		return "CNAME"
	case mdns.TypeMX:
		return "MX"
	case mdns.TypeNS:
		return "NS"
	case mdns.TypePTR:
		return "PTR"
	case mdns.TypeSOA:
		return "SOA"
	case mdns.TypeTXT:
		return "TXT"
	case mdns.TypeSRV:
		return "SRV"
	case mdns.TypeDNSKEY:
		return "DNSKEY"
	case mdns.TypeRRSIG:
		return "RRSIG"
	case mdns.TypeNSEC:
		return "NSEC"
	case mdns.TypeNSEC3:
		return "NSEC3"
	case mdns.TypeDS:
		return "DS"
	case mdns.TypeCAA:
		return "CAA"
	default:
		return fmt.Sprintf("TYPE%d", qtype)
	}
}

