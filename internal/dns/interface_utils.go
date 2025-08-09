package dns

import (
	"fmt"
	"net"
	"runtime"
	"strings"

	"github.com/google/gopacket/pcap"
)

func getAvailableInterfaces() ([]string, error) {
	devices, err := pcap.FindAllDevs()
	if err != nil {
		return nil, fmt.Errorf("failed to find network devices: %w", err)
	}

	var interfaces []string
	for _, device := range devices {
		if isValidInterface(device) {
			interfaces = append(interfaces, device.Name)
		}
	}

	if len(interfaces) == 0 {
		return nil, fmt.Errorf("no suitable network interfaces found")
	}

	return interfaces, nil
}

func isValidInterface(device pcap.Interface) bool {
	if strings.HasPrefix(device.Name, "lo") {
		return false
	}

	if runtime.GOOS == "darwin" {
		if strings.HasPrefix(device.Name, "lo") || 
		   strings.HasPrefix(device.Name, "utun") ||
		   strings.HasPrefix(device.Name, "awdl") ||
		   strings.HasPrefix(device.Name, "llw") ||
		   strings.HasPrefix(device.Name, "bridge") {
			return false
		}
	}

	if runtime.GOOS == "linux" {
		if strings.HasPrefix(device.Name, "lo") ||
		   strings.HasPrefix(device.Name, "docker") ||
		   strings.HasPrefix(device.Name, "br-") ||
		   strings.HasPrefix(device.Name, "veth") ||
		   strings.HasPrefix(device.Name, "virbr") {
			return false
		}
	}

	if runtime.GOOS == "windows" {
		if strings.Contains(strings.ToLower(device.Description), "loopback") ||
		   strings.Contains(strings.ToLower(device.Description), "virtual") ||
		   strings.Contains(strings.ToLower(device.Description), "vmware") ||
		   strings.Contains(strings.ToLower(device.Description), "virtualbox") {
			return false
		}
	}

	hasValidAddress := false
	for _, addr := range device.Addresses {
		if addr.IP != nil && !addr.IP.IsLoopback() {
			if addr.IP.To4() != nil || addr.IP.To16() != nil {
				hasValidAddress = true
				break
			}
		}
	}

	return hasValidAddress
}

func validateInterface(name string) error {
	devices, err := pcap.FindAllDevs()
	if err != nil {
		return fmt.Errorf("failed to find network devices: %w", err)
	}

	for _, device := range devices {
		if device.Name == name {
			if !isValidInterface(device) {
				return fmt.Errorf("interface %s is not suitable for packet capture", name)
			}
			return nil
		}
	}

	return fmt.Errorf("interface %s not found", name)
}

func RequiresPrivileges() bool {
	if runtime.GOOS == "windows" {
		return false
	}

	// First, try to get available physical network interfaces
	devices, err := pcap.FindAllDevs()
	if err != nil {
		// If we can't even list devices, we likely need privileges
		return true
	}

	// Find a suitable physical interface to test
	var testInterface string
	for _, device := range devices {
		// Skip loopback and virtual interfaces for more accurate testing
		if isValidInterface(device) {
			testInterface = device.Name
			break
		}
	}

	// If no physical interface found, fall back to testing with loopback
	if testInterface == "" {
		testInterface = "lo"
	}

	// Try to open the interface for packet capture
	testHandle, err := pcap.OpenLive(testInterface, 1, false, 0)
	if err != nil {
		// If we can't open the interface, we need privileges
		return true
	}
	testHandle.Close()

	// Additionally, if we're testing a physical interface, 
	// try to set promiscuous mode which typically requires privileges
	if testInterface != "lo" {
		testHandle, err = pcap.OpenLive(testInterface, 1, true, 0)
		if err != nil {
			return true
		}
		testHandle.Close()
	}

	return false
}

func GetInterfaceInfo(name string) (*InterfaceInfo, error) {
	devices, err := pcap.FindAllDevs()
	if err != nil {
		return nil, fmt.Errorf("failed to find network devices: %w", err)
	}

	for _, device := range devices {
		if device.Name == name {
			info := &InterfaceInfo{
				Name:        device.Name,
				Description: device.Description,
				Addresses:   []string{},
			}

			for _, addr := range device.Addresses {
				if addr.IP != nil {
					info.Addresses = append(info.Addresses, addr.IP.String())
				}
			}

			iface, err := net.InterfaceByName(name)
			if err == nil {
				info.MTU = iface.MTU
				info.Flags = iface.Flags.String()
				info.HardwareAddr = iface.HardwareAddr.String()
			}

			return info, nil
		}
	}

	return nil, fmt.Errorf("interface %s not found", name)
}

type InterfaceInfo struct {
	Name         string
	Description  string
	Addresses    []string
	MTU          int
	Flags        string
	HardwareAddr string
}

func ListAllInterfaces() ([]*InterfaceInfo, error) {
	devices, err := pcap.FindAllDevs()
	if err != nil {
		return nil, fmt.Errorf("failed to find network devices: %w", err)
	}

	var interfaces []*InterfaceInfo
	for _, device := range devices {
		info := &InterfaceInfo{
			Name:        device.Name,
			Description: device.Description,
			Addresses:   []string{},
		}

		for _, addr := range device.Addresses {
			if addr.IP != nil {
				info.Addresses = append(info.Addresses, addr.IP.String())
			}
		}

		iface, err := net.InterfaceByName(device.Name)
		if err == nil {
			info.MTU = iface.MTU
			info.Flags = iface.Flags.String()
			info.HardwareAddr = iface.HardwareAddr.String()
		}

		interfaces = append(interfaces, info)
	}

	return interfaces, nil
}

func GetDefaultInterface() (string, error) {
	interfaces, err := getAvailableInterfaces()
	if err != nil {
		return "", err
	}

	if len(interfaces) > 0 {
		for _, iface := range interfaces {
			if runtime.GOOS == "darwin" && strings.HasPrefix(iface, "en") {
				return iface, nil
			}
			if runtime.GOOS == "linux" && (strings.HasPrefix(iface, "eth") || strings.HasPrefix(iface, "enp")) {
				return iface, nil
			}
		}
		return interfaces[0], nil
	}

	return "", fmt.Errorf("no default interface found")
}