package dns

import (
	"strings"
	"sync"
	"time"
	
	"github.com/randax/dns-monitoring/internal/config"
)

// CacheAnalyzer provides cache performance analysis for DNS responses
type CacheAnalyzer struct {
	mu              sync.RWMutex
	responseHistory map[string]*ResponseHistory
	baselines       map[string]time.Duration
	ttlMu           sync.RWMutex  // Separate mutex for TTL tracking to reduce contention
	ttlTracker      map[string]*TTLRecord
	config          *config.CacheConfig
	lastCleanup     time.Time
	memoryUsage     int64 // Estimated memory usage in bytes
	domainCount     int
	serverCount     int
}

// ResponseHistory tracks historical responses for pattern analysis
type ResponseHistory struct {
	Domain      string
	QueryType   string
	Responses   []ResponseRecord
	LastUpdated time.Time
}

// ResponseRecord stores individual response data for analysis
type ResponseRecord struct {
	Timestamp    time.Time
	Duration     time.Duration
	TTL          time.Duration
	Answers      []string
	ServerName   string
	CacheHit     bool
}

// TTLRecord tracks TTL values over time for cache detection
type TTLRecord struct {
	mu               sync.RWMutex  // Per-record mutex for fine-grained locking
	InitialTTL       time.Duration
	CurrentTTL       time.Duration
	LastObserved     time.Time
	ObservationCount int
}

// NewCacheAnalyzer creates a new cache analyzer instance
func NewCacheAnalyzer(cfg *config.CacheConfig) *CacheAnalyzer {
	if cfg == nil {
		cfg = &config.CacheConfig{
			MaxTrackedDomains: 1000,
			MaxTrackedServers: 100,
			MemoryLimitMB:     50,
			CleanupInterval:   5 * time.Minute,
			DataRetention:     1 * time.Hour,
		}
	}
	
	ca := &CacheAnalyzer{
		responseHistory: make(map[string]*ResponseHistory),
		baselines:       make(map[string]time.Duration),
		ttlTracker:      make(map[string]*TTLRecord),
		config:          cfg,
		lastCleanup:     time.Now(),
		memoryUsage:     0,
		domainCount:     0,
		serverCount:     0,
	}
	
	// Start periodic cleanup goroutine
	go ca.periodicCleanup()
	
	return ca
}

// AnalyzeResult analyzes a DNS result for cache performance indicators
func (ca *CacheAnalyzer) AnalyzeResult(result *Result, cfg config.CacheConfig) {
	if !cfg.Enabled {
		return
	}

	// Determine if server is recursive
	result.RecursiveServer = ca.isRecursiveServer(result.Server, cfg.RecursiveServers)
	
	// Apply detection methods
	for _, method := range cfg.DetectionMethods {
		switch method {
		case "timing":
			ca.detectCacheHitByTiming(result, cfg.CacheHitThreshold)
		case "ttl":
			ca.detectCacheHitByTTL(result)
		case "pattern":
			ca.detectCacheHitByPattern(result)
		}
	}
	
	// Calculate cache efficiency if enabled
	if cfg.CacheEfficiencyMetrics {
		result.CacheEfficiency = ca.calculateCacheEfficiency(result)
	}
	
	// Store response for future analysis
	ca.storeResponse(result)
}

// detectCacheHitByTiming uses response time to detect cache hits
func (ca *CacheAnalyzer) detectCacheHitByTiming(result *Result, threshold time.Duration) {
	ca.mu.RLock()
	baseline, hasBaseline := ca.baselines[result.Server]
	ca.mu.RUnlock()
	
	// If response is significantly faster than baseline, likely a cache hit
	if result.Duration < threshold {
		result.CacheHit = true
		result.CacheAge = ca.estimateCacheAge(result)
	}
	
	// Update baseline for authoritative responses
	if !result.CacheHit && result.Error == nil {
		ca.updateBaseline(result.Server, result.Duration)
	}
	
	// Compare against historical patterns
	if hasBaseline && result.Duration < baseline/3 {
		result.CacheHit = true
	}
}

