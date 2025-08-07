package exporters

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/randax/dns-monitoring/internal/config"
)

// Protocol utilities

func BuildZabbixPacket(data []byte) []byte {
	var packet bytes.Buffer
	
	// Write header "ZBXD"
	packet.WriteString(zabbixProtocolHeader)
	
	// Write protocol version (0x01)
	packet.WriteByte(zabbixProtocolVersion)
	
	// Write data length (8 bytes, little endian)
	dataLen := uint64(len(data))
	binary.Write(&packet, binary.LittleEndian, dataLen)
	
	// Write data
	packet.Write(data)
	
	return packet.Bytes()
}

func ParseZabbixPacket(data []byte) ([]byte, error) {
	if len(data) < 13 {
		return nil, fmt.Errorf("packet too short: %d bytes", len(data))
	}
	
	// Check header
	if string(data[:4]) != zabbixProtocolHeader {
		return nil, fmt.Errorf("invalid header: %s", string(data[:4]))
	}
	
	// Check version
	if data[4] != zabbixProtocolVersion {
		return nil, fmt.Errorf("unsupported protocol version: %d", data[4])
	}
	
	// Read data length
	var dataLen uint64
	buf := bytes.NewReader(data[5:13])
	if err := binary.Read(buf, binary.LittleEndian, &dataLen); err != nil {
		return nil, fmt.Errorf("failed to read data length: %w", err)
	}
	
	// Check if we have enough data
	if uint64(len(data)-13) < dataLen {
		return nil, fmt.Errorf("incomplete packet: expected %d bytes, got %d", dataLen, len(data)-13)
	}
	
	return data[13 : 13+dataLen], nil
}

// JSON utilities

func MarshalZabbixRequest(request *ZabbixRequest) ([]byte, error) {
	// Use custom marshaling to ensure proper formatting
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	
	if err := encoder.Encode(request); err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	
	// Remove trailing newline
	data := buf.Bytes()
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	
	return data, nil
}

func UnmarshalZabbixResponse(data []byte) (*ZabbixResponse, error) {
	var response ZabbixResponse
	
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	return &response, nil
}

// Connection utilities

func TestConnection(server string, port int, timeout time.Duration) error {
	address := fmt.Sprintf("%s:%d", server, port)
	
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer conn.Close()
	
	// Send a minimal test packet
	testItem := ZabbixItem{
		Host:  "test",
		Key:   "agent.ping",
		Value: "1",
		Clock: time.Now().Unix(),
	}
	
	request := &ZabbixRequest{
		Request: "sender data",
		Data:    []ZabbixItem{testItem},
		Clock:   time.Now().Unix(),
	}
	
	jsonData, err := MarshalZabbixRequest(request)
	if err != nil {
		return fmt.Errorf("failed to marshal test request: %w", err)
	}
	
	packet := BuildZabbixPacket(jsonData)
	
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("failed to set deadline: %w", err)
	}
	
	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("failed to send test packet: %w", err)
	}
	
	// Read response header
	header := make([]byte, 13)
	if _, err := conn.Read(header); err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}
	
	// Basic validation
	if string(header[:4]) != zabbixProtocolHeader {
		return fmt.Errorf("invalid response header")
	}
	
	return nil
}

func IsConnectionHealthy(conn net.Conn) bool {
	if conn == nil {
		return false
	}
	
	// Try to set a short deadline and write nothing
	conn.SetDeadline(time.Now().Add(time.Millisecond))
	_, err := conn.Write([]byte{})
	conn.SetDeadline(time.Time{})
	
	return err == nil
}

// Data conversion utilities

func ConvertToZabbixTimestamp(t time.Time) int64 {
	return t.Unix()
}

func FormatZabbixValue(value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v), nil
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v), nil
	case float32:
		return formatFloatValue(float64(v)), nil
	case float64:
		return formatFloatValue(v), nil
	case bool:
		if v {
			return "1", nil
		}
		return "0", nil
	case time.Time:
		return fmt.Sprintf("%d", v.Unix()), nil
	case time.Duration:
		return fmt.Sprintf("%.3f", v.Seconds()), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

