package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"gopkg.in/yaml.v3"
)

var (
	ErrFileNotFound = errors.New("config file not found")
	ErrParseFailed  = errors.New("failed to parse config file")
	ErrInvalidConfig = errors.New("invalid configuration")
)

type Config struct {
	DNS     DNSConfig       `yaml:"dns"`
	Monitor MonitorConfig   `yaml:"monitor"`
	Output  OutputConfig    `yaml:"output"`
	Metrics *MetricsConfig  `yaml:"metrics"`
	Passive PassiveConfig   `yaml:"passive"`
	Cache   CacheConfig     `yaml:"cache"`
	Network NetworkConfig   `yaml:"network"`
}

type MetricsConfig struct {
	Enabled              bool
	WindowDuration       time.Duration
	MaxStoredResults     int
	CalculationInterval  time.Duration
	EnabledMetrics       []string
	PercentilePrecision  int
	RateWindowSizes      []time.Duration
	ThroughputSmoothing  bool
	Export               ExportConfig
	AdaptiveSampling     *AdaptiveSamplingConfig `yaml:"adaptive_sampling"`
}

type AdaptiveSamplingConfig struct {
	Enabled                 bool          `yaml:"enabled"`
	MinSampleRate           float64       `yaml:"min_sample_rate"`
	MaxSampleRate           float64       `yaml:"max_sample_rate"`
	TargetSampleRate        float64       `yaml:"target_sample_rate"`
	MemoryLimitMB           float64       `yaml:"memory_limit_mb"`
	AdjustInterval          time.Duration `yaml:"adjust_interval"`
	WindowDuration          time.Duration `yaml:"window_duration"`
	HighVolumeQPS           float64       `yaml:"high_volume_qps"`
	LowVolumeQPS            float64       `yaml:"low_volume_qps"`
	MemoryPressureThreshold float64       `yaml:"memory_pressure_threshold"`
}

type ExportConfig struct {
	Prometheus PrometheusConfig `yaml:"prometheus"`
	Zabbix     ZabbixConfig     `yaml:"zabbix"`
}

type PrometheusConfig struct {
	Enabled               bool          `yaml:"enabled"`
	Port                  int           `yaml:"port"`
	Path                  string        `yaml:"path"`
	UpdateInterval        time.Duration `yaml:"update_interval"`
	IncludeServerLabels   bool          `yaml:"include_server_labels"`
	IncludeProtocolLabels bool          `yaml:"include_protocol_labels"`
	MetricPrefix          string        `yaml:"metric_prefix"`
	// EnableLatencySampling controls whether to populate histogram/summary metrics
	// When false (default), only percentile gauges are updated for lower overhead
	// When true, individual latency samples are tracked for histogram/summary metrics
	EnableLatencySampling bool          `yaml:"enable_latency_sampling"`
	// DetailedValidation enables verbose configuration warnings (default: false)
	DetailedValidation    bool          `yaml:"detailed_validation"`
	// CounterStateCleanup configures periodic cleanup of counter state maps
	CounterStateCleanupInterval time.Duration `yaml:"counter_state_cleanup_interval"`
	// CounterStateMaxAge determines how long to keep counter state entries before cleanup
	CounterStateMaxAge          time.Duration `yaml:"counter_state_max_age"`
	
	// Advanced metric features (default: all false for minimal overhead)
	// Set individual features to true to enable specific metric groups
	MetricFeatures MetricFeaturesConfig `yaml:"metric_features"`
	
	// Security configuration
	Security PrometheusSecurityConfig `yaml:"security"`
}

// MetricFeaturesConfig controls which advanced metric groups are enabled
type MetricFeaturesConfig struct {
	// EnableCacheMetrics enables cache hit rate, efficiency, and TTL metrics
	EnableCacheMetrics    bool `yaml:"enable_cache_metrics"`
	// EnableNetworkMetrics enables packet loss, network quality, and interface metrics
	EnableNetworkMetrics  bool `yaml:"enable_network_metrics"`
	// EnableJitterMetrics enables jitter percentile tracking
	EnableJitterMetrics   bool `yaml:"enable_jitter_metrics"`
	// EnableTTLMetrics enables TTL min/max/average tracking
	EnableTTLMetrics      bool `yaml:"enable_ttl_metrics"`
	// EnableMonitoringMetrics enables monitor health, CPU, memory metrics
	EnableMonitoringMetrics bool `yaml:"enable_monitoring_metrics"`
}

type PrometheusSecurityConfig struct {
	// Basic authentication
	BasicAuth BasicAuthConfig `yaml:"basic_auth"`
	
	// TLS configuration
	TLS TLSConfig `yaml:"tls"`
	
	// Rate limiting configuration
	RateLimit RateLimitConfig `yaml:"rate_limit"`
	
	// Request logging
	RequestLogging RequestLoggingConfig `yaml:"request_logging"`
}

type BasicAuthConfig struct {
	Enabled  bool              `yaml:"enabled"`
	Users    map[string]string `yaml:"users"` // username -> password (should be hashed in production)
	Realm    string            `yaml:"realm"`
}

type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
	// Optional: client certificate authentication
	ClientAuth         bool     `yaml:"client_auth"`
	ClientCACertFiles  []string `yaml:"client_ca_cert_files"`
}