// detectCacheHitByTTL tracks TTL decrements to identify cached responses
func (ca *CacheAnalyzer) detectCacheHitByTTL(result *Result) {
	if result.TTL == 0 {
		return
	}
	
	key := ca.getCacheKey(result)
	
	// Use separate TTL mutex for better concurrency
	ca.ttlMu.RLock()
	record, exists := ca.ttlTracker[key]
	ca.ttlMu.RUnlock()
	
	if !exists {
		// First observation of this record - need write lock
		ca.ttlMu.Lock()
		// Double-check after acquiring write lock
		record, exists = ca.ttlTracker[key]
		if !exists {
			record = &TTLRecord{
				InitialTTL:       result.TTL,
				CurrentTTL:       result.TTL,
				LastObserved:     result.Timestamp,
				ObservationCount: 1,
			}
			ca.ttlTracker[key] = record
		}
		ca.ttlMu.Unlock()
		return
	}
	
	// Use per-record mutex for updating existing record
	record.mu.Lock()
	defer record.mu.Unlock()
	
	// Calculate expected TTL based on time elapsed
	elapsed := result.Timestamp.Sub(record.LastObserved)
	expectedTTL := record.CurrentTTL - elapsed
	
	// If TTL decreased as expected, likely served from cache
	if result.TTL < record.CurrentTTL && result.TTL >= expectedTTL-time.Second {
		result.CacheHit = true
		result.CacheAge = record.InitialTTL - result.TTL
	}
	
	// Update record
	record.CurrentTTL = result.TTL
	record.LastObserved = result.Timestamp
	record.ObservationCount++
}

// detectCacheHitByPattern analyzes response patterns for cache indicators
func (ca *CacheAnalyzer) detectCacheHitByPattern(result *Result) {
	key := ca.getCacheKey(result)
	
	ca.mu.RLock()
	history, exists := ca.responseHistory[key]
	ca.mu.RUnlock()
	
	if !exists || len(history.Responses) < 3 {
		return
	}
	
	// Check for identical responses in short time window
	recentWindow := 5 * time.Second
	identicalCount := 0
	
	for i := len(history.Responses) - 1; i >= 0 && i >= len(history.Responses)-5; i-- {
		resp := history.Responses[i]
		if result.Timestamp.Sub(resp.Timestamp) > recentWindow {
			break
		}
		
		if ca.answersMatch(result.Answers, resp.Answers) {
			identicalCount++
		}
	}
	
	// Multiple identical responses suggest caching
	if identicalCount >= 2 {
		result.CacheHit = true
	}
}

// calculateCacheEfficiency computes cache performance score
func (ca *CacheAnalyzer) calculateCacheEfficiency(result *Result) float64 {
	if !result.CacheHit {
		return 0.0
	}
	
	ca.mu.RLock()
	history, exists := ca.responseHistory[ca.getCacheKey(result)]
	baseline, hasBaseline := ca.baselines[result.Server]
	ca.mu.RUnlock()
	
	if !exists || !hasBaseline {
		return 0.5 // Default efficiency for cache hits without history
	}
	
	// Calculate efficiency based on multiple factors
	var efficiency float64
	
	// Factor 1: Speed improvement (40% weight)
	speedRatio := float64(baseline) / float64(result.Duration)
	if speedRatio > 10 {
		speedRatio = 10 // Cap at 10x improvement
	}
	efficiency += (speedRatio / 10) * 0.4
	
	// Factor 2: Hit rate (40% weight) 
	hitCount := 0
	totalCount := len(history.Responses)
	if totalCount > 0 {
		for _, resp := range history.Responses {
			if resp.CacheHit {
				hitCount++
			}
		}
		hitRate := float64(hitCount) / float64(totalCount)
		efficiency += hitRate * 0.4
	}
	
	// Factor 3: Cache freshness (20% weight)
	if result.CacheAge < 60*time.Second {
		efficiency += 0.2
	} else if result.CacheAge < 300*time.Second {
		efficiency += 0.1
	}
	
	return efficiency
}

// estimateCacheAge estimates how long a response has been cached
func (ca *CacheAnalyzer) estimateCacheAge(result *Result) time.Duration {
	key := ca.getCacheKey(result)
	
	// Check TTL tracking with separate mutex
	ca.ttlMu.RLock()
	record, exists := ca.ttlTracker[key]
	ca.ttlMu.RUnlock()
	
	if exists {
		record.mu.RLock()
		age := record.InitialTTL - result.TTL
		record.mu.RUnlock()
		return age
	}
	
	// Check response history
	ca.mu.RLock()
	history, exists := ca.responseHistory[key]
	ca.mu.RUnlock()
	
	if exists && len(history.Responses) > 0 {
		// Find first response with same answers
		for _, resp := range history.Responses {
			if ca.answersMatch(result.Answers, resp.Answers) {
				return result.Timestamp.Sub(resp.Timestamp)
			}
		}
	}
	
	// Default estimate based on response time
	if result.Duration < 5*time.Millisecond {
		return 30 * time.Second // Very fast, likely fresh cache
	} else if result.Duration < 20*time.Millisecond {
		return 2 * time.Minute // Moderately fast
	}
	
	return 5 * time.Minute // Default for cache hits
}