func formatFloatValue(value float64) string {
	// Format with up to 6 decimal places, removing trailing zeros
	s := strconv.FormatFloat(value, 'f', 6, 64)
	
	// Remove trailing zeros after decimal point
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	
	return s
}

// Validation utilities

func ValidateZabbixConfig(cfg *ZabbixConfig) error {
	if cfg.Server == "" {
		return fmt.Errorf("server address is required")
	}
	
	// Validate server address format
	if strings.Contains(cfg.Server, "://") {
		return fmt.Errorf("server address should not include protocol")
	}
	
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	
	if cfg.HostName == "" {
		return fmt.Errorf("hostname is required")
	}
	
	// Validate hostname format (Zabbix requirements)
	if len(cfg.HostName) > 128 {
		return fmt.Errorf("hostname too long (max 128 characters)")
	}
	
	if cfg.SendInterval <= 0 {
		return fmt.Errorf("send interval must be positive")
	}
	
	if cfg.BatchSize <= 0 || cfg.BatchSize > 1000 {
		return fmt.Errorf("batch size must be between 1 and 1000")
	}
	
	if cfg.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	
	if cfg.RetryAttempts < 0 || cfg.RetryAttempts > 10 {
		return fmt.Errorf("retry attempts must be between 0 and 10")
	}
	
	if cfg.RetryDelay < 0 {
		return fmt.Errorf("retry delay must be non-negative")
	}
	
	if cfg.ItemPrefix == "" {
		return fmt.Errorf("item prefix is required")
	}
	
	// Validate item prefix format
	if err := validateItemKey(cfg.ItemPrefix + ".test"); err != nil {
		return fmt.Errorf("invalid item prefix: %w", err)
	}
	
	return nil
}

// Type alias for config validation
type ZabbixConfig = config.ZabbixConfig

// Debugging utilities

func DebugPacket(packet []byte) string {
	var sb strings.Builder
	
	if len(packet) < 13 {
		return fmt.Sprintf("Invalid packet (too short): %d bytes", len(packet))
	}
	
	sb.WriteString(fmt.Sprintf("Header: %s\n", string(packet[:4])))
	sb.WriteString(fmt.Sprintf("Version: 0x%02x\n", packet[4]))
	
	var dataLen uint64
	buf := bytes.NewReader(packet[5:13])
	binary.Read(buf, binary.LittleEndian, &dataLen)
	sb.WriteString(fmt.Sprintf("Data length: %d\n", dataLen))
	
	if uint64(len(packet)-13) >= dataLen {
		jsonData := packet[13 : 13+dataLen]
		
		// Try to pretty print JSON
		var jsonObj interface{}
		if err := json.Unmarshal(jsonData, &jsonObj); err == nil {
			prettyJSON, _ := json.MarshalIndent(jsonObj, "", "  ")
			sb.WriteString("Data (JSON):\n")
			sb.WriteString(string(prettyJSON))
		} else {
			sb.WriteString(fmt.Sprintf("Data (raw): %s\n", string(jsonData)))
		}
	} else {
		sb.WriteString(fmt.Sprintf("Data: incomplete (%d bytes missing)\n", dataLen-uint64(len(packet)-13)))
	}
	
	return sb.String()
}

func DebugResponse(response *ZabbixResponse) string {
	var sb strings.Builder
	
	sb.WriteString(fmt.Sprintf("Response: %s\n", response.Response))
	if response.Info != "" {
		sb.WriteString(fmt.Sprintf("Info: %s\n", response.Info))
	}
	sb.WriteString(fmt.Sprintf("Success: %d\n", response.Success))
	sb.WriteString(fmt.Sprintf("Failed: %d\n", response.Failed))
	sb.WriteString(fmt.Sprintf("Total: %d\n", response.Total))
	
	return sb.String()
}

// Item creation helpers

func CreateZabbixItem(host, key, value string) ZabbixItem {
	return ZabbixItem{
		Host:  host,
		Key:   key,
		Value: value,
		Clock: time.Now().Unix(),
	}
}

func CreateZabbixItemWithTime(host, key, value string, timestamp time.Time) ZabbixItem {
	return ZabbixItem{
		Host:  host,
		Key:   key,
		Value: value,
		Clock: timestamp.Unix(),
	}
}