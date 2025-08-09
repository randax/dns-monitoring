package config

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// ValidationResult contains detailed validation information
type ValidationResult struct {
	Valid   bool
	Errors  []ValidationError
	Warnings []ValidationWarning
}

// ValidationError represents a configuration error with remediation advice
type ValidationError struct {
	Field       string
	Message     string
	Remediation string
}

// ValidationWarning represents a non-critical configuration issue
type ValidationWarning struct {
	Field   string
	Message string
}

// ValidateComprehensive performs thorough configuration validation
func ValidateComprehensive(cfg *Config) (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:    true,
		Errors:   []ValidationError{},
		Warnings: []ValidationWarning{},
	}
	
	// DNS server reachability validation
	if err := validateDNSServerReachability(cfg.DNS.Servers, result); err != nil {
		result.Valid = false
	}
	
	// Protocol-specific configuration validation
	if err := validateProtocolConfiguration(cfg.DNS.Servers, result); err != nil {
		result.Valid = false
	}
	
	// Export configuration validation
	if err := validateExportConfiguration(cfg.Metrics, result); err != nil {
		result.Valid = false
	}
	
	// Passive monitoring privilege checks
	if cfg.Passive.Enabled {
		if err := validatePassivePrivileges(result); err != nil {
			result.Valid = false
		}
	}
	
	// Configuration dependency validation
	if err := validateConfigurationDependencies(cfg, result); err != nil {
		result.Valid = false
	}
	
	if !result.Valid {
		return result, fmt.Errorf("configuration validation failed with %d errors", len(result.Errors))
	}
	
	return result, nil
}

// validateDNSServerReachability tests connectivity to configured DNS servers
func validateDNSServerReachability(servers []DNSServer, result *ValidationResult) error {
	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		
		address := fmt.Sprintf("%s:%d", server.Address, server.Port)
		
		switch server.Protocol {
		case "udp", "":
			if err := testUDPConnectivity(address); err != nil {
				result.Errors = append(result.Errors, ValidationError{
					Field:   fmt.Sprintf("dns.servers.%s", server.Name),
					Message: fmt.Sprintf("DNS server %s (UDP) is unreachable: %v", address, err),
					Remediation: fmt.Sprintf("Check network connectivity to %s. Verify firewall rules allow UDP port %d. Try: nc -u -z -w 2 %s %d",
						server.Address, server.Port, server.Address, server.Port),
				})
			}
			
		case "tcp":
			if err := testTCPConnectivity(address, 5*time.Second); err != nil {
				result.Errors = append(result.Errors, ValidationError{
					Field:   fmt.Sprintf("dns.servers.%s", server.Name),
					Message: fmt.Sprintf("DNS server %s (TCP) is unreachable: %v", address, err),
					Remediation: fmt.Sprintf("Check network connectivity to %s. Verify firewall rules allow TCP port %d. Try: nc -z -w 2 %s %d",
						server.Address, server.Port, server.Address, server.Port),
				})
			}
			
		case "dot":
			if err := testDoTConnectivity(address, server.InsecureSkipVerify); err != nil {
				result.Errors = append(result.Errors, ValidationError{
					Field:   fmt.Sprintf("dns.servers.%s", server.Name),
					Message: fmt.Sprintf("DNS-over-TLS server %s is unreachable: %v", address, err),
					Remediation: fmt.Sprintf("Verify TLS certificate validity for %s. Check port %d is open. If certificate issues persist, temporarily set insecure_skip_verify: true",
						server.Address, server.Port),
				})
			}
			
		case "doh":
			if err := testDoHConnectivity(server.Address, server.DoHEndpoint, server.InsecureSkipVerify); err != nil {
				result.Errors = append(result.Errors, ValidationError{
					Field:   fmt.Sprintf("dns.servers.%s", server.Name),
					Message: fmt.Sprintf("DNS-over-HTTPS server %s is unreachable: %v", server.Address, err),
					Remediation: fmt.Sprintf("Verify the DoH endpoint URL: %s%s. Check HTTPS connectivity. Ensure the endpoint path is correct (usually /dns-query)",
						server.Address, server.DoHEndpoint),
				})
			}
		}
	}
	
	return nil
}

