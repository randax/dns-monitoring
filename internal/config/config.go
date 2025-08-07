package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

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
				
				if !strings.HasPrefix(server.DoHEndpoint, "/") {
					return fmt.Errorf("DNS server %d: DoH endpoint must start with '/', got %s", i, server.DoHEndpoint)
				}
				
				if strings.Contains(server.DoHEndpoint, "//") {
					return fmt.Errorf("DNS server %d: DoH endpoint contains invalid double slashes: %s", i, server.DoHEndpoint)
				}
				
				if strings.Contains(server.DoHEndpoint, " ") {
					return fmt.Errorf("DNS server %d: DoH endpoint cannot contain spaces: %s", i, server.DoHEndpoint)
				}
				
				if !strings.HasPrefix(server.Address, "http://") && !strings.HasPrefix(server.Address, "https://") {
					server.Address = "https://" + server.Address
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
				Enabled:               true,
				Port:                  9090,
				Path:                  "/metrics",
				UpdateInterval:        30 * time.Second,
				IncludeServerLabels:   true,
				IncludeProtocolLabels: true,
				MetricPrefix:          "dns",
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