// Helper functions

func (ca *CacheAnalyzer) getCacheKey(result *Result) string {
	return result.Domain + ":" + result.QueryType + ":" + result.Server
}

func (ca *CacheAnalyzer) isRecursiveServer(serverName string, recursiveServers []string) bool {
	if len(recursiveServers) == 0 {
		// If no specific servers configured, assume all are recursive
		return true
	}
	
	for _, rs := range recursiveServers {
		if serverName == rs {
			return true
		}
	}
	return false
}

func (ca *CacheAnalyzer) answersMatch(a1, a2 []string) bool {
	if len(a1) != len(a2) {
		return false
	}
	
	// Create map for efficient comparison
	answerMap := make(map[string]bool)
	for _, ans := range a1 {
		answerMap[ans] = true
	}
	
	for _, ans := range a2 {
		if !answerMap[ans] {
			return false
		}
	}
	
	return true
}

func (ca *CacheAnalyzer) updateBaseline(server string, duration time.Duration) {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	
	current, exists := ca.baselines[server]
	if !exists || duration > current {
		// Update to slower response as more reliable baseline
		ca.baselines[server] = duration
	} else {
		// Weighted average to smooth baseline
		ca.baselines[server] = (current*9 + duration) / 10
	}
}

func (ca *CacheAnalyzer) storeResponse(result *Result) {
	// Check if cleanup is needed before storing
	if time.Since(ca.lastCleanup) > ca.config.CleanupInterval {
		ca.performCleanup()
	}
	
	// Check memory limits before storing
	if !ca.checkMemoryLimits() {
		return // Skip storing if memory limits exceeded
	}
	
	key := ca.getCacheKey(result)
	
	ca.mu.Lock()
	defer ca.mu.Unlock()
	
	// Check domain and server limits
	domain := result.Domain
	server := result.Server
	
	if !ca.canTrackDomain(domain) || !ca.canTrackServer(server) {
		return // Skip if limits exceeded
	}
	
	history, exists := ca.responseHistory[key]
	if !exists {
		history = &ResponseHistory{
			Domain:    result.Domain,
			QueryType: result.QueryType,
			Responses: make([]ResponseRecord, 0, 100),
		}
		ca.responseHistory[key] = history
		ca.updateTrackingCounts(domain, server, true)
	}
	
	// Add new response
	record := ResponseRecord{
		Timestamp:  result.Timestamp,
		Duration:   result.Duration,
		TTL:        result.TTL,
		Answers:    result.Answers,
		ServerName: result.Server,
		CacheHit:   result.CacheHit,
	}
	
	history.Responses = append(history.Responses, record)
	history.LastUpdated = result.Timestamp
	
	// Trim old responses (keep last 100)
	if len(history.Responses) > 100 {
		history.Responses = history.Responses[len(history.Responses)-100:]
	}
	
	// Update estimated memory usage
	ca.updateMemoryEstimate()
}

// periodicCleanup runs periodic cleanup of old data
func (ca *CacheAnalyzer) periodicCleanup() {
	ticker := time.NewTicker(ca.config.CleanupInterval)
	defer ticker.Stop()
	
	for range ticker.C {
		ca.performCleanup()
	}
}