// testUDPConnectivity tests UDP connection to DNS server
func testUDPConnectivity(address string) error {
	conn, err := net.DialTimeout("udp", address, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	
	// Send a minimal DNS query (query for . NS)
	query := []byte{
		0x00, 0x00, // Transaction ID
		0x01, 0x00, // Flags: standard query
		0x00, 0x01, // Questions: 1
		0x00, 0x00, // Answers: 0
		0x00, 0x00, // Authority: 0
		0x00, 0x00, // Additional: 0
		0x00,       // Root domain
		0x00, 0x02, // Type: NS
		0x00, 0x01, // Class: IN
	}
	
	if _, err := conn.Write(query); err != nil {
		return fmt.Errorf("failed to send test query: %w", err)
	}
	
	// Try to read response with timeout
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buffer := make([]byte, 512)
	if _, err := conn.Read(buffer); err != nil {
		return fmt.Errorf("no response received: %w", err)
	}
	
	return nil
}

// testTCPConnectivity tests TCP connection to DNS server
func testTCPConnectivity(address string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	
	return nil
}

// testDoTConnectivity tests DNS-over-TLS connectivity
func testDoTConnectivity(address string, insecureSkipVerify bool) error {
	config := &tls.Config{
		InsecureSkipVerify: insecureSkipVerify,
	}
	
	conn, err := tls.DialWithDialer(&net.Dialer{
		Timeout: 5 * time.Second,
	}, "tcp", address, config)
	
	if err != nil {
		return err
	}
	defer conn.Close()
	
	// Verify handshake completed
	if err := conn.Handshake(); err != nil {
		return fmt.Errorf("TLS handshake failed: %w", err)
	}
	
	return nil
}

// testDoHConnectivity tests DNS-over-HTTPS connectivity
func testDoHConnectivity(serverURL, endpoint string, insecureSkipVerify bool) error {
	// Server URL should already have explicit scheme from validation
	if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
		return fmt.Errorf("DoH server URL must have explicit scheme (http:// or https://): %s", serverURL)
	}
	
	fullURL := serverURL + endpoint
	
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: insecureSkipVerify,
			},
		},
	}
	
	// Send HEAD request to check endpoint availability
	req, err := http.NewRequestWithContext(context.Background(), "HEAD", fullURL, nil)
	if err != nil {
		return fmt.Errorf("invalid DoH URL: %w", err)
	}
	
	req.Header.Set("Accept", "application/dns-message")
	
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("DoH endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()
	
	// Accept 200, 405 (method not allowed), or 400 (bad request) as signs the endpoint exists
	if resp.StatusCode != 200 && resp.StatusCode != 405 && resp.StatusCode != 400 {
		return fmt.Errorf("DoH endpoint returned status %d", resp.StatusCode)
	}
	
	return nil
}

// validateProtocolConfiguration checks protocol-specific settings
func validateProtocolConfiguration(servers []DNSServer, result *ValidationResult) error {
	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		
		switch server.Protocol {
		case "dot":
			if server.Port != 853 {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   fmt.Sprintf("dns.servers.%s.port", server.Name),
					Message: fmt.Sprintf("DNS-over-TLS typically uses port 853, but %d is configured", server.Port),
				})
			}
			
		case "doh", "doh-get":
			if server.Port != 443 && server.Port != 8443 {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   fmt.Sprintf("dns.servers.%s.port", server.Name),
					Message: fmt.Sprintf("DNS-over-HTTPS typically uses port 443 or 8443, but %d is configured", server.Port),
				})
			}
			
			if !strings.HasPrefix(server.DoHEndpoint, "/") {
				result.Errors = append(result.Errors, ValidationError{
					Field:       fmt.Sprintf("dns.servers.%s.doh_endpoint", server.Name),
					Message:     fmt.Sprintf("DoH endpoint must start with '/', got '%s'", server.DoHEndpoint),
					Remediation: "Change the endpoint to start with '/', e.g., '/dns-query'",
				})
			}
			
			// Check for IP address usage without explicit insecure_skip_verify
			hostname := extractHostnameFromAddress(server.Address)
			if net.ParseIP(hostname) != nil && !server.InsecureSkipVerify {
				result.Errors = append(result.Errors, ValidationError{
					Field:       fmt.Sprintf("dns.servers.%s", server.Name),
					Message:     fmt.Sprintf("DoH server using IP address '%s' requires explicit TLS configuration", hostname),
					Remediation: fmt.Sprintf("Either use a domain name for the DoH server, or explicitly set 'insecure_skip_verify: true' if you understand the security implications of skipping certificate verification"),
				})
			}
		}
	}
	
	return nil
}

