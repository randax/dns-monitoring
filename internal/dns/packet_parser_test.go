package dns

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTCPStreamReassembly(t *testing.T) {
	t.Run("direct_single_message", func(t *testing.T) {
		tcpSeq = 1
		receivedCount := 0
		var receivedDomain string
		
		handler := func(result *Result, isQuery bool, err error) {
			if err != nil {
				t.Logf("Handler error: %v", err)
				return
			}
			if result != nil {
				receivedCount++
				receivedDomain = result.Domain
				t.Logf("Received DNS message - Domain: %s, Query: %v", result.Domain, isQuery)
			}
		}
		
		reassembler := NewTCPStreamReassembler(handler)
		reassembler.Start()
		defer reassembler.Stop()
		
		packet := createCompleteTCPDNSPacket(t, "test.example.com")
		pkt := gopacket.NewPacket(packet, layers.LayerTypeEthernet, gopacket.Default)
		
		reassembler.ProcessPacket(pkt)
		
		reassembler.FlushAll()
		time.Sleep(200 * time.Millisecond)
		
		assert.Equal(t, 1, receivedCount, "Should receive exactly one DNS message")
		assert.Equal(t, "test.example.com", receivedDomain, "Domain should match")
	})
}

func TestTCPBufferManager(t *testing.T) {
	bm := &TCPBufferManager{
		buffers:       make(map[string]*StreamBuffer),
		maxBuffers:    100,
		maxBufferSize: 65536,
	}

	t.Run("store_and_retrieve_buffer", func(t *testing.T) {
		key := "test-stream"
		data := []byte{0x00, 0x10, 0x01, 0x02, 0x03}
		
		bm.storeIncompleteMessage(key, data, gopacket.Flow{}, gopacket.Flow{})
		
		buffer := bm.getBuffer(key)
		require.NotNil(t, buffer)
		assert.Equal(t, data, buffer.buffer)
	})

	t.Run("cleanup_old_buffers", func(t *testing.T) {
		key1 := "old-stream"
		key2 := "new-stream"
		
		bm.storeIncompleteMessage(key1, []byte{0x01}, gopacket.Flow{}, gopacket.Flow{})
		
		time.Sleep(10 * time.Millisecond)
		
		bm.storeIncompleteMessage(key2, []byte{0x02}, gopacket.Flow{}, gopacket.Flow{})
		
		bm.cleanupOldBuffers(5 * time.Millisecond)
		
		assert.Nil(t, bm.getBuffer(key1), "Old buffer should be cleaned up")
		assert.NotNil(t, bm.getBuffer(key2), "New buffer should still exist")
	})
}

func TestParseDNSPacketWithReassembly(t *testing.T) {
	t.Run("tcp_with_reassembly_enabled", func(t *testing.T) {
		packet := createTCPPacket(t, []byte{0x00, 0x10})
		
		handler := func(result *Result, isQuery bool, err error) {
			t.Log("Handler called")
		}
		
		reassembler := NewTCPStreamReassembler(handler)
		reassembler.Start()
		defer reassembler.Stop()
		
		result, isQuery, err := parseDNSPacketWithReassembly(packet, true, reassembler)
		
		assert.Nil(t, result)
		assert.False(t, isQuery)
		assert.Nil(t, err)
	})

	t.Run("tcp_without_reassembly", func(t *testing.T) {
		dnsMsg := createDNSQuery(t, "test.example.com")
		msgBytes, err := dnsMsg.Pack()
		require.NoError(t, err)
		
		tcpPayload := make([]byte, 2+len(msgBytes))
		binary.BigEndian.PutUint16(tcpPayload[:2], uint16(len(msgBytes)))
		copy(tcpPayload[2:], msgBytes)
		
		packet := createTCPPacket(t, tcpPayload)
		
		result, isQuery, err := parseDNSPacketWithReassembly(packet, false, nil)
		
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, isQuery)
		assert.Equal(t, "test.example.com", result.Domain)
	})

	t.Run("udp_packet", func(t *testing.T) {
		dnsMsg := createDNSQuery(t, "udp.example.com")
		msgBytes, err := dnsMsg.Pack()
		require.NoError(t, err)
		
		packet := createUDPPacket(t, msgBytes)
		
		result, isQuery, err := parseDNSPacketWithReassembly(packet, false, nil)
		
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, isQuery)
		assert.Equal(t, "udp.example.com", result.Domain)
		assert.Equal(t, ProtocolUDP, result.Protocol)
	})
}

