package exporters

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"sync"
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
	
	conn    net.Conn
	mu      sync.Mutex
	
	connPool   chan net.Conn
	poolSize   int
	maxPoolSize int
}

func NewZabbixSender(server string, port int, timeout time.Duration) *ZabbixSender {
	return &ZabbixSender{
		server:      server,
		port:        port,
		timeout:     timeout,
		maxPoolSize: 5,
		connPool:    make(chan net.Conn, 5),
	}
}

func (s *ZabbixSender) Connect() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.conn != nil {
		return nil // Already connected
	}
	
	address := fmt.Sprintf("%s:%d", s.server, s.port)
	conn, err := net.DialTimeout("tcp", address, s.timeout)
	if err != nil {
		return fmt.Errorf("failed to connect to Zabbix server %s: %w", address, err)
	}
	
	s.conn = conn
	return nil
}

func (s *ZabbixSender) getConnection() (net.Conn, error) {
	// Try to get a connection from the pool
	select {
	case conn := <-s.connPool:
		// Test if connection is still alive
		conn.SetDeadline(time.Now().Add(time.Millisecond))
		if _, err := conn.Write([]byte{}); err == nil {
			conn.SetDeadline(time.Time{})
			return conn, nil
		}
		// Connection is dead, close it
		conn.Close()
	default:
		// Pool is empty or doesn't have available connections
	}
	
	// Create a new connection
	address := fmt.Sprintf("%s:%d", s.server, s.port)
	conn, err := net.DialTimeout("tcp", address, s.timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Zabbix server %s: %w", address, err)
	}
	
	return conn, nil
}

func (s *ZabbixSender) returnConnection(conn net.Conn) {
	if s.poolSize < s.maxPoolSize {
		select {
		case s.connPool <- conn:
			s.poolSize++
			return
		default:
			// Pool is full
		}
	}
	conn.Close()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Close all pooled connections
	close(s.connPool)
	for conn := range s.connPool {
		conn.Close()
	}
	
	if s.conn != nil {
		err := s.conn.Close()
		s.conn = nil
		return err
	}
	
	return nil
}

func (s *ZabbixSender) TestConnection() error {
	conn, err := s.getConnection()
	if err != nil {
		return err
	}
	defer conn.Close()
	
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