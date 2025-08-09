package dns

import (
	"fmt"
	"math"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	
	"github.com/randax/dns-monitoring/internal/config"
)

// NetworkAnalyzer provides network-level metrics collection for DNS
type NetworkAnalyzer struct {
	mu               sync.RWMutex
	queryTracker     map[string]*QueryTracker
	latencyHistory   map[string][]time.Duration
	interfaceStats   map[string]*InterfaceMetrics
	hopCountCache    map[string]int
	sampleSize       int
}

// QueryTracker tracks sent queries for packet loss detection
type QueryTracker struct {
	SentQueries      int64
	ReceivedResponses int64
	LastUpdate       time.Time
	TimeWindow       time.Duration
	QueryTimestamps  []time.Time
	ResponseTimes    []time.Duration
}

// InterfaceMetrics tracks per-interface network statistics
type InterfaceMetrics struct {
	PacketsSent     int64
	PacketsReceived int64
	TotalLatency    time.Duration
	LatencySamples  int64
	JitterValues    []float64
}


// NewNetworkAnalyzer creates a new network analyzer instance
func NewNetworkAnalyzer(sampleSize int) *NetworkAnalyzer {
	return &NetworkAnalyzer{
		queryTracker:   make(map[string]*QueryTracker),
		latencyHistory: make(map[string][]time.Duration),
		interfaceStats: make(map[string]*InterfaceMetrics),
		hopCountCache:  make(map[string]int),
		sampleSize:     sampleSize,
	}
}

// AnalyzeResult analyzes network metrics for a DNS result
func (na *NetworkAnalyzer) AnalyzeResult(result *Result, cfg config.NetworkConfig) {
	if !cfg.Enabled {
		return
	}
	
	// Track query for packet loss detection
	if cfg.PacketLossDetection {
		na.trackQuery(result)
		result.PacketLoss = na.detectPacketLoss(result.Server, cfg.NetworkTimeouts)
	}
	
	// Calculate jitter if enabled
	if cfg.JitterCalculation {
		result.Jitter = na.calculateJitter(result.Server, result.Duration)
	}
	
	// Measure network latency
	result.NetworkLatency = na.measureNetworkLatency(result)
	
	// Analyze hop count only if enabled
	if cfg.HopCountEstimation {
		if cfg.UseTraceroute {
			result.HopCount = na.performTraceroute(result.Server)
		} else {
			result.HopCount = na.analyzeHopCount(result.Server)
		}
	} else {
		result.HopCount = -1 // Indicate hop count is disabled
	}
	
	// Calculate overall network quality score
	result.NetworkQuality = na.calculateNetworkQuality(
		result.PacketLoss,
		result.Jitter,
		result.NetworkLatency,
		cfg,
	)
	
	// Update interface statistics
	na.updateInterfaceStats(result)
}

// trackQuery records a DNS query for packet loss tracking
func (na *NetworkAnalyzer) trackQuery(result *Result) {
	na.mu.Lock()
	defer na.mu.Unlock()
	
	tracker, exists := na.queryTracker[result.Server]
	if !exists {
		tracker = &QueryTracker{
			TimeWindow:      5 * time.Minute,
			QueryTimestamps: make([]time.Time, 0, na.sampleSize),
			ResponseTimes:   make([]time.Duration, 0, na.sampleSize),
		}
		na.queryTracker[result.Server] = tracker
	}
	
	// Record query
	tracker.SentQueries++
	tracker.QueryTimestamps = append(tracker.QueryTimestamps, result.Timestamp)
	
	// Record response if successful
	if result.Error == nil {
		tracker.ReceivedResponses++
		tracker.ResponseTimes = append(tracker.ResponseTimes, result.Duration)
	}
	
	tracker.LastUpdate = result.Timestamp
	
	// Trim old data
	na.trimOldQueryData(tracker, result.Timestamp)
}

// detectPacketLoss calculates packet loss percentage with robust error handling
func (na *NetworkAnalyzer) detectPacketLoss(server string, timeWindow time.Duration) float64 {
	// Input validation
	if server == "" || timeWindow <= 0 {
		return 0.0
	}
	
	na.mu.RLock()
	defer na.mu.RUnlock()
	
	tracker, exists := na.queryTracker[server]
	if !exists || tracker.SentQueries == 0 {
		return 0.0
	}
	
	// Additional validation for data consistency
	if tracker.ReceivedResponses > tracker.SentQueries {
		// Data inconsistency - reset to avoid negative loss
		return 0.0
	}
	
	// Calculate loss percentage over time window
	sent := float64(tracker.SentQueries)
	received := float64(tracker.ReceivedResponses)
	
	if sent == 0 {
		return 0.0
	}
	
	lossRate := (sent - received) / sent
	
	// Ensure result is within valid range [0, 1]
	return math.Max(0, math.Min(1, lossRate))
}

