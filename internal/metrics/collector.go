package metrics

import (
	"sync"
	"time"

	"github.com/randax/dns-monitoring/internal/config"
	"github.com/randax/dns-monitoring/internal/dns"
)

type Collector struct {
	mu               sync.RWMutex
	results          []dns.Result
	config           *config.MetricsConfig
	windowStart      time.Time
	lastCleanup      time.Time
	nextIndex        int  // Circular buffer index for next insertion
	
	// Track recent latency samples for Prometheus histogram/summary metrics
	// This is a separate circular buffer to efficiently provide samples
	// without iterating through all results
	latencySamples   []latencySample  // Stores latency with timestamp
	sampleIndex      int              // Circular buffer index for latency samples
	sampleMu         sync.RWMutex
	samplesFilled    bool             // Indicates if the buffer has wrapped around
	
	// Adaptive sampling for memory control
	adaptiveSampler  *AdaptiveSampler
	lastQPSUpdate    time.Time
	recentQPS        float64
}

// latencySample stores a latency measurement with its timestamp
type latencySample struct {
	LatencySeconds float64
	Timestamp      time.Time
}

func NewCollector(cfg *config.MetricsConfig) *Collector {
	if cfg == nil {
		cfg = config.DefaultMetricsConfig()
	}
	
	// Initialize adaptive sampler if configured
	var adaptiveSampler *AdaptiveSampler
	if cfg.AdaptiveSampling != nil && cfg.AdaptiveSampling.Enabled {
		adaptiveSampler = NewAdaptiveSampler(cfg.AdaptiveSampling)
	}
	
	// Calculate sample buffer size based on adaptive sampling or default
	var sampleSize int
	if adaptiveSampler != nil {
		sampleSize = adaptiveSampler.GetOptimalBufferSize()
	} else {
		// Calculate sample buffer size (10% of max results or minimum 1000)
		sampleSize = cfg.MaxStoredResults / 10
		if sampleSize < 1000 {
			sampleSize = 1000
		}
		if sampleSize > 10000 {
			sampleSize = 10000  // Cap at 10k samples to avoid excessive memory
		}
	}
	
	return &Collector{
		results:         make([]dns.Result, cfg.MaxStoredResults),
		config:          cfg,
		windowStart:     time.Now(),
		lastCleanup:     time.Now(),
		nextIndex:       0,
		latencySamples:  make([]latencySample, sampleSize),
		sampleIndex:     0,
		samplesFilled:   false,
		adaptiveSampler: adaptiveSampler,
		lastQPSUpdate:   time.Now(),
		recentQPS:       0,
	}
}

func (c *Collector) AddResult(result dns.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Check if we need to resize the buffer due to config change
	if len(c.results) != c.config.MaxStoredResults {
		c.resizeBuffer()
	}
	
	// Use circular buffer approach - overwrite oldest entry
	c.results[c.nextIndex] = result
	c.nextIndex = (c.nextIndex + 1) % c.config.MaxStoredResults
	
	// Update QPS tracking for adaptive sampling
	if c.adaptiveSampler != nil {
		c.updateQPSTracking()
	}
	
	// Apply adaptive sampling for latency samples
	shouldSample := true
	if c.adaptiveSampler != nil {
		shouldSample = c.adaptiveSampler.ShouldSample()
	}
	
	// Store latency sample for Prometheus metrics if sampled
	if shouldSample {
		c.addLatencySample(result.Duration.Seconds())
	}
	
	// Perform cleanup more frequently (every 100 results or based on time)
	if c.nextIndex%100 == 0 || time.Since(c.lastCleanup) > c.config.CalculationInterval/2 {
		c.cleanupOldDataCircular()
		
		// Adjust sampling rate based on query volume
		if c.adaptiveSampler != nil {
			c.adaptiveSampler.AdjustSamplingRate(c.recentQPS)
		}
	}
}

func (c *Collector) GetMetrics() *Metrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	if len(c.results) == 0 {
		return &Metrics{
			Period: MetricsPeriod{
				Start: c.windowStart,
				End:   time.Now(),
			},
		}
	}
	
	now := time.Now()
	windowStart := now.Add(-c.config.WindowDuration)
	
	var filteredResults []dns.Result
	for _, r := range c.results {
		// Skip empty entries in circular buffer
		if !r.Timestamp.IsZero() && r.Timestamp.After(windowStart) {
			filteredResults = append(filteredResults, r)
		}
	}
	
	if len(filteredResults) == 0 {
		return &Metrics{
			Period: MetricsPeriod{
				Start: windowStart,
				End:   now,
			},
		}
	}
	
	return c.calculateMetrics(filteredResults, windowStart, now)
}

