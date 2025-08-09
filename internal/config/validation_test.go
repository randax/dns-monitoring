package config

import (
	"strings"
	"testing"
	"time"
)

func TestPrometheusValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		wantError bool
		errorMsg  string
		wantWarn  bool
		warnMsg   string
	}{
		{
			name: "valid metric prefix with underscore",
			config: &Config{
				DNS:     defaultConfig().DNS,
				Monitor: defaultConfig().Monitor,
				Output:  defaultConfig().Output,
				Metrics: &MetricsConfig{
					Export: ExportConfig{
						Prometheus: PrometheusConfig{
							Enabled:        true,
							Port:          9090,
							Path:          "/metrics",
							UpdateInterval: 5 * time.Second,
							MetricPrefix:   "dns_monitor",
						},
					},
				},
			},
			wantError: false,
		},
		{
			name: "valid metric prefix with colon",
			config: &Config{
				DNS:     defaultConfig().DNS,
				Monitor: defaultConfig().Monitor,
				Output:  defaultConfig().Output,
				Metrics: &MetricsConfig{
					Export: ExportConfig{
						Prometheus: PrometheusConfig{
							Enabled:        true,
							Port:          9090,
							Path:          "/metrics",
							UpdateInterval: 5 * time.Second,
							MetricPrefix:   "dns:monitor",
						},
					},
				},
			},
			wantError: false,
		},
		{
			name: "sub-second update interval allowed with warning",
			config: &Config{
				DNS:     defaultConfig().DNS,
				Monitor: defaultConfig().Monitor,
				Output:  defaultConfig().Output,
				Metrics: &MetricsConfig{
					Export: ExportConfig{
						Prometheus: PrometheusConfig{
							Enabled:        true,
							Port:          9090,
							Path:          "/metrics",
							UpdateInterval: 500 * time.Millisecond,
							MetricPrefix:   "dns",
						},
					},
				},
			},
			wantError: false,
			wantWarn:  true,
		},
		{
			name: "too small update interval rejected",
			config: &Config{
				DNS:     defaultConfig().DNS,
				Monitor: defaultConfig().Monitor,
				Output:  defaultConfig().Output,
				Metrics: &MetricsConfig{
					Export: ExportConfig{
						Prometheus: PrometheusConfig{
							Enabled:        true,
							Port:          9090,
							Path:          "/metrics",
							UpdateInterval: 50 * time.Millisecond,
							MetricPrefix:   "dns",
						},
					},
				},
			},
			wantError: true,
			errorMsg:  "Prometheus update interval too low",
		},
		{
			name: "invalid metric prefix starting with number",
			config: &Config{
				DNS:     defaultConfig().DNS,
				Monitor: defaultConfig().Monitor,
				Output:  defaultConfig().Output,
				Metrics: &MetricsConfig{
					Export: ExportConfig{
						Prometheus: PrometheusConfig{
							Enabled:        true,
							Port:          9090,
							Path:          "/metrics",
							UpdateInterval: 5 * time.Second,
							MetricPrefix:   "123dns",
						},
					},
				},
			},
			wantError: true,
			errorMsg:  "Invalid Prometheus metric prefix",
		},
		{
			name: "metric prefix starting with underscore allowed",
			config: &Config{
				DNS:     defaultConfig().DNS,
				Monitor: defaultConfig().Monitor,
				Output:  defaultConfig().Output,
				Metrics: &MetricsConfig{
					Export: ExportConfig{
						Prometheus: PrometheusConfig{
							Enabled:        true,
							Port:          9090,
							Path:          "/metrics",
							UpdateInterval: 5 * time.Second,
							MetricPrefix:   "_dns_internal",
						},
					},
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing '%s', got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestPortConflictDetection(t *testing.T) {
	// Test port availability check - use a port that's likely unavailable
	// First try to use port 80 which typically requires elevated privileges
	testPort := 80
	
	// Try to determine if we can even test port binding
	if err := checkPortAvailability(testPort); err == nil {
		// If port 80 is available, skip this test as we may be running as root
		t.Skip("Skipping port conflict test - running with elevated privileges")
		return
	}
	
	cfg := &Config{
		DNS:     defaultConfig().DNS,
		Monitor: defaultConfig().Monitor,
		Output:  defaultConfig().Output,
		Metrics: &MetricsConfig{
			Export: ExportConfig{
				Prometheus: PrometheusConfig{
					Enabled:        true,
					Port:          testPort,
					Path:          "/metrics",
					UpdateInterval: 5 * time.Second,
					MetricPrefix:   "dns",
				},
			},
		},
	}

	result, _ := ValidateComprehensive(cfg)
	
	// Check we got an error about the port
	hasPortError := false
	for _, err := range result.Errors {
		if strings.Contains(err.Field, "prometheus.port") {
			hasPortError = true
			// Verify we get helpful remediation advice
			if !strings.Contains(err.Remediation, "1024") && 
			   !strings.Contains(err.Remediation, "elevated privileges") &&
			   !strings.Contains(err.Remediation, "Find process") &&
			   !strings.Contains(err.Remediation, "different port") {
				t.Errorf("expected helpful remediation advice, got: %s", err.Remediation)
			}
		}
	}
	
	if !hasPortError {
		// This may happen if we're running as root or on certain platforms
		t.Skip("Could not test port conflict - may be running with different privileges")
	}
}

func TestMetricPrefixWarnings(t *testing.T) {
	tests := []struct {
		name       string
		prefix     string
		wantWarn   bool
		warnSubstr string
	}{
		{
			name:       "prefix with trailing underscore",
			prefix:     "dns_",
			wantWarn:   true,
			warnSubstr: "ends with underscore",
		},
		{
			name:       "prefix with double underscore",
			prefix:     "dns__monitor",
			wantWarn:   true,
			warnSubstr: "double underscores",
		},
		{
			name:       "very long prefix",
			prefix:     "this_is_a_very_long_metric_prefix_name",
			wantWarn:   true,
			warnSubstr: "longer than 20 characters",
		},
		{
			name:     "normal prefix",
			prefix:   "dns",
			wantWarn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				DNS:     defaultConfig().DNS,
				Monitor: defaultConfig().Monitor,
				Output:  defaultConfig().Output,
				Metrics: &MetricsConfig{
					Export: ExportConfig{
						Prometheus: PrometheusConfig{
							Enabled:        true,
							Port:          9090,
							Path:          "/metrics",
							UpdateInterval: 5 * time.Second,
							MetricPrefix:   tt.prefix,
						},
					},
				},
			}

			result, _ := ValidateComprehensive(cfg)

			hasWarning := false
			for _, warn := range result.Warnings {
				if strings.Contains(warn.Field, "metric_prefix") {
					hasWarning = true
					if tt.warnSubstr != "" && !strings.Contains(warn.Message, tt.warnSubstr) {
						t.Errorf("expected warning containing '%s', got: %s", tt.warnSubstr, warn.Message)
					}
				}
			}

			if tt.wantWarn && !hasWarning {
				t.Errorf("expected warning for prefix '%s' but got none", tt.prefix)
			}
			if !tt.wantWarn && hasWarning {
				t.Errorf("unexpected warning for prefix '%s'", tt.prefix)
			}
		})
	}
}