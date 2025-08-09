package config

import (
	"os"
	"testing"
)

func TestValidateBPFFilter(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		wantErr bool
	}{
		{
			name:    "valid port filter",
			filter:  "port 53",
			wantErr: false,
		},
		{
			name:    "valid tcp filter",
			filter:  "tcp port 53",
			wantErr: false,
		},
		{
			name:    "valid udp filter",
			filter:  "udp port 53",
			wantErr: false,
		},
		{
			name:    "valid complex filter",
			filter:  "tcp port 53 or udp port 53",
			wantErr: false,
		},
		{
			name:    "valid host filter",
			filter:  "host 8.8.8.8",
			wantErr: false,
		},
		{
			name:    "valid combined filter",
			filter:  "host 8.8.8.8 and port 53",
			wantErr: false,
		},
		{
			name:    "empty filter",
			filter:  "",
			wantErr: false,
		},
		{
			name:    "invalid syntax - missing port number",
			filter:  "port",
			wantErr: true,
		},
		{
			name:    "invalid syntax - bad keyword",
			filter:  "invalid filter",
			wantErr: true,
		},
		{
			name:    "invalid syntax - unmatched parenthesis",
			filter:  "(tcp port 53",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBPFFilter(tt.filter)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateBPFFilter() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigValidationWithBPF(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid BPF filter in passive config",
			config: &Config{
				DNS: DNSConfig{
					Servers: []DNSServer{
						{
							Name:    "Test",
							Address: "8.8.8.8",
							Port:    53,
							Enabled: true,
						},
					},
					Queries: QueryConfig{
						Types:   []string{"A"},
						Domains: []string{"example.com"},
					},
				},
				Passive: PassiveConfig{
					Enabled:    true,
					Interfaces: []string{"eth0"},
					BPF:        "port 53",
					Workers:    4,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid BPF filter in passive config",
			config: &Config{
				DNS: DNSConfig{
					Servers: []DNSServer{
						{
							Name:    "Test",
							Address: "8.8.8.8",
							Port:    53,
							Enabled: true,
						},
					},
					Queries: QueryConfig{
						Types:   []string{"A"},
						Domains: []string{"example.com"},
					},
				},
				Passive: PassiveConfig{
					Enabled:    true,
					Interfaces: []string{"eth0"},
					BPF:        "invalid filter syntax",
					Workers:    4,
				},
			},
			wantErr: true,
		},
		{
			name: "passive disabled - BPF not validated",
			config: &Config{
				DNS: DNSConfig{
					Servers: []DNSServer{
						{
							Name:    "Test",
							Address: "8.8.8.8",
							Port:    53,
							Enabled: true,
						},
					},
					Queries: QueryConfig{
						Types:   []string{"A"},
						Domains: []string{"example.com"},
					},
				},
				Passive: PassiveConfig{
					Enabled:    false,
					Interfaces: []string{"eth0"},
					BPF:        "invalid filter syntax",
					Workers:    4,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateDoHEndpoint(t *testing.T) {
	// Suppress warnings for test
	oldStderr := os.Stderr
	os.Stderr, _ = os.Open(os.DevNull)
	defer func() { os.Stderr = oldStderr }()

	tests := []struct {
		name     string
		endpoint string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "valid standard endpoint",
			endpoint: "/dns-query",
			wantErr:  false,
		},
		{
			name:     "valid cloudflare endpoint",
			endpoint: "/resolve",
			wantErr:  false,
		},
		{
			name:     "valid generic dns endpoint",
			endpoint: "/dns",
			wantErr:  false,
		},
		{
			name:     "valid nested path",
			endpoint: "/dns-query/resolve",
			wantErr:  false,
		},
		{
			name:     "valid with special chars",
			endpoint: "/dns-query/v1.0",
			wantErr:  false,
		},
		{
			name:     "missing leading slash",
			endpoint: "dns-query",
			wantErr:  true,
			errMsg:   "must start with '/'",
		},
		{
			name:     "contains query parameters",
			endpoint: "/dns-query?type=A",
			wantErr:  true,
			errMsg:   "cannot contain query parameters",
		},
		{
			name:     "contains fragment",
			endpoint: "/dns-query#section",
			wantErr:  true,
			errMsg:   "cannot contain fragments",
		},
		{
			name:     "contains double slashes",
			endpoint: "/dns//query",
			wantErr:  true,
			errMsg:   "invalid double slashes",
		},
		{
			name:     "contains spaces",
			endpoint: "/dns query",
			wantErr:  true,
			errMsg:   "cannot contain spaces",
		},
		{
			name:     "contains invalid characters",
			endpoint: "/dns<query>",
			wantErr:  true,
			errMsg:   "invalid characters",
		},
		{
			name:     "contains backslash",
			endpoint: "/dns\\query",
			wantErr:  true,
			errMsg:   "invalid characters",
		},
		{
			name:     "non-standard but valid",
			endpoint: "/custom/doh/endpoint",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDoHEndpoint(tt.endpoint)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDoHEndpoint() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateDoHEndpoint() error message = %v, want to contain %v", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestValidateDoHURL(t *testing.T) {
	tests := []struct {
		name          string
		serverAddress string
		endpoint      string
		wantErr       bool
		errMsg        string
	}{
		{
			name:          "valid HTTPS URL",
			serverAddress: "https://cloudflare-dns.com",
			endpoint:      "/dns-query",
			wantErr:       false,
		},
		{
			name:          "valid HTTPS with port",
			serverAddress: "https://dns.example.com:8443",
			endpoint:      "/dns-query",
			wantErr:       false,
		},
		{
			name:          "valid HTTP URL",
			serverAddress: "http://localhost:8080",
			endpoint:      "/dns-query",
			wantErr:       false,
		},
		{
			name:          "invalid scheme",
			serverAddress: "ftp://example.com",
			endpoint:      "/dns-query",
			wantErr:       true,
			errMsg:        "must use http or https scheme",
		},
		{
			name:          "missing host",
			serverAddress: "https://",
			endpoint:      "/dns-query",
			wantErr:       true,
			errMsg:        "missing host",
		},
		{
			name:          "URL with query params",
			serverAddress: "https://example.com?param=value",
			endpoint:      "/dns-query",
			wantErr:       true,
			errMsg:        "server address should not contain query parameters",
		},
		{
			name:          "URL with fragment",
			serverAddress: "https://example.com#section",
			endpoint:      "/dns-query",
			wantErr:       true,
			errMsg:        "server address should not contain fragments",
		},
		{
			name:          "malformed URL",
			serverAddress: "https://[invalid",
			endpoint:      "/dns-query",
			wantErr:       true,
			errMsg:        "invalid server address",
		},
		{
			name:          "server with path",
			serverAddress: "https://example.com/path",
			endpoint:      "/dns-query",
			wantErr:       true,
			errMsg:        "server address should not contain a path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDoHURL(tt.serverAddress, tt.endpoint)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDoHURL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateDoHURL() error message = %v, want to contain %v", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestConfigValidationWithDoH(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid DoH configuration",
			config: &Config{
				DNS: DNSConfig{
					Servers: []DNSServer{
						{
							Name:        "Cloudflare DoH",
							Address:     "cloudflare-dns.com",
							Port:        443,
							Enabled:     true,
							Protocol:    "doh",
							DoHEndpoint: "/dns-query",
						},
					},
					Queries: QueryConfig{
						Types:   []string{"A"},
						Domains: []string{"example.com"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "DoH with invalid endpoint",
			config: &Config{
				DNS: DNSConfig{
					Servers: []DNSServer{
						{
							Name:        "Invalid DoH",
							Address:     "example.com",
							Port:        443,
							Enabled:     true,
							Protocol:    "doh",
							DoHEndpoint: "dns-query", // missing leading slash
						},
					},
					Queries: QueryConfig{
						Types:   []string{"A"},
						Domains: []string{"example.com"},
					},
				},
			},
			wantErr: true,
			errMsg:  "must start with '/'",
		},
		{
			name: "DoH with query parameters",
			config: &Config{
				DNS: DNSConfig{
					Servers: []DNSServer{
						{
							Name:        "Invalid DoH",
							Address:     "example.com",
							Port:        443,
							Enabled:     true,
							Protocol:    "doh",
							DoHEndpoint: "/dns-query?ct=application/dns-json",
						},
					},
					Queries: QueryConfig{
						Types:   []string{"A"},
						Domains: []string{"example.com"},
					},
				},
			},
			wantErr: true,
			errMsg:  "cannot contain query parameters",
		},
		{
			name: "DoH with default endpoint",
			config: &Config{
				DNS: DNSConfig{
					Servers: []DNSServer{
						{
							Name:        "DoH with default",
							Address:     "dns.google",
							Port:        443,
							Enabled:     true,
							Protocol:    "doh",
							DoHEndpoint: "", // should default to /dns-query
						},
					},
					Queries: QueryConfig{
						Types:   []string{"A"},
						Domains: []string{"example.com"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "DoH auto-adds https",
			config: &Config{
				DNS: DNSConfig{
					Servers: []DNSServer{
						{
							Name:        "DoH auto https",
							Address:     "dns.example.com", // no scheme
							Port:        443,
							Enabled:     true,
							Protocol:    "doh",
							DoHEndpoint: "/dns-query",
						},
					},
					Queries: QueryConfig{
						Types:   []string{"A"},
						Domains: []string{"example.com"},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("Config.validate() error message = %v, want to contain %v", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr || 
		len(s) >= len(substr) && contains(s[1:], substr)
}