// validateExportConfiguration checks export settings
func validateExportConfiguration(metrics *MetricsConfig, result *ValidationResult) error {
	if metrics == nil {
		return nil
	}
	
	// Check Prometheus configuration
	if metrics.Export.Prometheus.Enabled {
		// Basic metric prefix validation - make it lenient
		if prefix := metrics.Export.Prometheus.MetricPrefix; prefix != "" {
			// Only warn about obvious issues
			if strings.Contains(prefix, "__") {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   "metrics.export.prometheus.metric_prefix",
					Message: fmt.Sprintf("Metric prefix '%s' contains double underscores, which Prometheus reserves for internal use", prefix),
				})
			}
		}
		
		port := metrics.Export.Prometheus.Port
		if err := checkPortAvailability(port); err != nil {
			// Provide OS-specific remediation advice
			remediation := ""
			if strings.Contains(err.Error(), "already in use") {
				switch runtime.GOOS {
				case "darwin":
					remediation = fmt.Sprintf("Find process using port: lsof -i :%d\nKill process: kill -9 <PID>\nOr choose a different port in config", port)
				case "linux":
					remediation = fmt.Sprintf("Find process using port: ss -tlnp | grep :%d or lsof -i :%d\nKill process: kill -9 <PID>\nOr choose a different port in config", port, port)
				case "windows":
					remediation = fmt.Sprintf("Find process using port: netstat -ano | findstr :%d\nKill process: taskkill /PID <PID> /F\nOr choose a different port in config", port)
				default:
					remediation = fmt.Sprintf("Choose a different port or stop the service using port %d", port)
				}
			} else if strings.Contains(err.Error(), "permission denied") {
				remediation = fmt.Sprintf("Use a port number above 1024 (e.g., 9090) or run with elevated privileges (sudo/admin)")
			} else {
				remediation = fmt.Sprintf("Verify network configuration and try a different port (e.g., 9090, 9091, 8080)")
			}
			
			result.Errors = append(result.Errors, ValidationError{
				Field:       "metrics.export.prometheus.port",
				Message:     fmt.Sprintf("Prometheus port %d is not available: %v", port, err),
				Remediation: remediation,
			})
		}
		
		// Only warn about very aggressive intervals
		if metrics.Export.Prometheus.UpdateInterval < 500*time.Millisecond {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "metrics.export.prometheus.update_interval",
				Message: fmt.Sprintf("Prometheus update interval of %v is very aggressive and may cause high CPU usage", metrics.Export.Prometheus.UpdateInterval),
			})
		}
		
		// Validate counter state cleanup configuration
		if metrics.Export.Prometheus.CounterStateCleanupInterval > 0 {
			// Warn if cleanup interval is too frequent
			if metrics.Export.Prometheus.CounterStateCleanupInterval < 5*time.Minute {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   "metrics.export.prometheus.counter_state_cleanup_interval",
					Message: fmt.Sprintf("Counter state cleanup interval of %v is very frequent and may impact performance", 
						metrics.Export.Prometheus.CounterStateCleanupInterval),
				})
			}
			
			// Warn if max age is less than cleanup interval
			if metrics.Export.Prometheus.CounterStateMaxAge > 0 &&
				metrics.Export.Prometheus.CounterStateMaxAge < metrics.Export.Prometheus.CounterStateCleanupInterval {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   "metrics.export.prometheus.counter_state_max_age",
					Message: fmt.Sprintf("Counter state max age (%v) is less than cleanup interval (%v), entries may be removed too quickly",
						metrics.Export.Prometheus.CounterStateMaxAge, 
						metrics.Export.Prometheus.CounterStateCleanupInterval),
				})
			}
			
			// Warn if max age is too short
			if metrics.Export.Prometheus.CounterStateMaxAge > 0 &&
				metrics.Export.Prometheus.CounterStateMaxAge < 10*time.Minute {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   "metrics.export.prometheus.counter_state_max_age",
					Message: fmt.Sprintf("Counter state max age of %v is very short, may cause premature cleanup of active entries",
						metrics.Export.Prometheus.CounterStateMaxAge),
				})
			}
		}
	}
	
	// Check Zabbix configuration
	if metrics.Export.Zabbix.Enabled {
		if err := testTCPConnectivity(
			fmt.Sprintf("%s:%d", metrics.Export.Zabbix.Server, metrics.Export.Zabbix.Port),
			metrics.Export.Zabbix.Timeout,
		); err != nil {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "metrics.export.zabbix.server",
				Message: fmt.Sprintf("Cannot connect to Zabbix server %s:%d: %v",
					metrics.Export.Zabbix.Server, metrics.Export.Zabbix.Port, err),
				Remediation: fmt.Sprintf("Verify Zabbix server is running and accepting connections on %s:%d. Check firewall rules and Zabbix server configuration",
					metrics.Export.Zabbix.Server, metrics.Export.Zabbix.Port),
			})
		}
		
		if metrics.Export.Zabbix.SendInterval < metrics.CalculationInterval {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "metrics.export.zabbix.send_interval",
				Message: "Zabbix send interval is less than metrics calculation interval, may send duplicate data",
			})
		}
	}
	
	return nil
}