type RateLimitConfig struct {
	Enabled         bool          `yaml:"enabled"`
	RequestsPerMin  int           `yaml:"requests_per_min"`
	BurstSize       int           `yaml:"burst_size"`
	// Per-IP rate limiting
	PerIP           bool          `yaml:"per_ip"`
	// Whitelist IPs that bypass rate limiting
	WhitelistIPs    []string      `yaml:"whitelist_ips"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
}

type RequestLoggingConfig struct {
	Enabled        bool     `yaml:"enabled"`
	LogLevel       string   `yaml:"log_level"` // debug, info, warn, error
	LogHeaders     bool     `yaml:"log_headers"`
	LogBody        bool     `yaml:"log_body"`
	ExcludePaths   []string `yaml:"exclude_paths"`
	// Log slow requests (useful for debugging scraping issues)
	SlowRequestThreshold time.Duration `yaml:"slow_request_threshold"`
}

type ZabbixConfig struct {
	Enabled       bool          `yaml:"enabled"`
	Server        string        `yaml:"server"`
	Port          int           `yaml:"port"`
	HostName      string        `yaml:"hostname"`
	SendInterval  time.Duration `yaml:"send_interval"`
	BatchSize     int           `yaml:"batch_size"`
	Timeout       time.Duration `yaml:"timeout"`
	RetryAttempts int           `yaml:"retry_attempts"`
	RetryDelay    time.Duration `yaml:"retry_delay"`
	ItemPrefix    string        `yaml:"item_prefix"`
}

type DNSConfig struct {
	Servers []DNSServer `yaml:"servers"`
	Queries QueryConfig `yaml:"queries"`
}

type DNSServer struct {
	Name               string `yaml:"name"`
	Address            string `yaml:"address"`
	Port               int    `yaml:"port"`
	Enabled            bool   `yaml:"enabled"`
	Protocol           string `yaml:"protocol,omitempty"`
	DoHEndpoint        string `yaml:"doh_endpoint,omitempty"`
	DoHMethod          string `yaml:"doh_method,omitempty"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify,omitempty"`
}

type QueryConfig struct {
	Types   []string      `yaml:"types"`
	Domains []string      `yaml:"domains"`
	Timeout time.Duration `yaml:"timeout"`
	Retries int           `yaml:"retries"`
}

type MonitorConfig struct {
	Interval       time.Duration `yaml:"interval"`
	MaxConcurrent  int           `yaml:"max_concurrent"`
	AlertThreshold int           `yaml:"alert_threshold"`
}

type OutputConfig struct {
	Format     string       `yaml:"format"`
	File       string       `yaml:"file"`
	Console    bool         `yaml:"console"`
	Metrics    MetricConfig `yaml:"metrics"`
	BufferSize int          `yaml:"buffer_size"`
	CLI        CLIConfig    `yaml:"cli"`
}

type MetricConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint"`
	Interval int    `yaml:"interval"`
}

type CLIConfig struct {
	ShowColors        bool          `yaml:"show_colors"`
	DetailedView      bool          `yaml:"detailed_view"`
	RefreshInterval   time.Duration `yaml:"refresh_interval"`
	CompactMode       bool          `yaml:"compact_mode"`
	ShowDistributions bool          `yaml:"show_distributions"`
}

type PassiveConfig struct {
	Enabled      bool                 `yaml:"enabled"`
	Interfaces   []string             `yaml:"interfaces"`
	BPF          string               `yaml:"bpf"`
	SnapLen      int                  `yaml:"snaplen"`
	Workers      int                  `yaml:"workers"`
	MatchTimeout time.Duration        `yaml:"match_timeout"`
	BufferSize   int                  `yaml:"buffer_size"`
	TCPReassembly *TCPReassemblyConfig `yaml:"tcp_reassembly"`
}

