package exporters

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	zabbixProtocolHeader = "ZBXD"
	zabbixProtocolVersion = 0x01
	maxRetries = 3
	retryDelay = time.Second
)

type ZabbixSender struct {
	server  string
	port    int
	timeout time.Duration
	
	// Connection pool management
	connPool    chan net.Conn
	maxPoolSize int
	poolMu      sync.RWMutex
	poolSize    int32
	closed      bool
}

func NewZabbixSender(server string, port int, timeout time.Duration) *ZabbixSender {
	return &ZabbixSender{
		server:      server,
		port:        port,
		timeout:     timeout,
		maxPoolSize: 5,
		connPool:    make(chan net.Conn, 5),
		poolSize:    0,
		closed:      false,
	}
}

func (s *ZabbixSender) Connect() error {
	// Test connectivity by creating and validating a connection
	conn, err := s.createConnection()
	if err != nil {
		return err
	}
	
	// Return the connection to the pool
	s.returnConnection(conn)
	return nil
}

func (s *ZabbixSender) createConnection() (net.Conn, error) {
	address := fmt.Sprintf("%s:%d", s.server, s.port)
	conn, err := net.DialTimeout("tcp", address, s.timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Zabbix server %s: %w", address, err)
	}
	return conn, nil
}

func (s *ZabbixSender) validateConnection(conn net.Conn) bool {
	// Set a short deadline for the validation
	if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		return false
	}
	defer conn.SetReadDeadline(time.Time{})
	
	// Try to read one byte with MSG_PEEK (non-destructive read)
	// If the connection is closed, this will return an error
	one := make([]byte, 1)
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		// For TCP connections, we can use a more reliable method
		if err := tcpConn.SetKeepAlive(true); err != nil {
			return false
		}
	}
	
	// Attempt a zero-byte read to check if connection is still open
	conn.SetReadDeadline(time.Now())
	_, err := conn.Read(one[:0])
	conn.SetReadDeadline(time.Time{})
	
	return err == nil || err.Error() == "i/o timeout"
}

func (s *ZabbixSender) getConnection() (net.Conn, error) {
	s.poolMu.RLock()
	if s.closed {
		s.poolMu.RUnlock()
		return nil, fmt.Errorf("sender is closed")
	}
	s.poolMu.RUnlock()
	
	// Try to get a connection from the pool
	for {
		select {
		case conn := <-s.connPool:
			// Decrement pool size atomically
			newSize := atomic.AddInt32(&s.poolSize, -1)
			if newSize < 0 {
				// This shouldn't happen, but reset if it does
				atomic.StoreInt32(&s.poolSize, 0)
			}
			
			// Validate the connection
			if s.validateConnection(conn) {
				return conn, nil
			}
			// Connection is dead, close it and try again
			conn.Close()
		default:
			// Pool is empty, create a new connection
			return s.createConnection()
		}
	}
}

func (s *ZabbixSender) returnConnection(conn net.Conn) {
	if conn == nil {
		return
	}
	
	s.poolMu.RLock()
	if s.closed {
		s.poolMu.RUnlock()
		conn.Close()
		return
	}
	s.poolMu.RUnlock()
	
	// Check current pool size
	currentSize := atomic.LoadInt32(&s.poolSize)
	if currentSize >= int32(s.maxPoolSize) {
		// Pool is full, close the connection
		conn.Close()
		return
	}
	
	// Try to return the connection to the pool
	select {
	case s.connPool <- conn:
		// Successfully added to pool, increment size
		atomic.AddInt32(&s.poolSize, 1)
	default:
		// Pool channel is full (shouldn't happen if our counting is correct)
		conn.Close()
	}
}