func (c *Collector) GetMetricsForPeriod(start, end time.Time) *Metrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	var filteredResults []dns.Result
	for _, r := range c.results {
		// Skip empty entries in circular buffer
		if !r.Timestamp.IsZero() && r.Timestamp.After(start) && r.Timestamp.Before(end) {
			filteredResults = append(filteredResults, r)
		}
	}
	
	if len(filteredResults) == 0 {
		return &Metrics{
			Period: MetricsPeriod{
				Start: start,
				End:   end,
			},
		}
	}
	
	return c.calculateMetrics(filteredResults, start, end)
}

func (c *Collector) calculateMetrics(results []dns.Result, start, end time.Time) *Metrics {
	metrics := &Metrics{
		Period: MetricsPeriod{
			Start: start,
			End:   end,
		},
	}
	
	if len(results) == 0 {
		return metrics
	}
	
	metrics.Latency = calculateLatencyMetrics(results)
	metrics.Rates = calculateRateMetrics(results)
	metrics.ResponseCode = calculateResponseCodeDistribution(results)
	metrics.Throughput = calculateThroughputMetrics(results, start, end)
	metrics.QueryType = calculateQueryTypeDistribution(results)
	
	// Calculate cache metrics if any results have cache data
	if c.hasCacheData(results) {
		metrics.Cache = c.calculateCacheMetrics(results)
	}
	
	// Calculate network metrics if any results have network data
	if c.hasNetworkData(results) {
		metrics.Network = c.calculateNetworkMetrics(results)
	}
	
	// Include sampling metrics if adaptive sampling is enabled
	if c.adaptiveSampler != nil {
		samplingMetrics := c.GetSamplingMetrics()
		metrics.Sampling = samplingMetrics
	}
	
	return metrics
}

func (c *Collector) cleanupOldData() {
	cutoff := time.Now().Add(-c.config.WindowDuration * 2)
	
	newResults := make([]dns.Result, 0, len(c.results))
	for _, r := range c.results {
		if r.Timestamp.After(cutoff) {
			newResults = append(newResults, r)
		}
	}
	
	c.results = newResults
	c.lastCleanup = time.Now()
}

// cleanupOldDataCircular removes expired entries from the circular buffer
func (c *Collector) cleanupOldDataCircular() {
	cutoff := time.Now().Add(-c.config.WindowDuration * 2)
	
	// Mark expired entries with zero timestamp
	for i := range c.results {
		if !c.results[i].Timestamp.IsZero() && c.results[i].Timestamp.Before(cutoff) {
			c.results[i] = dns.Result{} // Clear expired entry
		}
	}
	
	c.lastCleanup = time.Now()
}

// resizeBuffer adjusts buffer size when config changes
func (c *Collector) resizeBuffer() {
	newSize := c.config.MaxStoredResults
	if newSize <= 0 {
		newSize = 10000 // Default fallback
	}
	
	// Create new buffer with correct size
	newBuffer := make([]dns.Result, newSize)
	
	// Copy existing valid results to new buffer
	validResults := c.getValidResults()
	copyCount := len(validResults)
	if copyCount > newSize {
		// If we have more results than new size, keep most recent
		validResults = validResults[copyCount-newSize:]
		copyCount = newSize
	}
	
	copy(newBuffer, validResults)
	c.results = newBuffer
	c.nextIndex = copyCount % newSize
}

// getValidResults returns all non-expired results from the buffer
func (c *Collector) getValidResults() []dns.Result {
	var valid []dns.Result
	for _, r := range c.results {
		if !r.Timestamp.IsZero() {
			valid = append(valid, r)
		}
	}
	return valid
}

func (c *Collector) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Clear all entries in circular buffer
	for i := range c.results {
		c.results[i] = dns.Result{}
	}
	c.nextIndex = 0
	c.windowStart = time.Now()
	c.lastCleanup = time.Now()
	
	// Clear latency samples
	c.sampleMu.Lock()
	for i := range c.latencySamples {
		c.latencySamples[i] = latencySample{}
	}
	c.sampleIndex = 0
	c.samplesFilled = false
	c.sampleMu.Unlock()
}

func (c *Collector) GetResultCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	// Count non-empty entries in circular buffer
	count := 0
	for _, r := range c.results {
		if !r.Timestamp.IsZero() {
			count++
		}
	}
	return count
}

func (c *Collector) GetConfig() *config.MetricsConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return c.config
}