type TCPReassemblyConfig struct {
	Enabled         bool          `yaml:"enabled"`
	MaxStreams      int           `yaml:"max_streams"`
	MaxBufferSize   int           `yaml:"max_buffer_size"`
	MaxTotalMemory  int64         `yaml:"max_total_memory"`
	FlushInterval   time.Duration `yaml:"flush_interval"`
	StreamTimeout   time.Duration `yaml:"stream_timeout"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
}

type CacheConfig struct {
	Enabled                bool          `yaml:"enabled"`
	AnalysisInterval       time.Duration `yaml:"analysis_interval"`
	CacheHitThreshold      time.Duration `yaml:"cache_hit_threshold"`
	RecursiveServers       []string      `yaml:"recursive_servers"`
	TTLTracking            bool          `yaml:"ttl_tracking"`
	CacheEfficiencyMetrics bool          `yaml:"cache_efficiency_metrics"`
	DetectionMethods       []string      `yaml:"detection_methods"`
	MaxTrackedDomains      int           `yaml:"max_tracked_domains"`
	MaxTrackedServers      int           `yaml:"max_tracked_servers"`
	MemoryLimitMB          float64       `yaml:"memory_limit_mb"`
	CleanupInterval        time.Duration `yaml:"cleanup_interval"`
	DataRetention          time.Duration `yaml:"data_retention"`
}

type NetworkConfig struct {
	Enabled              bool          `yaml:"enabled"`
	PacketLossDetection  bool          `yaml:"packet_loss_detection"`
	JitterCalculation    bool          `yaml:"jitter_calculation"`
	PingIntegration      bool          `yaml:"ping_integration"`
	NetworkTimeouts      time.Duration `yaml:"network_timeouts"`
	PacketLossThreshold  float64       `yaml:"packet_loss_threshold"`
	JitterThreshold      time.Duration `yaml:"jitter_threshold"`
	SampleSize           int           `yaml:"sample_size"`
	NetworkInterfaces    []string      `yaml:"network_interfaces"`
	HopCountEstimation   bool          `yaml:"hop_count_estimation"`
	UseTraceroute        bool          `yaml:"use_traceroute"`
}

func Load(path string) (*Config, error) {
	cfg := defaultConfig()

	if path == "" {
		path = "config.yaml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, fmt.Errorf("%w: %s", ErrFileNotFound, path)
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrParseFailed, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}

	return cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		DNS: DNSConfig{
			Servers: []DNSServer{
				{
					Name:     "Google DNS",
					Address:  "8.8.8.8",
					Port:     53,
					Enabled:  true,
					Protocol: "udp",
				},
				{
					Name:     "Cloudflare DNS",
					Address:  "1.1.1.1",
					Port:     53,
					Enabled:  true,
					Protocol: "udp",
				},
			},
			Queries: QueryConfig{
				Types:   []string{"A", "AAAA"},
				Domains: []string{"example.com"},
				Timeout: 5 * time.Second,
				Retries: 3,
			},
		},
		Monitor: MonitorConfig{
			Interval:       30 * time.Second,
			MaxConcurrent:  10,
			AlertThreshold: 3,
		},
		Output: OutputConfig{
			Format:     "json",
			Console:    true,
			BufferSize: 1000,
			Metrics: MetricConfig{
				Enabled:  false,
				Interval: 60,
			},
			CLI: CLIConfig{
				ShowColors:        true,
				DetailedView:      false,
				RefreshInterval:   10 * time.Second,
				CompactMode:       false,
				ShowDistributions: true,
			},
		},
		Metrics: DefaultMetricsConfig(),
		Passive: PassiveConfig{
			Enabled:      false,
			Interfaces:   []string{},
			BPF:          "port 53",
			SnapLen:      262144,
			Workers:      4,
			MatchTimeout: 5 * time.Second,
			BufferSize:   10000,
			TCPReassembly: &TCPReassemblyConfig{
				Enabled:         false, // Disabled by default
				MaxStreams:      1000,
				MaxBufferSize:   65536, // 64KB per stream
				MaxTotalMemory:  100 * 1024 * 1024, // 100MB total
				FlushInterval:   5 * time.Second,
				StreamTimeout:   30 * time.Second,
				CleanupInterval: 10 * time.Second,
			},
		},
		Cache: CacheConfig{
			Enabled:                false,
			AnalysisInterval:       30 * time.Second,
			CacheHitThreshold:      10 * time.Millisecond,
			RecursiveServers:       []string{},
			TTLTracking:            true,
			CacheEfficiencyMetrics: true,
			DetectionMethods:       []string{"timing", "ttl", "pattern"},
			MaxTrackedDomains:      1000,
			MaxTrackedServers:      100,
			MemoryLimitMB:          50,
			CleanupInterval:        5 * time.Minute,
			DataRetention:          1 * time.Hour,
		},
		Network: NetworkConfig{
			Enabled:              false,
			PacketLossDetection:  false,
			JitterCalculation:    false,
			PingIntegration:      false,
			NetworkTimeouts:      10 * time.Second,
			PacketLossThreshold:  0.05,
			JitterThreshold:      50 * time.Millisecond,
			SampleSize:           100,
			NetworkInterfaces:    []string{},
			HopCountEstimation:   false,
			UseTraceroute:        false,
		},
	}
}

func (c *Config) validate() error {
	if len(c.DNS.Servers) == 0 {
		return fmt.Errorf("at least one DNS server must be configured")
	}

	for i := range c.DNS.Servers {
		server := &c.DNS.Servers[i]
		if server.Address == "" {
			return fmt.Errorf("DNS server %d: address is required", i)
		}
		if server.Port <= 0 || server.Port > 65535 {
			return fmt.Errorf("DNS server %d: invalid port %d", i, server.Port)
		}
		if server.Protocol != "" {
			switch server.Protocol {
			case "udp", "tcp", "dot", "doh":
			default:
				return fmt.Errorf("DNS server %d: invalid protocol %s (must be udp, tcp, dot, or doh)", i, server.Protocol)
			}
			
			if server.Protocol == "doh" {
				if server.DoHEndpoint == "" {
					server.DoHEndpoint = "/dns-query"
				}
				
				if err := validateDoHEndpoint(server.DoHEndpoint); err != nil {
					return fmt.Errorf("DNS server %d: %w", i, err)
				}
				
				// DoH addresses without scheme will have https:// added in protocols.go
				// No need to require explicit scheme here
				
				if err := validateDoHURL(server.Address, server.DoHEndpoint); err != nil {
					return fmt.Errorf("DNS server %d: %w", i, err)
				}
				
				// Validate and normalize DoH method
				if server.DoHMethod == "" {
					server.DoHMethod = "POST"
				} else {
					// Normalize to uppercase for consistency
					normalized := strings.ToUpper(server.DoHMethod)
					if normalized != "GET" && normalized != "POST" {
						return fmt.Errorf("DNS server %d: invalid DoH method %s (must be GET or POST)", i, server.DoHMethod)
					}
					server.DoHMethod = normalized
				}
			} else if server.Protocol != "" {
				// For non-DoH protocols, validate the address is a valid hostname or IP
				if err := validateHostnameOrIP(server.Address); err != nil {
					return fmt.Errorf("DNS server %d: invalid address for %s protocol: %w", i, server.Protocol, err)
				}
			}
		}
	}

	if len(c.DNS.Queries.Types) == 0 {
		return fmt.Errorf("at least one query type must be configured")
	}

	if len(c.DNS.Queries.Domains) == 0 {
		return fmt.Errorf("at least one domain must be configured")
	}

	if c.DNS.Queries.Timeout <= 0 {
		c.DNS.Queries.Timeout = 5 * time.Second
	}

	if c.DNS.Queries.Retries < 0 {
		c.DNS.Queries.Retries = 3
	}

	if c.Monitor.Interval <= 0 {
		c.Monitor.Interval = 30 * time.Second
	}

	if c.Monitor.MaxConcurrent <= 0 {
		c.Monitor.MaxConcurrent = 10
	}

	if c.Output.Format != "json" && c.Output.Format != "text" && c.Output.Format != "csv" {
		c.Output.Format = "json"
	}

	if c.Metrics == nil {
		c.Metrics = DefaultMetricsConfig()
	}

	if c.Output.CLI.RefreshInterval <= 0 {
		c.Output.CLI.RefreshInterval = 10 * time.Second
	}

	if c.Output.CLI.RefreshInterval < time.Second {
		return fmt.Errorf("CLI refresh interval must be at least 1 second")
	}

	// Validate Zabbix configuration if enabled
	if c.Metrics != nil && c.Metrics.Export.Zabbix.Enabled {
		zabbix := &c.Metrics.Export.Zabbix
		if zabbix.Server == "" {
			return fmt.Errorf("Zabbix server address is required when Zabbix export is enabled")
		}
		if zabbix.Port <= 0 || zabbix.Port > 65535 {
			return fmt.Errorf("Zabbix port must be between 1 and 65535, got %d", zabbix.Port)
		}
		if zabbix.HostName == "" {
			return fmt.Errorf("Zabbix hostname is required when Zabbix export is enabled")
		}
		if zabbix.SendInterval <= 0 {
			zabbix.SendInterval = 60 * time.Second
		}
		if zabbix.BatchSize <= 0 {
			zabbix.BatchSize = 100
		}
		if zabbix.Timeout <= 0 {
			zabbix.Timeout = 10 * time.Second
		}
		if zabbix.RetryAttempts < 0 {
			zabbix.RetryAttempts = 3
		}
		if zabbix.RetryDelay <= 0 {
			zabbix.RetryDelay = 5 * time.Second
		}
		if zabbix.ItemPrefix == "" {
			zabbix.ItemPrefix = "dns"
		}
	}

	// Validate passive configuration
	if c.Passive.Enabled {
		if c.Passive.Workers <= 0 {
			return fmt.Errorf("passive workers must be positive, got %d", c.Passive.Workers)
		}
		if c.Passive.MatchTimeout <= 0 {
			c.Passive.MatchTimeout = 5 * time.Second
		}
		if c.Passive.SnapLen <= 0 {
			c.Passive.SnapLen = 262144
		}
		if c.Passive.BufferSize <= 0 {
			c.Passive.BufferSize = 10000
		}
		if c.Passive.BPF == "" {
			c.Passive.BPF = "port 53"
		}
		
		// Validate BPF filter syntax
		if err := validateBPFFilter(c.Passive.BPF); err != nil {
			return fmt.Errorf("invalid BPF filter syntax: %w", err)
		}
	}

	// Validate cache configuration
	if c.Cache.Enabled {
		if c.Cache.AnalysisInterval <= 0 {
			c.Cache.AnalysisInterval = 30 * time.Second
		}
		if c.Cache.AnalysisInterval < 5*time.Second {
			return fmt.Errorf("cache analysis interval must be at least 5 seconds")
		}
		if c.Cache.CacheHitThreshold <= 0 {
			c.Cache.CacheHitThreshold = 10 * time.Millisecond
		}
		// Validate memory and tracking limits
		if c.Cache.MaxTrackedDomains <= 0 {
			c.Cache.MaxTrackedDomains = 1000
		}
		if c.Cache.MaxTrackedDomains > 100000 {
			return fmt.Errorf("cache max tracked domains too large (maximum 100000)")
		}
		if c.Cache.MaxTrackedServers <= 0 {
			c.Cache.MaxTrackedServers = 100
		}
		if c.Cache.MaxTrackedServers > 1000 {
			return fmt.Errorf("cache max tracked servers too large (maximum 1000)")
		}
		if c.Cache.MemoryLimitMB <= 0 {
			c.Cache.MemoryLimitMB = 50
		}
		if c.Cache.MemoryLimitMB > 1000 {
			return fmt.Errorf("cache memory limit too large (maximum 1000MB)")
		}
		if c.Cache.CleanupInterval <= 0 {
			c.Cache.CleanupInterval = 5 * time.Minute
		}
		if c.Cache.CleanupInterval < time.Minute {
			return fmt.Errorf("cache cleanup interval must be at least 1 minute")
		}
		if c.Cache.DataRetention <= 0 {
			c.Cache.DataRetention = 1 * time.Hour
		}
		if c.Cache.DataRetention > 24*time.Hour {
			fmt.Fprintf(os.Stderr, "Warning: Cache data retention of %v may consume significant memory\n", c.Cache.DataRetention)
		}
		// Validate recursive servers exist in main server list
		if len(c.Cache.RecursiveServers) > 0 {
			serverMap := make(map[string]bool)
			for _, server := range c.DNS.Servers {
				serverMap[server.Name] = true
			}
			for _, recursiveServer := range c.Cache.RecursiveServers {
				if !serverMap[recursiveServer] {
					return fmt.Errorf("cache recursive server '%s' not found in DNS servers list", recursiveServer)
				}
			}
		}
		// Validate detection methods
		validMethods := map[string]bool{"timing": true, "ttl": true, "pattern": true}
		for _, method := range c.Cache.DetectionMethods {
			if !validMethods[method] {
				return fmt.Errorf("invalid cache detection method '%s', must be one of: timing, ttl, pattern", method)
			}
		}
		if len(c.Cache.DetectionMethods) == 0 {
			c.Cache.DetectionMethods = []string{"timing", "ttl", "pattern"}
		}
	}

	// Validate network configuration
	if c.Network.Enabled {
		if c.Network.NetworkTimeouts <= 0 {
			c.Network.NetworkTimeouts = 10 * time.Second
		}
		if c.Network.PacketLossThreshold < 0 || c.Network.PacketLossThreshold > 1 {
			return fmt.Errorf("packet loss threshold must be between 0 and 1, got %f", c.Network.PacketLossThreshold)
		}
		if c.Network.PacketLossThreshold == 0 {
			c.Network.PacketLossThreshold = 0.05
		}
		if c.Network.JitterThreshold <= 0 {
			c.Network.JitterThreshold = 50 * time.Millisecond
		}
		if c.Network.SampleSize <= 0 {
			c.Network.SampleSize = 100
		}
		if c.Network.SampleSize < 10 {
			return fmt.Errorf("network sample size must be at least 10 for statistical validity")
		}
		if c.Network.SampleSize > 10000 {
			return fmt.Errorf("network sample size too large (maximum 10000)")
		}
	}

	// Validate cross-configuration dependencies
	if c.Cache.Enabled && len(c.Cache.RecursiveServers) == 0 {
		// If no specific recursive servers specified, warn that all servers will be analyzed
		// This is allowed but may not be desired
	}

	// Validate adaptive sampling configuration if present
	if c.Metrics != nil && c.Metrics.AdaptiveSampling != nil {
		as := c.Metrics.AdaptiveSampling
		
		if as.Enabled {
			// Validate sample rates
			if as.MinSampleRate < 0 || as.MinSampleRate > 1 {
				return fmt.Errorf("adaptive sampling min_sample_rate must be between 0 and 1, got %f", as.MinSampleRate)
			}
			if as.MaxSampleRate < 0 || as.MaxSampleRate > 1 {
				return fmt.Errorf("adaptive sampling max_sample_rate must be between 0 and 1, got %f", as.MaxSampleRate)
			}
			if as.TargetSampleRate < 0 || as.TargetSampleRate > 1 {
				return fmt.Errorf("adaptive sampling target_sample_rate must be between 0 and 1, got %f", as.TargetSampleRate)
			}
			
			// Ensure logical ordering
			if as.MinSampleRate > as.MaxSampleRate {
				return fmt.Errorf("adaptive sampling min_sample_rate (%f) cannot be greater than max_sample_rate (%f)", 
					as.MinSampleRate, as.MaxSampleRate)
			}
			if as.TargetSampleRate < as.MinSampleRate || as.TargetSampleRate > as.MaxSampleRate {
				return fmt.Errorf("adaptive sampling target_sample_rate (%f) must be between min (%f) and max (%f)", 
					as.TargetSampleRate, as.MinSampleRate, as.MaxSampleRate)
			}
			
			// Validate memory limit
			if as.MemoryLimitMB <= 0 {
				as.MemoryLimitMB = 100 // Default 100MB
			}
			if as.MemoryLimitMB < 1 {
				return fmt.Errorf("adaptive sampling memory_limit_mb must be at least 1MB, got %f", as.MemoryLimitMB)
			}
			if as.MemoryLimitMB > 10000 {
				return fmt.Errorf("adaptive sampling memory_limit_mb too large (max 10GB), got %f", as.MemoryLimitMB)
			}
			
			// Validate intervals
			if as.AdjustInterval <= 0 {
				as.AdjustInterval = 30 * time.Second
			}
			if as.AdjustInterval < 5*time.Second {
				return fmt.Errorf("adaptive sampling adjust_interval must be at least 5s for stability, got %v", as.AdjustInterval)
			}
			
			if as.WindowDuration <= 0 {
				as.WindowDuration = 5 * time.Minute
			}
			if as.WindowDuration < time.Minute {
				return fmt.Errorf("adaptive sampling window_duration must be at least 1m, got %v", as.WindowDuration)
			}
			
			// Validate QPS thresholds
			if as.HighVolumeQPS <= 0 {
				as.HighVolumeQPS = 1000
			}
			if as.LowVolumeQPS <= 0 {
				as.LowVolumeQPS = 10
			}
			if as.LowVolumeQPS >= as.HighVolumeQPS {
				return fmt.Errorf("adaptive sampling low_volume_qps (%f) must be less than high_volume_qps (%f)", 
					as.LowVolumeQPS, as.HighVolumeQPS)
			}
			
			// Validate memory pressure threshold
			if as.MemoryPressureThreshold <= 0 {
				as.MemoryPressureThreshold = 0.8
			}
			if as.MemoryPressureThreshold < 0.5 || as.MemoryPressureThreshold > 0.95 {
				return fmt.Errorf("adaptive sampling memory_pressure_threshold should be between 0.5 and 0.95, got %f", 
					as.MemoryPressureThreshold)
			}
			
			// Warn about performance implications
			if as.MemoryLimitMB < 10 {
				fmt.Fprintf(os.Stderr, "Warning: Adaptive sampling memory limit of %.1fMB is very low and may impact sampling accuracy\n", as.MemoryLimitMB)
			}
			if as.AdjustInterval < 10*time.Second {
				fmt.Fprintf(os.Stderr, "Warning: Adaptive sampling adjust interval of %v may cause frequent rate changes\n", as.AdjustInterval)
			}
		}
	}
	
	// Validate Prometheus configuration
	if c.Metrics != nil && c.Metrics.Export.Prometheus.Enabled {
		prom := &c.Metrics.Export.Prometheus
		
		// Critical validation only - port and path
		if prom.Port <= 0 || prom.Port > 65535 {
			return fmt.Errorf("Prometheus port must be between 1 and 65535, got %d", prom.Port)
		}
		
		if !strings.HasPrefix(prom.Path, "/") {
			return fmt.Errorf("Prometheus path must start with '/', got '%s'", prom.Path)
		}
		
		// Minimum interval check - only error on extreme values
		if prom.UpdateInterval < 100*time.Millisecond {
			return fmt.Errorf("Prometheus update interval too low (minimum 100ms), got %v", prom.UpdateInterval)
		}
		
		// Basic metric prefix validation - only check format, no warnings
		if prom.MetricPrefix != "" {
			validPrefixRegex := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_:]*$`)
			if !validPrefixRegex.MatchString(prom.MetricPrefix) {
				return fmt.Errorf("Invalid Prometheus metric prefix format: '%s'", prom.MetricPrefix)
			}
		}
		
		// Always show memory usage warning when latency sampling is enabled
		if prom.EnableLatencySampling {
			fmt.Fprintf(os.Stderr, "Warning: EnableLatencySampling is enabled. This will store individual latency samples in memory for histogram/summary metrics.\n")
			fmt.Fprintf(os.Stderr, "         Memory usage will scale with query volume and update interval (current: %v).\n", prom.UpdateInterval)
			fmt.Fprintf(os.Stderr, "         Consider disabling this option if you only need percentile gauges or experience high memory usage.\n")
		}
		
		// Only show detailed warnings if explicitly enabled
		if prom.DetailedValidation {
			// Performance warnings
			if prom.UpdateInterval < 1*time.Second {
				fmt.Fprintf(os.Stderr, "Warning: Sub-second update interval (%v) may impact performance\n", prom.UpdateInterval)
			}
			
			// Metric prefix warnings
			if prom.MetricPrefix != "" && strings.Contains(prom.MetricPrefix, ":") {
				fmt.Fprintf(os.Stderr, "Info: Metric prefix contains colons\n")
			}
		}
		
		// Validate security configuration
		if prom.Security.BasicAuth.Enabled {
			if len(prom.Security.BasicAuth.Users) == 0 {
				return fmt.Errorf("Basic auth enabled but no users configured")
			}
			if prom.Security.BasicAuth.Realm == "" {
				prom.Security.BasicAuth.Realm = "Prometheus Metrics"
			}
		}
		
		if prom.Security.TLS.Enabled {
			if prom.Security.TLS.CertFile == "" || prom.Security.TLS.KeyFile == "" {
				return fmt.Errorf("TLS enabled but cert_file or key_file not specified")
			}
			// Check if files exist
			if _, err := os.Stat(prom.Security.TLS.CertFile); err != nil {
				return fmt.Errorf("TLS cert file not found: %s", prom.Security.TLS.CertFile)
			}
			if _, err := os.Stat(prom.Security.TLS.KeyFile); err != nil {
				return fmt.Errorf("TLS key file not found: %s", prom.Security.TLS.KeyFile)
			}
			
			if prom.Security.TLS.ClientAuth {
				if len(prom.Security.TLS.ClientCACertFiles) == 0 {
					return fmt.Errorf("Client auth enabled but no client CA cert files specified")
				}
				for _, caFile := range prom.Security.TLS.ClientCACertFiles {
					if _, err := os.Stat(caFile); err != nil {
						return fmt.Errorf("Client CA cert file not found: %s", caFile)
					}
				}
			}
		}
		
		if prom.Security.RateLimit.Enabled {
			if prom.Security.RateLimit.RequestsPerMin <= 0 {
				prom.Security.RateLimit.RequestsPerMin = 60
			}
			if prom.Security.RateLimit.BurstSize <= 0 {
				prom.Security.RateLimit.BurstSize = prom.Security.RateLimit.RequestsPerMin / 2
			}
			if prom.Security.RateLimit.CleanupInterval <= 0 {
				prom.Security.RateLimit.CleanupInterval = 1 * time.Minute
			}
			// Validate whitelist IPs
			for _, ip := range prom.Security.RateLimit.WhitelistIPs {
				if net.ParseIP(ip) == nil {
					// Try parsing as CIDR
					if _, _, err := net.ParseCIDR(ip); err != nil {
						return fmt.Errorf("Invalid IP/CIDR in rate limit whitelist: %s", ip)
					}
				}
			}
		}
		
		if prom.Security.RequestLogging.Enabled {
			validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
			if prom.Security.RequestLogging.LogLevel == "" {
				prom.Security.RequestLogging.LogLevel = "info"
			} else if !validLogLevels[prom.Security.RequestLogging.LogLevel] {
				return fmt.Errorf("Invalid request logging level: %s (must be debug, info, warn, or error)", prom.Security.RequestLogging.LogLevel)
			}
			if prom.Security.RequestLogging.SlowRequestThreshold <= 0 {
				prom.Security.RequestLogging.SlowRequestThreshold = 5 * time.Second
			}
		}
	}

	// Perform comprehensive validation with connectivity and privilege checks
	validationResult, err := ValidateComprehensive(c)
	if err != nil {
		// Log warnings but don't fail on non-critical issues
		if validationResult != nil && len(validationResult.Warnings) > 0 {
			for _, warning := range validationResult.Warnings {
				fmt.Fprintf(os.Stderr, "Warning: %s: %s\n", warning.Field, warning.Message)
			}
		}
		
		// Report errors with remediation advice
		if validationResult != nil && len(validationResult.Errors) > 0 {
			for _, error := range validationResult.Errors {
				fmt.Fprintf(os.Stderr, "Error: %s: %s\n", error.Field, error.Message)
				if error.Remediation != "" {
					fmt.Fprintf(os.Stderr, "  Remediation: %s\n", error.Remediation)
				}
			}
		}
		
		return fmt.Errorf("comprehensive validation failed: %w", err)
	}

	return nil
}