// calculateJitter measures variation in response times with robust error handling
func (na *NetworkAnalyzer) calculateJitter(server string, currentLatency time.Duration) time.Duration {
	// Input validation
	if server == "" || currentLatency < 0 {
		return 0
	}
	
	na.mu.Lock()
	defer na.mu.Unlock()
	
	// Update latency history
	history, exists := na.latencyHistory[server]
	if !exists {
		history = make([]time.Duration, 0, na.sampleSize)
	}
	
	// Bounds checking for extreme values
	if currentLatency > 10*time.Second {
		// Likely an error or timeout, don't include in jitter calculation
		return 0
	}
	
	history = append(history, currentLatency)
	
	// Keep only recent samples
	if len(history) > na.sampleSize {
		history = history[len(history)-na.sampleSize:]
	}
	
	na.latencyHistory[server] = history
	
	// Need at least 3 samples for jitter calculation
	if len(history) < 3 {
		return 0
	}
	
	// Calculate standard deviation of latencies with overflow protection
	var sum, sumSquares float64
	for _, latency := range history {
		val := float64(latency.Nanoseconds())
		// Check for potential overflow
		if val > 1e15 { // More than ~16 minutes in nanoseconds
			continue
		}
		sum += val
		sumSquares += val * val
	}
	
	n := float64(len(history))
	if n == 0 {
		return 0
	}
	
	mean := sum / n
	variance := (sumSquares / n) - (mean * mean)
	
	// Handle floating point precision issues
	if variance < 0 {
		variance = 0
	}
	
	stdDev := math.Sqrt(variance)
	
	// Bounds check the result
	jitter := time.Duration(stdDev)
	if jitter > 10*time.Second {
		// Cap extreme jitter values
		jitter = 10 * time.Second
	}
	
	return jitter
}

// measureNetworkLatency estimates pure network transit time
func (na *NetworkAnalyzer) measureNetworkLatency(result *Result) time.Duration {
	// Network latency is approximately the total duration minus DNS processing time
	// For cache hits, network latency is very low
	if result.CacheHit {
		return result.Duration / 10 // Estimate 10% is network for cache hits
	}
	
	// For non-cached responses, estimate based on protocol
	var processingOverhead time.Duration
	switch result.Protocol {
	case ProtocolUDP:
		processingOverhead = 5 * time.Millisecond
	case ProtocolTCP:
		processingOverhead = 10 * time.Millisecond
	case ProtocolDoT:
		processingOverhead = 15 * time.Millisecond
	case ProtocolDoH:
		processingOverhead = 20 * time.Millisecond
	default:
		processingOverhead = 5 * time.Millisecond
	}
	
	networkLatency := result.Duration - processingOverhead
	if networkLatency < 0 {
		networkLatency = result.Duration / 2
	}
	
	return networkLatency
}

// analyzeHopCount determines network path length to DNS server
// Returns -1 if hop count cannot be determined or estimation is disabled
func (na *NetworkAnalyzer) analyzeHopCount(serverAddress string) int {
	// Check if hop count estimation is disabled (indicated by negative value in cache)
	na.mu.RLock()
	hopCount, exists := na.hopCountCache[serverAddress]
	na.mu.RUnlock()
	
	if exists {
		return hopCount
	}
	
	// Parse server address to get IP
	host, _, err := net.SplitHostPort(serverAddress)
	if err != nil {
		// Address might not have port
		host = serverAddress
	}
	
	// Try to resolve to IP if hostname
	ip := net.ParseIP(host)
	if ip == nil {
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			// Cannot determine hop count - cache as unknown
			na.mu.Lock()
			na.hopCountCache[serverAddress] = -1
			na.mu.Unlock()
			return -1
		}
		ip = ips[0]
	}
	
	// Provide estimated hop count with warning about accuracy
	hopCount = na.estimateHopCount(ip)
	
	// Cache the result
	na.mu.Lock()
	na.hopCountCache[serverAddress] = hopCount
	na.mu.Unlock()
	
	return hopCount
}

