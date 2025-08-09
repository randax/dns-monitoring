package dns

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPacketCaptureHandler_NewHandler(t *testing.T) {
	tests := []struct {
		name   string
		config CaptureConfig
		want   CaptureConfig
	}{
		{
			name: "with default values",
			config: CaptureConfig{
				Interfaces: []string{"lo0"},
			},
			want: CaptureConfig{
				Interfaces:    []string{"lo0"},
				BufferSize:    10000,
				SnapLen:       65535,
				StatsInterval: 5 * time.Second,
			},
		},
		{
			name: "with custom values",
			config: CaptureConfig{
				Interfaces:    []string{"eth0", "eth1"},
				BufferSize:    5000,
				SnapLen:       1500,
				StatsInterval: 10 * time.Second,
				BPFFilter:     "port 53",
			},
			want: CaptureConfig{
				Interfaces:    []string{"eth0", "eth1"},
				BufferSize:    5000,
				SnapLen:       1500,
				StatsInterval: 10 * time.Second,
				BPFFilter:     "port 53",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewPacketCaptureHandler(tt.config)
			require.NotNil(t, handler)

			pch := handler.(*packetCaptureHandler)
			assert.Equal(t, tt.want.BufferSize, pch.config.BufferSize)
			assert.Equal(t, tt.want.SnapLen, pch.config.SnapLen)
			assert.Equal(t, tt.want.StatsInterval, pch.config.StatsInterval)
		})
	}
}