func DefaultMetricsConfig() *MetricsConfig {
	return &MetricsConfig{
		Enabled:             true,
		WindowDuration:      15 * time.Minute,
		MaxStoredResults:    100000,
		CalculationInterval: 10 * time.Second,
		EnabledMetrics:      []string{"latency", "rates", "throughput", "distribution"},
		PercentilePrecision: 3,
		RateWindowSizes:     []time.Duration{1 * time.Minute, 5 * time.Minute, 15 * time.Minute},
		ThroughputSmoothing: true,
		Export: ExportConfig{
			Prometheus: PrometheusConfig{
				Enabled:                     true,
				Port:                        9090,
				Path:                        "/metrics",
				UpdateInterval:              30 * time.Second,
				IncludeServerLabels:         true,
				IncludeProtocolLabels:       true,
				MetricPrefix:                "dns",
				EnableLatencySampling:       false,
				DetailedValidation:          false,
				CounterStateCleanupInterval: 1 * time.Hour,
				CounterStateMaxAge:          2 * time.Hour,
				Security: PrometheusSecurityConfig{
					BasicAuth: BasicAuthConfig{
						Enabled: false,
						Users:   map[string]string{},
						Realm:   "Prometheus Metrics",
					},
					TLS: TLSConfig{
						Enabled:           false,
						CertFile:          "",
						KeyFile:           "",
						ClientAuth:        false,
						ClientCACertFiles: []string{},
					},
					RateLimit: RateLimitConfig{
						Enabled:         false,
						RequestsPerMin:  60,
						BurstSize:       30,
						PerIP:           true,
						WhitelistIPs:    []string{},
						CleanupInterval: 1 * time.Minute,
					},
					RequestLogging: RequestLoggingConfig{
						Enabled:              false,
						LogLevel:             "info",
						LogHeaders:           false,
						LogBody:              false,
						ExcludePaths:         []string{"/health", "/ready"},
						SlowRequestThreshold: 5 * time.Second,
					},
				},
			},
			Zabbix: ZabbixConfig{
				Enabled:       false,
				Server:        "localhost",
				Port:          10051,
				HostName:      "dns-monitor",
				SendInterval:  60 * time.Second,
				BatchSize:     100,
				Timeout:       10 * time.Second,
				RetryAttempts: 3,
				RetryDelay:    5 * time.Second,
				ItemPrefix:    "dns",
			},
		},
	}
}