// estimateHopCount provides a rough hop count estimation based on IP characteristics
// NOTE: This is a heuristic estimation and may not reflect actual network topology
// For accurate hop counts, consider implementing actual traceroute functionality
func (na *NetworkAnalyzer) estimateHopCount(ip net.IP) int {
	if ip.IsLoopback() {
		return 0  // Loopback - no network hops
	}
	
	if ip.IsPrivate() {
		// Private network - estimate based on common LAN configurations
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return 1  // Same subnet
		}
		return 3  // Typical local network with routing
	}
	
	// Public IPs - heuristic based on well-known DNS providers
	// These are rough estimates and actual hop counts will vary by location
	if ip.To4() != nil {
		// IPv4 addresses
		firstOctet := ip.To4()[0]
		switch {
		case firstOctet == 8:  // Google Public DNS (8.8.8.8, 8.8.4.4)
			return 12  // Typical global anycast
		case firstOctet == 1:  // Cloudflare (1.1.1.1, 1.0.0.1)
			return 10  // Optimized anycast network
		case firstOctet == 9:  // Quad9 (9.9.9.9)
			return 14  // Global anycast
		case firstOctet == 208 && ip.To4()[1] == 67:  // OpenDNS
			return 13
		case firstOctet >= 224:  // Multicast or reserved
			return -1  // Cannot estimate
		default:
			// Generic public IP - estimate based on typical internet paths
			return 15
		}
	}
	
	// IPv6 - typically more hops due to tunneling and routing complexity
	if ip.To16() != nil {
		// Check for well-known IPv6 DNS providers
		ipStr := ip.String()
		switch {
		case ipStr[:4] == "2001" && ipStr[5:9] == "4860":  // Google IPv6
			return 14
		case ipStr[:4] == "2606" && ipStr[5:9] == "4700":  // Cloudflare IPv6
			return 12
		case ipStr[:4] == "2620" && ipStr[5:7] == "fe":    // Quad9 IPv6
			return 16
		default:
			return 18  // Typical IPv6 path
		}
	}
	
	return -1  // Unknown or cannot estimate
}

// calculateNetworkQuality computes composite network performance score with robust handling
func (na *NetworkAnalyzer) calculateNetworkQuality(packetLoss float64, jitter, latency time.Duration, cfg config.NetworkConfig) float64 {
	// Input validation
	if packetLoss < 0 || packetLoss > 1 {
		packetLoss = math.Max(0, math.Min(1, packetLoss))
	}
	if jitter < 0 {
		jitter = 0
	}
	if latency < 0 {
		latency = 0
	}
	
	// Score from 0 (worst) to 100 (best)
	score := 100.0
	
	// Packet loss impact (0-40 points)
	if packetLoss > 0 && cfg.PacketLossThreshold > 0 {
		lossScore := math.Max(0, 40*(1-packetLoss/cfg.PacketLossThreshold))
		score -= (40 - lossScore)
	}
	
	// Jitter impact (0-30 points)
	if jitter > 0 && cfg.JitterThreshold > 0 {
		jitterRatio := float64(jitter) / float64(cfg.JitterThreshold)
		jitterScore := math.Max(0, 30*(1-jitterRatio))
		score -= (30 - jitterScore)
	}
	
	// Latency impact (0-30 points)
	baselineLatency := 50 * time.Millisecond
	if latency > baselineLatency {
		latencyRatio := float64(latency) / float64(baselineLatency*10)
		latencyScore := math.Max(0, 30*(1-latencyRatio))
		score -= (30 - latencyScore)
	}
	
	// Ensure score is within valid range
	return math.Max(0, math.Min(100, score))
}

// updateInterfaceStats updates per-interface statistics
func (na *NetworkAnalyzer) updateInterfaceStats(result *Result) {
	// Determine interface (simplified - would need actual interface detection)
	interfaceName := "default"
	
	na.mu.Lock()
	defer na.mu.Unlock()
	
	stats, exists := na.interfaceStats[interfaceName]
	if !exists {
		stats = &InterfaceMetrics{
			JitterValues: make([]float64, 0, na.sampleSize),
		}
		na.interfaceStats[interfaceName] = stats
	}
	
	// Update statistics
	stats.PacketsSent++
	if result.Error == nil {
		stats.PacketsReceived++
		stats.TotalLatency += result.Duration
		stats.LatencySamples++
	}
	
	// Calculate and store jitter value
	if result.Jitter > 0 {
		jitterMs := float64(result.Jitter.Milliseconds())
		stats.JitterValues = append(stats.JitterValues, jitterMs)
		
		// Keep only recent jitter values
		if len(stats.JitterValues) > na.sampleSize {
			stats.JitterValues = stats.JitterValues[len(stats.JitterValues)-na.sampleSize:]
		}
	}
}