func (c *Collector) UpdateConfig(cfg *config.MetricsConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	oldSize := c.config.MaxStoredResults
	c.config = cfg
	
	// Resize buffer if MaxStoredResults changed
	if oldSize != cfg.MaxStoredResults {
		c.resizeBuffer()
	}
}

// hasCacheData checks if any results contain cache analysis data
func (c *Collector) hasCacheData(results []dns.Result) bool {
	for _, r := range results {
		if r.RecursiveServer || r.CacheHit || r.CacheEfficiency > 0 {
			return true
		}
	}
	return false
}

// hasNetworkData checks if any results contain network metrics data
func (c *Collector) hasNetworkData(results []dns.Result) bool {
	for _, r := range results {
		if r.PacketLoss > 0 || r.Jitter > 0 || r.NetworkQuality > 0 || r.HopCount > 0 {
			return true
		}
	}
	return false
}

// calculateCacheMetrics computes cache performance metrics
func (c *Collector) calculateCacheMetrics(results []dns.Result) *CacheMetrics {
	metrics := &CacheMetrics{
		CacheAgeDistribution:       make(map[string]int64),
		RecursiveServerPerformance: make(map[string]float64),
	}
	
	var (
		cacheHits      int64
		totalQueries   int64
		totalTTL       time.Duration
		ttlCount       int64
		serverHits     = make(map[string]int64)
		serverQueries  = make(map[string]int64)
	)
	
	for _, r := range results {
		if !r.RecursiveServer {
			continue
		}
		
		totalQueries++
		serverQueries[r.Server]++
		
		if r.CacheHit {
			cacheHits++
			serverHits[r.Server]++
			
			// Track cache age distribution
			ageCategory := getCacheAgeCategory(r.CacheAge)
			metrics.CacheAgeDistribution[ageCategory]++
		}
		
		if r.TTL > 0 {
			totalTTL += r.TTL
			ttlCount++
		}
		
		// Track efficiency
		if r.CacheEfficiency > 0 {
			current := metrics.RecursiveServerPerformance[r.Server]
			metrics.RecursiveServerPerformance[r.Server] = (current + r.CacheEfficiency) / 2
		}
	}
	
	// Calculate rates
	if totalQueries > 0 {
		metrics.HitRate = float64(cacheHits) / float64(totalQueries)
		metrics.MissRate = 1.0 - metrics.HitRate
		metrics.TotalCacheableQueries = totalQueries
	}
	
	// Calculate average TTL
	if ttlCount > 0 {
		metrics.AverageTTL = totalTTL / time.Duration(ttlCount)
	}
	
	// Calculate per-server performance
	for server, queries := range serverQueries {
		if queries > 0 && metrics.RecursiveServerPerformance[server] == 0 {
			hits := serverHits[server]
			metrics.RecursiveServerPerformance[server] = float64(hits) / float64(queries)
		}
	}
	
	// Overall efficiency score
	if metrics.HitRate > 0 {
		metrics.Efficiency = metrics.HitRate * 0.6 // Base on hit rate
		if metrics.AverageTTL > 60*time.Second {
			metrics.Efficiency += 0.2 // Bonus for good TTL
		}
		if len(metrics.RecursiveServerPerformance) > 0 {
			var avgPerf float64
			for _, perf := range metrics.RecursiveServerPerformance {
				avgPerf += perf
			}
			avgPerf /= float64(len(metrics.RecursiveServerPerformance))
			metrics.Efficiency += avgPerf * 0.2 // Include server performance
		}
	}
	
	return metrics
}

// calculateNetworkMetrics computes network-level metrics
func (c *Collector) calculateNetworkMetrics(results []dns.Result) *NetworkMetrics {
	metrics := &NetworkMetrics{
		HopCountDistribution: make(map[int]int64),
		InterfaceStats:       make(map[string]NetworkInterfaceStats),
	}
	
	var (
		totalPacketLoss float64
		lossCount       int
		jitterValues    []time.Duration
		latencyValues   []time.Duration
		qualitySum      float64
		qualityCount    int
	)
	
	// Collect network data from results
	for _, r := range results {
		if r.PacketLoss > 0 {
			totalPacketLoss += r.PacketLoss
			lossCount++
		}
		
		if r.Jitter > 0 {
			jitterValues = append(jitterValues, r.Jitter)
		}
		
		if r.NetworkLatency > 0 {
			latencyValues = append(latencyValues, r.NetworkLatency)
		}
		
		if r.HopCount > 0 {
			metrics.HopCountDistribution[r.HopCount]++
		}
		
		if r.NetworkQuality > 0 {
			qualitySum += r.NetworkQuality
			qualityCount++
		}
	}
	
	// Calculate packet loss percentage
	if lossCount > 0 {
		metrics.PacketLossPercentage = totalPacketLoss / float64(lossCount)
	}
	
	// Calculate jitter statistics
	if len(jitterValues) > 0 {
		metrics.JitterStats = calculateLatencyMetricsFromDurations(jitterValues)
	}
	
	// Calculate network latency statistics
	if len(latencyValues) > 0 {
		metrics.NetworkLatencyStats = calculateLatencyMetricsFromDurations(latencyValues)
	}
	
	// Calculate average network quality score
	if qualityCount > 0 {
		metrics.NetworkQualityScore = qualitySum / float64(qualityCount)
	}
	
	// Note: Interface stats would be populated from actual interface monitoring
	// This is a placeholder for the structure
	metrics.InterfaceStats["default"] = NetworkInterfaceStats{
		PacketsSent:     int64(len(results)),
		PacketsReceived: int64(len(results)) - int64(float64(len(results))*metrics.PacketLossPercentage),
		PacketLoss:      metrics.PacketLossPercentage,
		AverageLatency:  metrics.NetworkLatencyStats.Mean,
		Jitter:          float64(metrics.JitterStats.Mean),
	}
	
	return metrics
}