func validateBPFFilter(filter string) error {
	if filter == "" {
		return nil
	}
	
	// Try multiple approaches for robust BPF filter validation
	
	// Approach 1: Try to create an inactive handle on a dummy interface
	// First, try with "any" pseudo-device which works on many systems
	inactiveHandle, err := pcap.NewInactiveHandle("any")
	if err != nil {
		// If "any" doesn't work, try with "lo" (loopback)
		inactiveHandle, err = pcap.NewInactiveHandle("lo")
		if err != nil {
			// If neither works, fall back to basic validation
			return validateBPFFilterBasic(filter)
		}
	}
	defer inactiveHandle.CleanUp()
	
	// Set common parameters for DNS packet capture
	// These settings match typical DNS monitoring requirements
	if err := inactiveHandle.SetSnapLen(65535); err != nil {
		// Fall back to basic validation if inactive handle setup fails
		return validateBPFFilterBasic(filter)
	}
	
	if err := inactiveHandle.SetTimeout(time.Millisecond); err != nil {
		// Fall back to basic validation
		return validateBPFFilterBasic(filter)
	}
	
	// Try to activate the handle (this creates a dummy handle for filter compilation)
	handle, err := inactiveHandle.Activate()
	if err != nil {
		// If we can't activate an inactive handle, fall back to basic validation
		return validateBPFFilterBasic(filter)
	}
	defer handle.Close()
	
	// Now compile the BPF filter with the actual handle
	// This provides the most accurate validation
	if err := handle.SetBPFFilter(filter); err != nil {
		return fmt.Errorf("BPF filter validation failed: %w", err)
	}
	
	// Additional semantic validation for DNS-specific filters
	if err := validateDNSBPFSemantics(filter); err != nil {
		return fmt.Errorf("DNS-specific BPF validation: %w", err)
	}
	
	return nil
}