// checkPortAvailability checks if a port is available for binding
func checkPortAvailability(port int) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		// Enhance error message with common causes
		if strings.Contains(err.Error(), "address already in use") || strings.Contains(err.Error(), "bind") {
			return fmt.Errorf("port %d is already in use by another process", port)
		}
		if strings.Contains(err.Error(), "permission denied") {
			return fmt.Errorf("permission denied to bind port %d (ports below 1024 require root/admin privileges)", port)
		}
		return fmt.Errorf("cannot bind to port %d: %w", port, err)
	}
	listener.Close()
	return nil
}

// validatePassivePrivileges checks for required capabilities
func validatePassivePrivileges(result *ValidationResult) error {
	switch runtime.GOOS {
	case "linux":
		// Check for CAP_NET_RAW capability
		if !hasNetRawCapability() {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "passive.enabled",
				Message: "Passive monitoring requires CAP_NET_RAW capability on Linux",
				Remediation: "Grant the capability with: sudo setcap cap_net_raw+ep ./dns-monitor\n" +
					"Or run as root: sudo ./dns-monitor\n" +
					"For systemd services, add: AmbientCapabilities=CAP_NET_RAW",
			})
		}
		
	case "darwin", "windows":
		// Check if running with admin privileges
		if !isAdmin() {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field: "passive.enabled",
				Message: fmt.Sprintf("Passive monitoring on %s typically requires administrator privileges",
					runtime.GOOS),
			})
		}
		
	default:
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "passive.enabled",
			Message: fmt.Sprintf("Passive monitoring privilege requirements unknown for %s", runtime.GOOS),
		})
	}
	
	return nil
}

