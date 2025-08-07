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
	DNS     DNSConfig     `yaml:"dns"`
	Monitor MonitorConfig `yaml:"monitor"`
	Output  OutputConfig  `yaml:"output"`
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
}

type MetricConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint"`
	Interval int    `yaml:"interval"`
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