// validateBPFFilterBasic performs basic BPF syntax validation
// This is used as a fallback when more advanced validation isn't available
func validateBPFFilterBasic(filter string) error {
	// Use pcap.CompileBPFFilter for basic syntax validation
	// LinkType 1 = Ethernet (most common)
	// SnapLen 65535 = Maximum packet size
	_, err := pcap.CompileBPFFilter(layers.LinkTypeEthernet, 65535, filter)
	if err != nil {
		// Also try with raw IP link type (for some VPN/tunnel interfaces)
		_, err2 := pcap.CompileBPFFilter(layers.LinkTypeRaw, 65535, filter)
		if err2 != nil {
			// Return the original Ethernet error as it's more common
			return fmt.Errorf("BPF compilation failed: %w", err)
		}
	}
	
	// Additional semantic validation
	if err := validateDNSBPFSemantics(filter); err != nil {
		return fmt.Errorf("DNS-specific BPF validation: %w", err)
	}
	
	return nil
}

// validateDNSBPFSemantics performs semantic validation specific to DNS monitoring
func validateDNSBPFSemantics(filter string) error {
	// Check for common DNS-related BPF patterns and potential issues
	lowerFilter := strings.ToLower(filter)
	
	// Warn about filters that might miss DNS traffic
	if !strings.Contains(lowerFilter, "port") && !strings.Contains(lowerFilter, "udp") && 
	   !strings.Contains(lowerFilter, "tcp") && filter != "" {
		// Filter doesn't mention port or protocol - might be too broad
		fmt.Fprintf(os.Stderr, "Warning: BPF filter '%s' doesn't specify port or protocol - may capture non-DNS traffic\n", filter)
	}
	
	// Check for DNS-specific port filtering
	if strings.Contains(lowerFilter, "port") {
		// Check if it includes standard DNS port
		if !strings.Contains(lowerFilter, "53") {
			fmt.Fprintf(os.Stderr, "Warning: BPF filter specifies port but not port 53 (standard DNS) - may miss DNS traffic\n")
		}
	}
	
	// Check for overly complex filters that might impact performance
	// Count the number of logical operators
	orCount := strings.Count(lowerFilter, " or ")
	andCount := strings.Count(lowerFilter, " and ")
	if orCount+andCount > 10 {
		fmt.Fprintf(os.Stderr, "Warning: Complex BPF filter with %d logical operators may impact capture performance\n", orCount+andCount)
	}
	
	// Check for common mistakes in DNS filters
	if strings.Contains(lowerFilter, "port 53") {
		// Check if both UDP and TCP are covered
		hasUDP := strings.Contains(lowerFilter, "udp")
		hasTCP := strings.Contains(lowerFilter, "tcp")
		
		if (hasUDP && !hasTCP) || (!hasUDP && hasTCP) {
			// Only one protocol specified
			proto := "UDP"
			if hasTCP {
				proto = "TCP"
			}
			fmt.Fprintf(os.Stderr, "Info: BPF filter only captures %s DNS traffic. Consider including both UDP and TCP for complete DNS monitoring\n", proto)
		}
	}
	
	// Validate parentheses balance
	openCount := strings.Count(filter, "(")
	closeCount := strings.Count(filter, ")")
	if openCount != closeCount {
		return fmt.Errorf("unbalanced parentheses: %d opening, %d closing", openCount, closeCount)
	}
	
	// Check for empty expressions between logical operators
	patterns := []string{" and and ", " or or ", " and or ", " or and "}
	for _, pattern := range patterns {
		if strings.Contains(lowerFilter, pattern) {
			return fmt.Errorf("invalid logical operator sequence: %s", pattern)
		}
	}
	
	// Check for invalid negation patterns
	if strings.Contains(filter, "not not") {
		return fmt.Errorf("double negation 'not not' is invalid")
	}
	
	return nil
}