// trimOldQueryData removes data outside the time window
func (na *NetworkAnalyzer) trimOldQueryData(tracker *QueryTracker, currentTime time.Time) {
	cutoff := currentTime.Add(-tracker.TimeWindow)
	
	// Find index of first timestamp within window
	keepIndex := 0
	for i, ts := range tracker.QueryTimestamps {
		if ts.After(cutoff) {
			keepIndex = i
			break
		}
	}
	
	// Trim old data
	if keepIndex > 0 {
		tracker.QueryTimestamps = tracker.QueryTimestamps[keepIndex:]
		if keepIndex < len(tracker.ResponseTimes) {
			tracker.ResponseTimes = tracker.ResponseTimes[keepIndex:]
		}
	}
	
	// Limit size to prevent unbounded growth
	if len(tracker.QueryTimestamps) > na.sampleSize*10 {
		tracker.QueryTimestamps = tracker.QueryTimestamps[len(tracker.QueryTimestamps)-na.sampleSize*10:]
	}
	if len(tracker.ResponseTimes) > na.sampleSize*10 {
		tracker.ResponseTimes = tracker.ResponseTimes[len(tracker.ResponseTimes)-na.sampleSize*10:]
	}
}

// performTraceroute performs actual traceroute to measure hop count
func (na *NetworkAnalyzer) performTraceroute(serverAddress string) int {
	// Check cache first
	na.mu.RLock()
	hopCount, exists := na.hopCountCache[serverAddress]
	na.mu.RUnlock()
	
	if exists && hopCount != -1 {
		return hopCount
	}
	
	// Parse server address to get IP/hostname
	host, _, err := net.SplitHostPort(serverAddress)
	if err != nil {
		host = serverAddress
	}
	
	// Determine the appropriate traceroute command based on OS
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Windows uses tracert
		cmd = exec.Command("tracert", "-h", "30", "-w", "1000", host)
	case "darwin":
		// macOS uses traceroute
		cmd = exec.Command("traceroute", "-m", "30", "-w", "1", "-q", "1", host)
	default:
		// Linux and others
		cmd = exec.Command("traceroute", "-m", "30", "-w", "1", "-q", "1", host)
	}
	
	// Set timeout for the command
	output, err := runCommandWithTimeout(cmd, 10*time.Second)
	if err != nil {
		// Fall back to heuristic estimation if traceroute fails
		fmt.Printf("Traceroute failed for %s: %v, falling back to estimation\n", host, err)
		return na.analyzeHopCount(serverAddress)
	}
	
	// Parse traceroute output to count hops
	hopCount = parseTracerouteOutput(output, runtime.GOOS)
	
	// Cache the result
	na.mu.Lock()
	na.hopCountCache[serverAddress] = hopCount
	na.mu.Unlock()
	
	return hopCount
}

// runCommandWithTimeout runs a command with a timeout
func runCommandWithTimeout(cmd *exec.Cmd, timeout time.Duration) (string, error) {
	// Create a channel to receive the command result
	done := make(chan error, 1)
	var output []byte
	var cmdErr error
	
	go func() {
		output, cmdErr = cmd.CombinedOutput()
		done <- cmdErr
	}()
	
	select {
	case <-time.After(timeout):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return "", fmt.Errorf("command timed out after %v", timeout)
	case err := <-done:
		if err != nil {
			return string(output), fmt.Errorf("command failed: %v, output: %s", err, string(output))
		}
		return string(output), nil
	}
}

// parseTracerouteOutput parses traceroute output to count hops
func parseTracerouteOutput(output string, os string) int {
	lines := strings.Split(output, "\n")
	maxHop := 0
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		
		// Different parsing based on OS
		switch os {
		case "windows":
			// Windows tracert format: "  1    <1 ms    <1 ms    <1 ms  192.168.1.1"
			parts := strings.Fields(line)
			if len(parts) > 0 {
				if hopNum, err := strconv.Atoi(parts[0]); err == nil {
					if hopNum > maxHop {
						maxHop = hopNum
					}
				}
			}
		default:
			// Unix traceroute format: " 1  192.168.1.1 (192.168.1.1)  0.123 ms"
			parts := strings.Fields(line)
			if len(parts) > 0 {
				if hopNum, err := strconv.Atoi(parts[0]); err == nil {
					if hopNum > maxHop {
						maxHop = hopNum
					}
				}
			}
		}
	}
	
	// If we couldn't parse any hops, return -1
	if maxHop == 0 {
		return -1
	}
	
	return maxHop
}

// GetInterfaceStats returns statistics for all monitored interfaces
func (na *NetworkAnalyzer) GetInterfaceStats() map[string]*InterfaceMetrics {
	na.mu.RLock()
	defer na.mu.RUnlock()
	
	// Create a copy to avoid race conditions
	statsCopy := make(map[string]*InterfaceMetrics)
	for name, stats := range na.interfaceStats {
		statsCopy[name] = &InterfaceMetrics{
			PacketsSent:     stats.PacketsSent,
			PacketsReceived: stats.PacketsReceived,
			TotalLatency:    stats.TotalLatency,
			LatencySamples:  stats.LatencySamples,
			JitterValues:    append([]float64{}, stats.JitterValues...),
		}
	}
	
	return statsCopy
}