func (s *ZabbixSender) Send(data *ZabbixData) (*ZabbixResponse, error) {
	conn, err := s.getConnection()
	if err != nil {
		return nil, err
	}
	defer s.returnConnection(conn)
	
	// Set timeout for the entire operation
	if err := conn.SetDeadline(time.Now().Add(s.timeout)); err != nil {
		return nil, fmt.Errorf("failed to set connection deadline: %w", err)
	}
	
	// Prepare the request
	request := &ZabbixRequest{
		Request: "sender data",
		Data:    data.Data,
		Clock:   time.Now().Unix(),
	}
	
	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	
	// Build the packet
	packet := s.buildPacket(jsonData)
	
	// Send the packet
	if _, err := conn.Write(packet); err != nil {
		return nil, fmt.Errorf("failed to send data to Zabbix: %w", err)
	}
	
	// Read the response
	response, err := s.readResponse(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to read response from Zabbix: %w", err)
	}
	
	return response, nil
}

func (s *ZabbixSender) SendBatch(items []ZabbixItem) (*ZabbixResponse, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("no items to send")
	}
	
	data := &ZabbixData{
		Data: items,
	}
	
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		response, err := s.Send(data)
		if err == nil {
			return response, nil
		}
		
		lastErr = err
		if i < maxRetries-1 {
			time.Sleep(retryDelay * time.Duration(i+1))
		}
	}
	
	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

func (s *ZabbixSender) buildPacket(data []byte) []byte {
	var packet bytes.Buffer
	
	// Write header
	packet.WriteString(zabbixProtocolHeader)
	packet.WriteByte(zabbixProtocolVersion)
	
	// Write data length (8 bytes, little endian)
	dataLen := uint64(len(data))
	binary.Write(&packet, binary.LittleEndian, dataLen)
	
	// Write data
	packet.Write(data)
	
	return packet.Bytes()
}

func (s *ZabbixSender) readResponse(conn net.Conn) (*ZabbixResponse, error) {
	// Read header (5 bytes: "ZBXD" + version)
	header := make([]byte, 5)
	if _, err := conn.Read(header); err != nil {
		return nil, fmt.Errorf("failed to read response header: %w", err)
	}
	
	// Verify header
	if string(header[:4]) != zabbixProtocolHeader {
		return nil, fmt.Errorf("invalid response header: %s", string(header[:4]))
	}
	
	// Read data length (8 bytes)
	var dataLen uint64
	if err := binary.Read(conn, binary.LittleEndian, &dataLen); err != nil {
		return nil, fmt.Errorf("failed to read response data length: %w", err)
	}
	
	// Read data
	data := make([]byte, dataLen)
	if _, err := conn.Read(data); err != nil {
		return nil, fmt.Errorf("failed to read response data: %w", err)
	}
	
	// Parse response
	var response ZabbixResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	return &response, nil
}

func (s *ZabbixSender) Close() error {
	s.poolMu.Lock()
	defer s.poolMu.Unlock()
	
	if s.closed {
		return nil
	}
	
	s.closed = true
	
	// Close all pooled connections
	// Don't close the channel until we've drained it
	for {
		select {
		case conn := <-s.connPool:
			if conn != nil {
				conn.Close()
			}
		default:
			// No more connections in the pool
			close(s.connPool)
			return nil
		}
	}
}

func (s *ZabbixSender) TestConnection() error {
	conn, err := s.getConnection()
	if err != nil {
		return err
	}
	// Important: return connection to pool instead of closing it
	defer s.returnConnection(conn)
	
	// Send a minimal test packet
	testItem := ZabbixItem{
		Host:  "test",
		Key:   "agent.ping",
		Value: "1",
		Clock: time.Now().Unix(),
	}
	
	data := &ZabbixData{
		Data: []ZabbixItem{testItem},
	}
	
	request := &ZabbixRequest{
		Request: "sender data",
		Data:    data.Data,
		Clock:   time.Now().Unix(),
	}
	
	jsonData, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal test request: %w", err)
	}
	
	packet := s.buildPacket(jsonData)
	
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("failed to set test deadline: %w", err)
	}
	
	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("failed to send test packet: %w", err)
	}
	
	_, err = s.readResponse(conn)
	return err
}