func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// validateDoHEndpoint validates a DoH endpoint path
func validateDoHEndpoint(endpoint string) error {
	// Must start with /
	if !strings.HasPrefix(endpoint, "/") {
		return fmt.Errorf("DoH endpoint must start with '/', got %s", endpoint)
	}
	
	// Check for query parameters or fragments
	if strings.Contains(endpoint, "?") {
		return fmt.Errorf("DoH endpoint cannot contain query parameters: %s", endpoint)
	}
	
	if strings.Contains(endpoint, "#") {
		return fmt.Errorf("DoH endpoint cannot contain fragments: %s", endpoint)
	}
	
	// Check for spaces first (more specific error message)
	if strings.Contains(endpoint, " ") {
		return fmt.Errorf("DoH endpoint cannot contain spaces: %s", endpoint)
	}
	
	// Validate URL path characters using regex
	// RFC 3986 allows: unreserved chars (a-zA-Z0-9-._~) and some reserved chars (/:@!$&'()*+,;=)
	// For paths, we allow alphanumeric, -, _, ., ~, /, and common safe chars
	validPathRegex := regexp.MustCompile(`^/[a-zA-Z0-9\-._~/:@!$&'()*+,;=]*$`)
	if !validPathRegex.MatchString(endpoint) {
		return fmt.Errorf("DoH endpoint contains invalid characters: %s", endpoint)
	}
	
	// Check for double slashes (except at the beginning for protocol)
	if strings.Contains(endpoint, "//") {
		return fmt.Errorf("DoH endpoint contains invalid double slashes: %s", endpoint)
	}
	
	// Check for common DoH endpoint patterns (warning if not standard)
	commonEndpoints := []string{
		"/dns-query",           // RFC 8484 standard
		"/resolve",             // Cloudflare alternative
		"/dns",                 // Generic
		"/query",               // Generic
		"/dns-query/resolve",   // Some providers
		"/doh",                 // Generic
	}
	
	isCommon := false
	for _, common := range commonEndpoints {
		if endpoint == common || strings.HasPrefix(endpoint, common+"/") {
			isCommon = true
			break
		}
	}
	
	if !isCommon {
		// This is just a warning in the validation, not an error
		// The actual endpoint might be valid for a specific provider
		fmt.Fprintf(os.Stderr, "Warning: DoH endpoint '%s' doesn't match common patterns (/dns-query, /resolve, etc.)\n", endpoint)
	}
	
	return nil
}