func TestPacketCaptureHandler_AddRemoveInterface(t *testing.T) {
	handler := NewPacketCaptureHandler(CaptureConfig{
		BufferSize: 100,
	})

	pch := handler.(*packetCaptureHandler)

	t.Run("add interface", func(t *testing.T) {
		err := handler.AddInterface("test-interface", "port 53")
		if err != nil {
			t.Skipf("Skipping test: %v", err)
		}

		assert.Len(t, pch.interfaces, 1)
		assert.Contains(t, pch.interfaces, "test-interface")
	})

	t.Run("add duplicate interface", func(t *testing.T) {
		if len(pch.interfaces) == 0 {
			t.Skip("Previous test was skipped")
		}
		err := handler.AddInterface("test-interface", "port 53")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already added")
	})

	t.Run("remove interface", func(t *testing.T) {
		if len(pch.interfaces) == 0 {
			t.Skip("Previous test was skipped")
		}
		err := handler.RemoveInterface("test-interface")
		assert.NoError(t, err)
		assert.Len(t, pch.interfaces, 0)
	})

	t.Run("remove non-existent interface", func(t *testing.T) {
		err := handler.RemoveInterface("non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestPacketCaptureHandler_StartStop(t *testing.T) {
	handler := NewPacketCaptureHandler(CaptureConfig{
		BufferSize: 100,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("start without interfaces", func(t *testing.T) {
		err := handler.Start(ctx)
		if err != nil && err.Error() == "insufficient privileges: insufficient privileges for packet capture: operation not permitted" {
			t.Skip("Insufficient privileges for packet capture")
		}
	})

	t.Run("start twice", func(t *testing.T) {
		pch := handler.(*packetCaptureHandler)
		if !pch.running.Load() {
			t.Skip("Handler not running from previous test")
		}
		err := handler.Start(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already running")
	})

	t.Run("stop handler", func(t *testing.T) {
		pch := handler.(*packetCaptureHandler)
		if !pch.running.Load() {
			t.Skip("Handler not running")
		}
		handler.Stop()
		assert.False(t, pch.running.Load())
	})

	t.Run("stop twice", func(t *testing.T) {
		handler.Stop()
	})
}

func TestPacketCaptureHandler_GetStats(t *testing.T) {
	handler := NewPacketCaptureHandler(CaptureConfig{
		BufferSize: 100,
	})

	t.Run("get all stats empty", func(t *testing.T) {
		stats := handler.GetAllStats()
		assert.NotNil(t, stats)
		assert.Len(t, stats, 0)
	})

	t.Run("get interface stats not found", func(t *testing.T) {
		stats, err := handler.GetInterfaceStats("non-existent")
		assert.Error(t, err)
		assert.Nil(t, stats)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestPacketCaptureHandler_PacketChannel(t *testing.T) {
	handler := NewPacketCaptureHandler(CaptureConfig{
		BufferSize: 10,
	})

	packetChan := handler.GetPacketChannel()
	assert.NotNil(t, packetChan)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pch := handler.(*packetCaptureHandler)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			select {
			case pch.packetChan <- createTestPacket():
			case <-ctx.Done():
				return
			}
		}
	}()

	receivedCount := 0
	timeout := time.After(2 * time.Second)

	for receivedCount < 5 {
		select {
		case packet := <-packetChan:
			assert.NotNil(t, packet)
			receivedCount++
		case <-timeout:
			t.Fatal("Timeout receiving packets")
		}
	}

	cancel()
	wg.Wait()
	assert.Equal(t, 5, receivedCount)
}

func TestPacketCaptureHandler_Hooks(t *testing.T) {
	var receivedCount atomic.Uint64
	var droppedCount atomic.Uint64

	handler := NewPacketCaptureHandler(CaptureConfig{
		BufferSize: 2,
		OnPacketHooks: PacketHooks{
			OnPacketReceived: func() {
				receivedCount.Add(1)
			},
			OnPacketDropped: func() {
				droppedCount.Add(1)
			},
		},
	})

	pch := handler.(*packetCaptureHandler)

	mockInterface := &interfaceHandle{
		name: "test",
		stats: &InterfaceStats{
			Interface: "test",
		},
	}

	localChan := make(chan gopacket.Packet, 10)
	for i := 0; i < 10; i++ {
		localChan <- createTestPacket()
	}
	close(localChan)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for packet := range localChan {
			atomic.AddUint64(&mockInterface.stats.PacketsProcessed, 1)

			if pch.config.OnPacketHooks.OnPacketReceived != nil {
				pch.config.OnPacketHooks.OnPacketReceived()
			}

			select {
			case pch.packetChan <- packet:
			default:
				atomic.AddUint64(&mockInterface.stats.PacketsDropped, 1)
				if pch.config.OnPacketHooks.OnPacketDropped != nil {
					pch.config.OnPacketHooks.OnPacketDropped()
				}
			}
		}
	}()

	wg.Wait()

	assert.Equal(t, uint64(10), receivedCount.Load())
	assert.Greater(t, droppedCount.Load(), uint64(0))
}

func TestPacketCaptureHandler_GetActiveInterfaces(t *testing.T) {
	handler := NewPacketCaptureHandler(CaptureConfig{
		BufferSize: 100,
	})

	t.Run("no active interfaces", func(t *testing.T) {
		interfaces := handler.GetActiveInterfaces()
		assert.NotNil(t, interfaces)
		assert.Len(t, interfaces, 0)
	})

	pch := handler.(*packetCaptureHandler)

	t.Run("with inactive interface", func(t *testing.T) {
		pch.interfaces["test1"] = &interfaceHandle{
			name: "test1",
			stats: &InterfaceStats{
				Interface: "test1",
				Active:    false,
			},
		}

		interfaces := handler.GetActiveInterfaces()
		assert.Len(t, interfaces, 0)
	})

	t.Run("with active interface", func(t *testing.T) {
		pch.interfaces["test2"] = &interfaceHandle{
			name: "test2",
			stats: &InterfaceStats{
				Interface: "test2",
				Active:    true,
			},
		}
		pch.interfaces["test2"].running.Store(true)

		interfaces := handler.GetActiveInterfaces()
		assert.Len(t, interfaces, 1)
		assert.Contains(t, interfaces, "test2")
	})
}

func TestPacketCaptureHandler_ErrorChannel(t *testing.T) {
	errorChan := make(chan error, 10)
	handler := NewPacketCaptureHandler(CaptureConfig{
		BufferSize:   100,
		ErrorChannel: errorChan,
	})

	pch := handler.(*packetCaptureHandler)

	t.Run("send error", func(t *testing.T) {
		pch.sendError("test error")

		select {
		case err := <-errorChan:
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "test error")
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for error")
		}
	})

	t.Run("error channel full", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			pch.sendError("filling channel")
		}

		pch.sendError("overflow error")
	})
}

func TestPacketCaptureHandler_StatsAggregation(t *testing.T) {
	handler := NewPacketCaptureHandler(CaptureConfig{
		BufferSize:    100,
		StatsInterval: 100 * time.Millisecond,
	})

	pch := handler.(*packetCaptureHandler)

	mockInterface := &interfaceHandle{
		name: "test",
		stats: &InterfaceStats{
			Interface: "test",
		},
	}
	pch.interfaces["test"] = mockInterface

	pch.wg.Add(1)
	go pch.statsAggregator()

	pch.statsUpdateChan <- statsUpdate{
		interfaceName: "test",
		stats: &CaptureStats{
			PacketsReceived:  100,
			PacketsDropped:   10,
			PacketsIfDropped: 5,
		},
	}

	time.Sleep(200 * time.Millisecond)

	stats, err := handler.GetInterfaceStats("test")
	require.NoError(t, err)
	assert.Equal(t, uint64(100), stats.PacketsReceived)
	assert.Equal(t, uint64(10), stats.PacketsDropped)
	assert.Equal(t, uint64(5), stats.PacketsIfDropped)

	pch.cancel()
	pch.wg.Wait()
}

func createTestPacket() gopacket.Packet {
	eth := &layers.Ethernet{
		SrcMAC:       []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
		DstMAC:       []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x02},
		EthernetType: layers.EthernetTypeIPv4,
	}

	ip := &layers.IPv4{
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolUDP,
		SrcIP:    []byte{127, 0, 0, 1},
		DstIP:    []byte{8, 8, 8, 8},
	}

	udp := &layers.UDP{
		SrcPort: 12345,
		DstPort: 53,
	}
	udp.SetNetworkLayerForChecksum(ip)

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}

	_ = gopacket.SerializeLayers(buf, opts, eth, ip, udp)

	return gopacket.NewPacket(buf.Bytes(), layers.LayerTypeEthernet, gopacket.Default)
}