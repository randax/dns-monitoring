package dns

import (
	"context"
	"fmt"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
)

func openInterface(interfaceName, bpfFilter string, snapLen int) (*pcap.Handle, error) {
	inactive, err := pcap.NewInactiveHandle(interfaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to create inactive handle: %w", err)
	}
	defer inactive.CleanUp()

	if err := inactive.SetSnapLen(snapLen); err != nil {
		return nil, fmt.Errorf("failed to set snap length: %w", err)
	}

	if err := inactive.SetPromisc(true); err != nil {
		return nil, fmt.Errorf("failed to set promiscuous mode: %w", err)
	}

	if err := inactive.SetTimeout(time.Second); err != nil {
		return nil, fmt.Errorf("failed to set timeout: %w", err)
	}

	handle, err := inactive.Activate()
	if err != nil {
		return nil, fmt.Errorf("failed to activate handle: %w", err)
	}

	if bpfFilter != "" {
		if err := handle.SetBPFFilter(bpfFilter); err != nil {
			handle.Close()
			return nil, fmt.Errorf("failed to set BPF filter '%s': %w", bpfFilter, err)
		}
	}

	return handle, nil
}

func capturePackets(ctx context.Context, handle *pcap.Handle, resultChan chan<- gopacket.Packet) error {
	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	packetSource.Packets()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case packet, ok := <-packetSource.Packets():
			if !ok {
				return fmt.Errorf("packet source closed")
			}
			
			if packet == nil {
				continue
			}

			select {
			case resultChan <- packet:
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
	}
}

func openOfflineFile(filename string, bpfFilter string) (*pcap.Handle, error) {
	handle, err := pcap.OpenOffline(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open pcap file %s: %w", filename, err)
	}

	if bpfFilter != "" {
		if err := handle.SetBPFFilter(bpfFilter); err != nil {
			handle.Close()
			return nil, fmt.Errorf("failed to set BPF filter '%s': %w", bpfFilter, err)
		}
	}

	return handle, nil
}

type CaptureStats struct {
	PacketsReceived   uint32
	PacketsDropped    uint32
	PacketsIfDropped  uint32
}

func getCaptureStats(handle *pcap.Handle) (*CaptureStats, error) {
	stats, err := handle.Stats()
	if err != nil {
		return nil, fmt.Errorf("failed to get capture stats: %w", err)
	}

	return &CaptureStats{
		PacketsReceived:  uint32(stats.PacketsReceived),
		PacketsDropped:   uint32(stats.PacketsDropped),
		PacketsIfDropped: uint32(stats.PacketsIfDropped),
	}, nil
}

func checkPrivileges() error {
	testHandle, err := pcap.OpenLive("lo", 1, false, time.Second)
	if err != nil {
		return fmt.Errorf("insufficient privileges for packet capture: %w", err)
	}
	testHandle.Close()
	return nil
}