// performCleanup removes old data and enforces limits
func (ca *CacheAnalyzer) performCleanup() {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	
	now := time.Now()
	cutoff := now.Add(-ca.config.DataRetention)
	
	// Clean response history
	for key, history := range ca.responseHistory {
		if history.LastUpdated.Before(cutoff) {
			delete(ca.responseHistory, key)
			continue
		}
		
		// Remove old responses within active histories
		validResponses := make([]ResponseRecord, 0, len(history.Responses))
		for _, resp := range history.Responses {
			if resp.Timestamp.After(cutoff) {
				validResponses = append(validResponses, resp)
			}
		}
		history.Responses = validResponses
		
		// Remove empty histories
		if len(history.Responses) == 0 {
			delete(ca.responseHistory, key)
		}
	}
	
	// Clean TTL tracker with separate mutex
	ca.ttlMu.Lock()
	for key, record := range ca.ttlTracker {
		record.mu.RLock()
		if record.LastObserved.Before(cutoff) {
			record.mu.RUnlock()
			delete(ca.ttlTracker, key)
		} else {
			record.mu.RUnlock()
		}
	}
	ca.ttlMu.Unlock()
	
	// Clean baselines
	// Note: baselines don't have timestamps, so we keep them unless memory pressure
	if ca.memoryUsage > int64(ca.config.MemoryLimitMB*1024*1024)*80/100 {
		// Under memory pressure, remove half of the baselines (oldest entries)
		if len(ca.baselines) > 10 {
			count := 0
			for server := range ca.baselines {
				delete(ca.baselines, server)
				count++
				if count >= len(ca.baselines)/2 {
					break
				}
			}
		}
	}
	
	ca.lastCleanup = now
	ca.updateMemoryEstimate()
	ca.updateTrackingCounts("", "", false) // Recalculate counts
}

// checkMemoryLimits checks if memory limits are exceeded
func (ca *CacheAnalyzer) checkMemoryLimits() bool {
	memLimitBytes := int64(ca.config.MemoryLimitMB * 1024 * 1024)
	
	// Estimate current memory usage
	ca.updateMemoryEstimate()
	
	if ca.memoryUsage > memLimitBytes {
		// Try cleanup first
		ca.performCleanup()
		
		// Check again after cleanup
		if ca.memoryUsage > memLimitBytes {
			return false // Still over limit
		}
	}
	
	return true
}

// updateMemoryEstimate estimates current memory usage
func (ca *CacheAnalyzer) updateMemoryEstimate() {
	// Rough estimation of memory usage
	// Each ResponseRecord is approximately 200 bytes
	// Each ResponseHistory overhead is approximately 100 bytes
	// Each TTLRecord is approximately 64 bytes
	// Each baseline entry is approximately 32 bytes
	
	responseCount := 0
	for _, history := range ca.responseHistory {
		responseCount += len(history.Responses)
	}
	
	ca.memoryUsage = int64(responseCount*200 + 
		len(ca.responseHistory)*100 +
		len(ca.ttlTracker)*64 +
		len(ca.baselines)*32)
}

// canTrackDomain checks if we can track a new domain
func (ca *CacheAnalyzer) canTrackDomain(domain string) bool {
	// Count unique domains
	domains := make(map[string]bool)
	for key := range ca.responseHistory {
		parts := strings.Split(key, ":")
		if len(parts) > 0 {
			domains[parts[0]] = true
		}
	}
	
	if _, exists := domains[domain]; exists {
		return true // Already tracking this domain
	}
	
	return len(domains) < ca.config.MaxTrackedDomains
}

// canTrackServer checks if we can track a new server
func (ca *CacheAnalyzer) canTrackServer(server string) bool {
	// Count unique servers
	servers := make(map[string]bool)
	for key := range ca.responseHistory {
		parts := strings.Split(key, ":")
		if len(parts) > 2 {
			servers[parts[2]] = true
		}
	}
	
	if _, exists := servers[server]; exists {
		return true // Already tracking this server
	}
	
	return len(servers) < ca.config.MaxTrackedServers
}

// updateTrackingCounts updates domain and server counts
func (ca *CacheAnalyzer) updateTrackingCounts(domain, server string, increment bool) {
	if increment {
		// Simple increment for performance (exact count done during cleanup)
		ca.domainCount++
		ca.serverCount++
	} else {
		// Recalculate exact counts
		domains := make(map[string]bool)
		servers := make(map[string]bool)
		
		for key := range ca.responseHistory {
			parts := strings.Split(key, ":")
			if len(parts) >= 3 {
				domains[parts[0]] = true
				servers[parts[2]] = true
			}
		}
		
		ca.domainCount = len(domains)
		ca.serverCount = len(servers)
	}
}

// GetMemoryStats returns current memory usage statistics
func (ca *CacheAnalyzer) GetMemoryStats() (memoryUsageMB float64, domainCount, serverCount int) {
	ca.mu.RLock()
	defer ca.mu.RUnlock()
	
	memoryUsageMB = float64(ca.memoryUsage) / (1024 * 1024)
	return memoryUsageMB, ca.domainCount, ca.serverCount
}