// getCacheAgeCategory categorizes cache age for distribution
// addLatencySample adds a latency sample to the circular buffer for Prometheus metrics
func (c *Collector) addLatencySample(latencySeconds float64) {
	c.sampleMu.Lock()
	defer c.sampleMu.Unlock()
	
	if len(c.latencySamples) == 0 {
		return
	}
	
	c.latencySamples[c.sampleIndex] = latencySample{
		LatencySeconds: latencySeconds,
		Timestamp:      time.Now(),
	}
	
	// Check if we're about to wrap around
	if c.sampleIndex == len(c.latencySamples)-1 {
		c.samplesFilled = true
	}
	
	c.sampleIndex = (c.sampleIndex + 1) % len(c.latencySamples)
	
	// Clean up expired samples periodically (every 100 samples)
	if c.sampleIndex%100 == 0 {
		c.cleanupExpiredSamples()
	}
}

// GetRecentLatencySamples returns recent latency samples for Prometheus histogram/summary updates
// The samples are returned in seconds (as required by Prometheus)
// maxSamples limits the number of samples returned (0 = all available)
func (c *Collector) GetRecentLatencySamples(maxSamples int) []float64 {
	c.sampleMu.RLock()
	defer c.sampleMu.RUnlock()
	
	if len(c.latencySamples) == 0 {
		return []float64{}
	}
	
	// Apply bounds checking
	if maxSamples < 0 {
		maxSamples = 0
	}
	
	// Define reasonable upper bound to prevent excessive memory usage
	const maxAllowedSamples = 100000
	if maxSamples > maxAllowedSamples {
		maxSamples = maxAllowedSamples
	}
	
	// Get expiration cutoff (samples older than 2x window duration are expired)
	cutoff := time.Now().Add(-c.config.WindowDuration * 2)
	
	// Collect non-expired samples
	samples := []float64{}
	totalAvailable := len(c.latencySamples)
	if !c.samplesFilled {
		// Buffer hasn't wrapped yet, only check up to current index
		totalAvailable = c.sampleIndex
	}
	
	// Start from most recent and work backwards
	for i := 0; i < totalAvailable; i++ {
		// Calculate index working backwards from current position
		idx := (c.sampleIndex - 1 - i + len(c.latencySamples)) % len(c.latencySamples)
		sample := c.latencySamples[idx]
		
		// Skip expired or invalid samples
		if sample.Timestamp.IsZero() || sample.Timestamp.Before(cutoff) {
			continue
		}
		
		if sample.LatencySeconds > 0 {
			samples = append(samples, sample.LatencySeconds)
			if maxSamples > 0 && len(samples) >= maxSamples {
				break
			}
		}
	}
	
	return samples
}

// GetLatencySamplesForPeriod returns latency samples from results within a time period
// This method provides more accurate sampling by looking at actual result timestamps
func (c *Collector) GetLatencySamplesForPeriod(start, end time.Time, maxSamples int) []float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	samples := []float64{}
	for _, r := range c.results {
		// Skip empty entries and results outside the time window
		if !r.Timestamp.IsZero() && r.Timestamp.After(start) && r.Timestamp.Before(end) {
			samples = append(samples, r.Duration.Seconds())
			if maxSamples > 0 && len(samples) >= maxSamples {
				break
			}
		}
	}
	
	return samples
}

