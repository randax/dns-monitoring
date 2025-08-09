package metrics

import (
	"time"
)

type Metrics struct {
	Latency      LatencyMetrics
	Rates        RateMetrics
	ResponseCode ResponseCodeDistribution
	Throughput   ThroughputMetrics
	QueryType    QueryTypeDistribution
	Period       MetricsPeriod
	Cache        *CacheMetrics
	Network      *NetworkMetrics
	Sampling     *SamplingMetrics
}

type LatencyMetrics struct {
	P50  float64
	P95  float64
	P99  float64
	P999 float64
	Min  float64
	Max  float64
	Mean float64
}

type RateMetrics struct {
	SuccessRate    float64
	ErrorRate      float64
	TotalQueries   int64
	SuccessQueries int64
	ErrorQueries   int64
	NetworkErrors  int64
}

type ResponseCodeDistribution struct {
	NoError   int64
	NXDomain  int64
	ServFail  int64
	FormErr   int64
	NotImpl   int64
	Refused   int64
	Other     int64
	Total     int64
}

type ThroughputMetrics struct {
	CurrentQPS  float64
	AverageQPS  float64
	PeakQPS     float64
	TotalQueries int64
	Duration    time.Duration
	ByProtocol  map[string]float64
	ByServer    map[string]float64
}

type QueryTypeDistribution struct {
	A       int64
	AAAA    int64
	CNAME   int64
	MX      int64
	TXT     int64
	NS      int64
	SOA     int64
	PTR     int64
	Other   int64
	Total   int64
}

type MetricsPeriod struct {
	Start time.Time
	End   time.Time
}

type CacheMetrics struct {
	HitRate                    float64            `json:"hit_rate"`
	MissRate                   float64            `json:"miss_rate"`
	Efficiency                 float64            `json:"efficiency"`
	AverageTTL                 time.Duration      `json:"average_ttl"`
	CacheAgeDistribution       map[string]int64   `json:"cache_age_distribution"`
	RecursiveServerPerformance map[string]float64 `json:"recursive_server_performance"`
	TotalCacheableQueries      int64              `json:"total_cacheable_queries"`
	
	// Additional fields for CLI formatter
	AvgTTL  float64 `json:"avg_ttl"`  // Average TTL in seconds
	MinTTL  float64 `json:"min_ttl"`  // Minimum TTL in seconds
	MaxTTL  float64 `json:"max_ttl"`  // Maximum TTL in seconds
	Hits    int64   `json:"hits"`     // Total cache hits
	Misses  int64   `json:"misses"`   // Total cache misses
}

type NetworkMetrics struct {
	PacketLossPercentage  float64                       `json:"packet_loss_percentage"`
	JitterStats           LatencyMetrics                `json:"jitter_stats"`
	NetworkLatencyStats   LatencyMetrics                `json:"network_latency_stats"`
	HopCountDistribution  map[int]int64                 `json:"hop_count_distribution"`
	NetworkQualityScore   float64                       `json:"network_quality_score"`
	InterfaceStats        map[string]NetworkInterfaceStats `json:"interface_stats"`
	
	// Additional fields for CLI formatter
	PacketLoss       float64 `json:"packet_loss"`        // Packet loss as decimal (0.01 = 1%)
	QualityScore     float64 `json:"quality_score"`      // Quality score 0-100
	AvgJitter        float64 `json:"avg_jitter"`         // Average jitter in milliseconds
	LatencyP50       float64 `json:"latency_p50"`        // P50 network latency in milliseconds
	LatencyP95       float64 `json:"latency_p95"`        // P95 network latency in milliseconds
	LatencyP99       float64 `json:"latency_p99"`        // P99 network latency in milliseconds
	AvgHopCount      float64 `json:"avg_hop_count"`      // Average hop count
	PacketsSent      int64   `json:"packets_sent"`       // Total packets sent
	PacketsReceived  int64   `json:"packets_received"`   // Total packets received
}

type NetworkInterfaceStats struct {
	PacketsSent     int64   `json:"packets_sent"`
	PacketsReceived int64   `json:"packets_received"`
	PacketLoss      float64 `json:"packet_loss"`
	AverageLatency  float64 `json:"average_latency"`
	Jitter          float64 `json:"jitter"`
}

type SamplingMetrics struct {
	CurrentRate      float64   `json:"current_rate"`
	EffectiveRate    float64   `json:"effective_rate"`
	QueriesPerSecond float64   `json:"queries_per_second"`
	SamplesPerSecond float64   `json:"samples_per_second"`
	MemoryUsedBytes  uint64    `json:"memory_used_bytes"`
	MemoryLimitBytes uint64    `json:"memory_limit_bytes"`
	DroppedSamples   uint64    `json:"dropped_samples"`
	AdjustmentCount  uint64    `json:"adjustment_count"`
	LastAdjustment   time.Time `json:"last_adjustment"`
}