// hasNetRawCapability checks for CAP_NET_RAW on Linux
func hasNetRawCapability() bool {
	// Try to create a raw socket
	conn, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// isAdmin checks if running with admin privileges
func isAdmin() bool {
	switch runtime.GOOS {
	case "windows":
		// On Windows, check if we can open a privileged file
		_, err := os.Open("\\\\.\\PHYSICALDRIVE0")
		return err == nil
		
	case "darwin", "linux":
		// On Unix-like systems, check if we're root
		return os.Geteuid() == 0
		
	default:
		return false
	}
}

// validateConfigurationDependencies checks cross-configuration requirements
func validateConfigurationDependencies(cfg *Config, result *ValidationResult) error {
	// Cache analysis dependencies
	if cfg.Cache.Enabled {
		if len(cfg.Cache.RecursiveServers) > 0 {
			// Check that specified recursive servers exist
			serverMap := make(map[string]bool)
			for _, server := range cfg.DNS.Servers {
				serverMap[server.Name] = true
			}
			
			for _, recursiveServer := range cfg.Cache.RecursiveServers {
				if !serverMap[recursiveServer] {
					result.Errors = append(result.Errors, ValidationError{
						Field:   "cache.recursive_servers",
						Message: fmt.Sprintf("Recursive server '%s' not found in DNS servers configuration", recursiveServer),
						Remediation: fmt.Sprintf("Add '%s' to dns.servers or remove it from cache.recursive_servers",
							recursiveServer),
					})
				}
			}
		}
		
		if cfg.Cache.AnalysisInterval > cfg.Monitor.Interval {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "cache.analysis_interval",
				Message: "Cache analysis interval is greater than monitor interval, may miss cache events",
			})
		}
	}
	
	// Network analysis dependencies
	if cfg.Network.Enabled {
		if len(cfg.Network.NetworkInterfaces) > 0 {
			// Check that specified interfaces exist
			interfaces, err := net.Interfaces()
			if err != nil {
				// If we can't enumerate interfaces, add a warning instead of failing
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   "network.network_interfaces",
					Message: fmt.Sprintf("Unable to enumerate network interfaces: %v", err),
				})
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   "network.network_interfaces",
					Message: "Network interface validation skipped - interfaces will be checked at runtime",
				})
			} else {
				interfaceMap := make(map[string]bool)
				for _, iface := range interfaces {
					interfaceMap[iface.Name] = true
				}
				
				for _, ifaceName := range cfg.Network.NetworkInterfaces {
					if !interfaceMap[ifaceName] {
						// Make this a warning instead of an error for better flexibility
						result.Warnings = append(result.Warnings, ValidationWarning{
							Field:   "network.network_interfaces",
							Message: fmt.Sprintf("Network interface '%s' not currently available", ifaceName),
						})
						// Provide helpful information about available interfaces
						availableInterfaces := getInterfaceNames()
						if len(availableInterfaces) > 0 {
							result.Warnings = append(result.Warnings, ValidationWarning{
								Field:   "network.network_interfaces",
								Message: fmt.Sprintf("Available interfaces: %v", availableInterfaces),
							})
						}
					}
				}
			}
		}
		
		if cfg.Network.PingIntegration && !isAdmin() {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "network.ping_integration",
				Message: "Ping integration may require administrator privileges on some systems",
			})
		}
	}
	
	// Passive monitoring and network analysis interaction
	if cfg.Passive.Enabled && cfg.Network.Enabled {
		if len(cfg.Passive.Interfaces) > 0 && len(cfg.Network.NetworkInterfaces) > 0 {
			// Check for interface conflicts
			passiveSet := make(map[string]bool)
			for _, iface := range cfg.Passive.Interfaces {
				passiveSet[iface] = true
			}
			
			for _, iface := range cfg.Network.NetworkInterfaces {
				if !passiveSet[iface] {
					result.Warnings = append(result.Warnings, ValidationWarning{
						Field: "network.network_interfaces",
						Message: fmt.Sprintf("Network interface '%s' not monitored by passive mode, metrics may be incomplete",
							iface),
					})
				}
			}
		}
	}
	
	return nil
}

// getInterfaceNames returns a list of available network interface names
func getInterfaceNames() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return []string{}
	}
	
	names := make([]string, 0, len(interfaces))
	for _, iface := range interfaces {
		names = append(names, iface.Name)
	}
	
	return names
}

// extractHostnameFromAddress extracts the hostname from a URL or address string
func extractHostnameFromAddress(address string) string {
	// Remove protocol prefixes
	address = strings.TrimPrefix(address, "https://")
	address = strings.TrimPrefix(address, "http://")
	
	// Remove port if present
	if idx := strings.Index(address, ":"); idx != -1 {
		address = address[:idx]
	}
	// Remove path if present
	if idx := strings.Index(address, "/"); idx != -1 {
		address = address[:idx]
	}
	
	return address
}

// PrintValidationResult formats and prints validation results
func PrintValidationResult(result *ValidationResult) {
	if result.Valid {
		fmt.Println("✓ Configuration is valid")
		
		if len(result.Warnings) > 0 {
			fmt.Printf("\n⚠ %d warning(s):\n", len(result.Warnings))
			for _, warning := range result.Warnings {
				fmt.Printf("  - %s: %s\n", warning.Field, warning.Message)
			}
		}
	} else {
		fmt.Printf("✗ Configuration validation failed with %d error(s):\n\n", len(result.Errors))
		
		for i, err := range result.Errors {
			fmt.Printf("%d. Field: %s\n", i+1, err.Field)
			fmt.Printf("   Error: %s\n", err.Message)
			fmt.Printf("   Fix: %s\n\n", err.Remediation)
		}
		
		if len(result.Warnings) > 0 {
			fmt.Printf("Additionally, %d warning(s) were found:\n", len(result.Warnings))
			for _, warning := range result.Warnings {
				fmt.Printf("  - %s: %s\n", warning.Field, warning.Message)
			}
		}
	}
}