func createDNSQuery(t *testing.T, domain string) *mdns.Msg {
	msg := new(mdns.Msg)
	msg.SetQuestion(mdns.Fqdn(domain), mdns.TypeA)
	return msg
}

func createCompleteTCPDNSPacket(t *testing.T, domain string) []byte {
	dnsMsg := createDNSQuery(t, domain)
	msgBytes, err := dnsMsg.Pack()
	require.NoError(t, err)
	
	tcpPayload := make([]byte, 2+len(msgBytes))
	binary.BigEndian.PutUint16(tcpPayload[:2], uint16(len(msgBytes)))
	copy(tcpPayload[2:], msgBytes)
	
	return createTCPPacketBytes(t, tcpPayload)
}

func createFragmentedTCPDNSPackets(t *testing.T, domain string) [][]byte {
	dnsMsg := createDNSQuery(t, domain)
	msgBytes, err := dnsMsg.Pack()
	require.NoError(t, err)
	
	tcpPayload := make([]byte, 2+len(msgBytes))
	binary.BigEndian.PutUint16(tcpPayload[:2], uint16(len(msgBytes)))
	copy(tcpPayload[2:], msgBytes)
	
	mid := len(tcpPayload) / 2
	packet1 := createTCPPacketBytes(t, tcpPayload[:mid])
	packet2 := createTCPPacketBytes(t, tcpPayload[mid:])
	
	return [][]byte{packet1, packet2}
}

func createTCPPacket(t *testing.T, payload []byte) gopacket.Packet {
	return gopacket.NewPacket(
		createTCPPacketBytes(t, payload),
		layers.LayerTypeEthernet,
		gopacket.Default,
	)
}

var tcpSeq uint32 = 1

func createTCPPacketBytes(t *testing.T, payload []byte) []byte {
	eth := &layers.Ethernet{
		SrcMAC:       net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
		DstMAC:       net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x02},
		EthernetType: layers.EthernetTypeIPv4,
	}

	ip := &layers.IPv4{
		Version:  4,
		IHL:      5,
		TTL:      64,
		Protocol: layers.IPProtocolTCP,
		SrcIP:    net.IP{192, 168, 1, 100},
		DstIP:    net.IP{8, 8, 8, 8},
	}

	tcp := &layers.TCP{
		SrcPort: 54321,
		DstPort: 53,
		Seq:     tcpSeq,
		Ack:     1,
		SYN:     false,
		ACK:     true,
		PSH:     true,
		Window:  65535,
		DataOffset: 5,
	}
	tcpSeq += uint32(len(payload))
	tcp.SetNetworkLayerForChecksum(ip)

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		ComputeChecksums: true,
		FixLengths:       true,
	}

	gopacket.SerializeLayers(buf, opts,
		eth,
		ip,
		tcp,
		gopacket.Payload(payload),
	)

	return buf.Bytes()
}

func createUDPPacket(t *testing.T, payload []byte) gopacket.Packet {
	eth := &layers.Ethernet{
		SrcMAC:       net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
		DstMAC:       net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x02},
		EthernetType: layers.EthernetTypeIPv4,
	}

	ip := &layers.IPv4{
		Version:  4,
		IHL:      5,
		TTL:      64,
		Protocol: layers.IPProtocolUDP,
		SrcIP:    net.IP{192, 168, 1, 100},
		DstIP:    net.IP{8, 8, 8, 8},
	}

	udp := &layers.UDP{
		SrcPort: 54321,
		DstPort: 53,
	}
	udp.SetNetworkLayerForChecksum(ip)

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		ComputeChecksums: true,
		FixLengths:       true,
	}

	gopacket.SerializeLayers(buf, opts,
		eth,
		ip,
		udp,
		gopacket.Payload(payload),
	)

	return gopacket.NewPacket(buf.Bytes(), layers.LayerTypeEthernet, gopacket.Default)
}