func getCacheAgeCategory(age time.Duration) string {
	switch {
	case age < 10*time.Second:
		return "<10s"
	case age < 60*time.Second:
		return "10s-1m"
	case age < 5*time.Minute:
		return "1m-5m"
	case age < 30*time.Minute:
		return "5m-30m"
	case age < 1*time.Hour:
		return "30m-1h"
	default:
		return ">1h"
	}
}

// cleanupExpiredSamples removes expired samples from the latency buffer
// This should be called with sampleMu already locked
func (c *Collector) cleanupExpiredSamples() {
	if len(c.latencySamples) == 0 {
		return
	}
	
	cutoff := time.Now().Add(-c.config.WindowDuration * 2)
	
	for i := range c.latencySamples {
		if !c.latencySamples[i].Timestamp.IsZero() && c.latencySamples[i].Timestamp.Before(cutoff) {
			c.latencySamples[i] = latencySample{} // Clear expired entry
		}
	}
}

// ResizeLatencySampleBuffer adjusts the latency sample buffer size
func (c *Collector) ResizeLatencySampleBuffer(newSize int) {
	c.sampleMu.Lock()
	defer c.sampleMu.Unlock()
	
	// Apply bounds
	if newSize < 100 {
		newSize = 100
	}
	if newSize > 100000 {
		newSize = 100000
	}
	
	if newSize == len(c.latencySamples) {
		return
	}
	
	// Create new buffer
	newBuffer := make([]latencySample, newSize)
	
	// Copy valid samples to new buffer
	cutoff := time.Now().Add(-c.config.WindowDuration * 2)
	validCount := 0
	
	for _, sample := range c.latencySamples {
		if !sample.Timestamp.IsZero() && sample.Timestamp.After(cutoff) && sample.LatencySeconds > 0 {
			if validCount < newSize {
				newBuffer[validCount] = sample
				validCount++
			}
		}
	}
	
	c.latencySamples = newBuffer
	c.sampleIndex = validCount % newSize
	c.samplesFilled = validCount >= newSize
}

// calculateLatencyMetricsFromDurations calculates latency metrics from duration slice
func calculateLatencyMetricsFromDurations(durations []time.Duration) LatencyMetrics {
	if len(durations) == 0 {
		return LatencyMetrics{}
	}
	
	// Convert durations to dns.Result format for reuse of existing function
	results := make([]dns.Result, len(durations))
	for i, d := range durations {
		results[i] = dns.Result{
			Duration: d,
		}
	}
	
	// Use existing percentile calculation
	return calculateLatencyMetrics(results)
}

// updateQPSTracking updates the queries per second tracking for adaptive sampling
func (c *Collector) updateQPSTracking() {
	now := time.Now()
	elapsed := now.Sub(c.lastQPSUpdate).Seconds()
	
	if elapsed >= 1.0 {
		// Calculate QPS over the last period
		count := 0
		cutoff := now.Add(-time.Minute)
		for _, r := range c.results {
			if !r.Timestamp.IsZero() && r.Timestamp.After(cutoff) {
				count++
			}
		}
		
		c.recentQPS = float64(count) / 60.0 // QPS over last minute
		c.lastQPSUpdate = now
	}
}

// GetSamplingMetrics returns metrics about the adaptive sampling system
func (c *Collector) GetSamplingMetrics() *SamplingMetrics {
	if c.adaptiveSampler == nil {
		return nil
	}
	
	metrics := c.adaptiveSampler.GetMetrics()
	
	// Calculate actual memory usage of samples
	c.sampleMu.RLock()
	sampleCount := 0
	if c.samplesFilled {
		sampleCount = len(c.latencySamples)
	} else {
		sampleCount = c.sampleIndex
	}
	c.sampleMu.RUnlock()
	
	// Update memory usage in sampler
	estimatedMemory := c.adaptiveSampler.EstimateSampleMemory(sampleCount)
	c.adaptiveSampler.UpdateMemoryUsage(estimatedMemory)
	
	return &metrics
}

// SetAdaptiveSamplingConfig updates the adaptive sampling configuration
func (c *Collector) SetAdaptiveSamplingConfig(cfg *config.AdaptiveSamplingConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if cfg == nil {
		c.adaptiveSampler = nil
		return
	}
	
	if c.adaptiveSampler == nil {
		c.adaptiveSampler = NewAdaptiveSampler(cfg)
	} else {
		// Update existing sampler configuration
		c.adaptiveSampler.SetMemoryLimit(cfg.MemoryLimitMB)
	}
	
	// Resize sample buffer based on new optimal size
	newSize := c.adaptiveSampler.GetOptimalBufferSize()
	c.ResizeLatencySampleBuffer(newSize)
}