// validateDoHURL validates that the full DoH URL will be valid
func validateDoHURL(serverAddress, endpoint string) error {
	// Check for invalid schemes first
	if strings.Contains(serverAddress, "://") {
		// Has a scheme, make sure it's http or https
		if !strings.HasPrefix(serverAddress, "http://") && !strings.HasPrefix(serverAddress, "https://") {
			parsedURL, _ := url.Parse(serverAddress)
			scheme := ""
			if parsedURL != nil {
				scheme = parsedURL.Scheme
			}
			return fmt.Errorf("DoH URL must use http or https scheme, got %s", scheme)
		}
	} else {
		// Add scheme if missing (protocols.go does this too)
		serverAddress = "https://" + serverAddress
	}
	
	// First parse the server address to check for issues
	parsedServer, err := url.Parse(serverAddress)
	if err != nil {
		return fmt.Errorf("invalid server address %s: %w", serverAddress, err)
	}
	
	// Check if server address has query or fragment (which it shouldn't)
	if parsedServer.RawQuery != "" {
		return fmt.Errorf("DoH server address should not contain query parameters: %s", serverAddress)
	}
	
	if parsedServer.Fragment != "" {
		return fmt.Errorf("DoH server address should not contain fragments: %s", serverAddress)
	}
	
	// Check if server address already has a path (which it shouldn't, except for the root)
	if parsedServer.Path != "" && parsedServer.Path != "/" {
		return fmt.Errorf("DoH server address should not contain a path: %s", serverAddress)
	}
	
	// Validate the hostname in the URL
	if parsedServer.Hostname() == "" {
		return fmt.Errorf("DoH server address missing hostname: %s", serverAddress)
	}
	
	// Validate that the hostname is a valid IP or domain name
	if err := validateHostnameOrIP(parsedServer.Hostname()); err != nil {
		return fmt.Errorf("DoH server has invalid hostname: %w", err)
	}
	
	// Construct the full URL
	fullURL := serverAddress + endpoint
	
	// Parse the full URL to ensure it's valid
	parsedURL, err := url.Parse(fullURL)
	if err != nil {
		return fmt.Errorf("invalid DoH URL construction %s: %w", fullURL, err)
	}
	
	// Check scheme
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("DoH URL must use http or https scheme, got %s", parsedURL.Scheme)
	}
	
	// Check host is present
	if parsedURL.Host == "" {
		return fmt.Errorf("DoH URL missing host: %s", fullURL)
	}
	
	// Validate path matches our endpoint
	if parsedURL.Path != endpoint {
		return fmt.Errorf("DoH URL path mismatch: expected %s, got %s", endpoint, parsedURL.Path)
	}
	
	return nil
}

// validateHostnameOrIP validates that the given string is a valid hostname or IP address
func validateHostnameOrIP(address string) error {
	// Check if it's a valid IP address (v4 or v6)
	if net.ParseIP(address) != nil {
		return nil
	}
	
	// Check if it's a valid hostname
	// Hostname rules according to RFC 1123:
	// - Must be 1-253 characters total
	// - Each label must be 1-63 characters
	// - Labels can contain a-z, A-Z, 0-9, and hyphens
	// - Labels cannot start or end with hyphens
	// - Labels cannot consist entirely of digits (to distinguish from IPs)
	
	if len(address) == 0 || len(address) > 253 {
		return fmt.Errorf("hostname length must be between 1 and 253 characters, got %d", len(address))
	}
	
	// Split into labels
	labels := strings.Split(address, ".")
	if len(labels) == 0 {
		return fmt.Errorf("invalid hostname format")
	}
	
	// Regex for valid label: starts and ends with alphanumeric, can contain hyphens in the middle
	labelRegex := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
	
	for i, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return fmt.Errorf("hostname label '%s' must be between 1 and 63 characters", label)
		}
		
		if !labelRegex.MatchString(label) {
			return fmt.Errorf("hostname label '%s' contains invalid characters or format", label)
		}
		
		// Check if it's all digits (except for the last label which can be a TLD)
		if i < len(labels)-1 && regexp.MustCompile(`^[0-9]+$`).MatchString(label) {
			return fmt.Errorf("hostname label '%s' cannot consist entirely of digits", label)
		}
	}
	
	return nil
}