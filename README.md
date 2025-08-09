# DNS Monitoring Tool

A comprehensive DNS monitoring solution with real-time metrics, cache analysis, and network performance tracking.

## Overview

DNS Monitoring Tool is an enterprise-grade DNS monitoring solution that provides deep insights into DNS performance, cache behavior, and network quality. It supports both active and passive monitoring modes, multiple DNS protocols (UDP, TCP, DoT, DoH), and exports metrics to various monitoring platforms.

## Features

### Core Capabilities
- **Active DNS Monitoring**: Scheduled queries to configured DNS servers with multi-protocol support
- **Passive DNS Monitoring**: Real-time network traffic capture and analysis with BPF filtering
- **Hybrid Monitoring**: Simultaneous active and passive monitoring for comprehensive visibility
- **Multi-Protocol Support**: UDP, TCP, DNS-over-TLS (DoT), DNS-over-HTTPS (DoH) with full configuration
- **Cache Performance Analysis**: Multi-method cache detection (timing, TTL, pattern) with efficiency scoring
- **Network Metrics**: Packet loss detection, jitter calculation, hop count analysis, quality scoring
- **Real-time Metrics**: Live performance dashboards with configurable percentile calculations
- **Multiple Export Formats**: Prometheus, Zabbix, CLI, JSON, CSV with extensive customization

### Advanced Features
- **Adaptive Sampling**: Memory-aware and QPS-based automatic sampling rate adjustment
- **Backpressure Management**: Intelligent load handling with buffer management and suspension logic
- **TCP Stream Reassembly**: Optional reassembly for fragmented DNS over TCP packets
- **Query Matching**: Transaction ID-based correlation with timeout handling
- **Interface Detection**: Auto-discovery of network interfaces with per-interface statistics
- **Configuration Validation**: Comprehensive validation with remediation advice
- **Resource Management**: Circular buffers, worker pools, and memory limits
- **Security Features**: TLS certificate validation, basic auth for exporters, privilege checking

## Installation

### Prerequisites
- Go 1.21 or higher
- Root/admin privileges (for passive monitoring)
- Network access to DNS servers
- libpcap-dev (for passive monitoring on Linux)

### Build from Source
```bash
# Clone the repository
git clone https://github.com/randax/dns-monitoring.git
cd dns-monitoring

# Install dependencies
go mod download

# Build the binary
go build -o dns-monitor ./cmd/dns-monitoring

# Optional: Install system-wide
sudo cp dns-monitor /usr/local/bin/

# Build with specific features
go build -tags "pcap" -o dns-monitor ./cmd/dns-monitoring  # Include packet capture
```

### Platform-specific Builds
```bash
# Linux AMD64
GOOS=linux GOARCH=amd64 go build -o dns-monitor-linux ./cmd/dns-monitoring

# macOS AMD64
GOOS=darwin GOARCH=amd64 go build -o dns-monitor-mac ./cmd/dns-monitoring

# macOS ARM64 (M1/M2)
GOOS=darwin GOARCH=arm64 go build -o dns-monitor-mac-arm64 ./cmd/dns-monitoring

# Windows AMD64
GOOS=windows GOARCH=amd64 go build -o dns-monitor.exe ./cmd/dns-monitoring
```

## Quick Start with Prometheus

### 1. Configure Prometheus Export

Create a `config.yaml` file with Prometheus export enabled:

```yaml
dns:
  servers:
    - name: "Google DNS"
      address: "8.8.8.8"
      port: 53
      enabled: true
      protocol: "udp"
    - name: "Cloudflare DNS"
      address: "1.1.1.1"
      port: 53
      enabled: true
      protocol: "udp"
  queries:
    types: ["A", "AAAA"]
    domains: ["google.com", "github.com", "cloudflare.com"]
    timeout: 5s
    retries: 2

monitor:
  interval: 30s
  max_concurrent: 10

metrics:
  enabled: true
  export:
    prometheus:
      enabled: true
      port: 9090
      path: "/metrics"
      update_interval: 30s
      metric_prefix: "dns"
```

### 2. Start the Monitor

```bash
# Start monitoring with Prometheus exporter
dns-monitor monitor -k -c config.yaml

# Metrics will be available at http://localhost:9090/metrics
```

### 3. Configure Prometheus

Add to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'dns-monitor'
    static_configs:
      - targets: ['localhost:9090']
    scrape_interval: 30s
```

### 4. View Metrics

Access metrics at `http://localhost:9090/metrics`. Key metrics include:
- `dns_latency_p95_seconds` - 95th percentile latency
- `dns_queries_total` - Total queries performed
- `dns_success_rate` - Queries per second (successful)
- `dns_error_total` - Failed queries count

For detailed Prometheus configuration and Grafana dashboards, see [docs/prometheus-exporter.md](docs/prometheus-exporter.md).

### Docker Installation
```bash
# Build Docker image
docker build -t dns-monitor .

# Run with configuration
docker run -v $(pwd)/config.yaml:/app/config.yaml dns-monitor
```

## Usage Examples

### Basic DNS Monitoring
```bash
# Start monitoring with default configuration
./dns-monitor monitor -c config.yaml

# Monitor specific servers with custom domains
./dns-monitor monitor -s "8.8.8.8:53,1.1.1.1:53" -d "example.com,google.com"

# Enable verbose output with colored CLI display
./dns-monitor monitor -c config.yaml -v --color

# Quick monitoring with minimal configuration
./dns-monitor monitor -c config-minimal.yaml
```

### Active Monitoring Examples
```bash
# UDP monitoring with specific query types
./dns-monitor monitor -c config.yaml --query-types "A,AAAA,MX"

# DNS over TLS monitoring
./dns-monitor monitor --protocol dot --server "1.1.1.1:853"

# DNS over HTTPS monitoring
./dns-monitor monitor --protocol doh --server "https://cloudflare-dns.com/dns-query"

# Multi-protocol monitoring
./dns-monitor monitor -c config.yaml --enable-all-protocols
```

### Enterprise Monitoring with Prometheus
```yaml
# config.yaml - Advanced Prometheus configuration
metrics:
  export:
    prometheus:
      enabled: true
      port: 9090
      path: "/metrics"
      include_server_labels: true
      include_protocol_labels: true
      metric_prefix: "dns"
      
      # Enable specific metric groups
      metric_features:
        enable_cache_metrics: true      # Cache performance metrics
        enable_network_metrics: true    # Network quality metrics
        enable_jitter_metrics: true     # Jitter analysis
        enable_ttl_metrics: true        # TTL distribution
        enable_monitoring_metrics: true # Monitor health metrics
      
      # Optional: Enable histogram/summary for PromQL queries
      enable_latency_sampling: true
      
      # Security configuration
      auth:
        enabled: true
        username: "prometheus"
        password: "secure-password"
      
      tls:
        enabled: true
        cert_file: "/path/to/cert.pem"
        key_file: "/path/to/key.pem"
```

```bash
# Start monitoring with Prometheus export
./dns-monitor monitor -c config.yaml

# Verify metrics endpoint
curl -u prometheus:secure-password https://localhost:9090/metrics
```

### Prometheus Scrape Configuration
```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'dns_monitoring'
    static_configs:
      - targets: ['localhost:9090']
    scrape_interval: 30s
    basic_auth:
      username: 'prometheus'
      password: 'secure-password'
    scheme: https
    tls_config:
      insecure_skip_verify: false
```

### Network Troubleshooting with Passive Monitoring
```bash
# Enable passive monitoring (requires root)
sudo ./dns-monitor monitor -c config.yaml --passive

# Monitor specific interface with BPF filter
sudo ./dns-monitor monitor --passive --interface eth0 --bpf "port 53"

# Capture DNS traffic for specific servers
sudo ./dns-monitor monitor --passive --bpf "port 53 and (host 8.8.8.8 or host 1.1.1.1)"

# Monitor with TCP reassembly for DNS over TCP
sudo ./dns-monitor monitor --passive --tcp-reassembly --bpf "tcp port 53"

# Passive monitoring with query matching
sudo ./dns-monitor monitor --passive --enable-query-matching --match-timeout 5s
```

### Advanced Passive Monitoring Configuration
```yaml
# passive-config.yaml
passive:
  enabled: true
  interfaces: ["eth0", "eth1"]  # Monitor multiple interfaces
  bpf: "port 53 or port 853"     # DNS and DoT traffic
  
  # TCP reassembly for fragmented packets
  tcp_reassembly:
    enabled: true
    max_streams: 1000
    max_buffer_size: 65536      # 64KB per stream
    stream_timeout: 30s
  
  # Query matching configuration
  query_matching:
    enabled: true
    match_timeout: 5s
    max_pending_queries: 10000
  
  # Backpressure management
  backpressure:
    enabled: true
    max_buffer_size: 100000
    drop_threshold: 1000        # Consecutive drops before suspension
    suspension_duration: 5s
```

### Cache Analysis for Recursive Resolvers
```yaml
# cache-analysis-config.yaml
cache:
  enabled: true
  analysis_interval: 30s
  
  # Servers to analyze (empty = all servers)
  recursive_servers:
    - "Google Primary"
    - "Cloudflare Primary"
    - "Local Resolver"
  
  # Cache detection configuration
  cache_hit_threshold: 10ms      # Response time threshold for cache hits
  ttl_tracking: true              # Track TTL decrements
  cache_efficiency_metrics: true  # Calculate efficiency scores
  
  # Detection methods (can use multiple)
  detection_methods:
    - timing    # Response time analysis
    - ttl       # TTL decrement tracking
    - pattern   # Response pattern matching
  
  # Memory management
  max_tracked_queries: 10000     # Limit tracked queries
  cleanup_interval: 5m            # Cleanup old entries
  max_cache_age: 1h              # Maximum tracking duration
```

```bash
# Run cache analysis with detailed output
./dns-monitor monitor -c cache-analysis-config.yaml --verbose

# Monitor cache performance in real-time
./dns-monitor monitor -c cache-analysis-config.yaml --output cli --refresh 1s
```

### Cache Analysis Metrics
- **Cache Hit Rate**: Percentage of queries served from cache
- **Cache Efficiency Score**: Composite metric (0-1) based on hit rate and response times
- **TTL Distribution**: Histogram of TTL values for cached responses
- **Cache Age**: Time since response was cached
- **Server Performance Score**: Per-server cache effectiveness rating

### Hybrid Active/Passive Monitoring
```bash
# Run both active queries and passive capture
sudo ./dns-monitor monitor -c config.yaml --passive --active

# Hybrid monitoring with real-time dashboard
sudo ./dns-monitor monitor -c config.yaml --passive --active --output cli --refresh 1s

# Combine active queries with passive analysis
sudo ./dns-monitor monitor --active-interval 30s --passive-bpf "port 53" --output json
```

### Hybrid Configuration Example
```yaml
# hybrid-config.yaml
mode: hybrid  # Enable both active and passive

# Active monitoring configuration
active:
  enabled: true
  interval: 30s
  servers:
    - address: "8.8.8.8:53"
      protocol: udp
    - address: "1.1.1.1:853"
      protocol: dot

# Passive monitoring configuration  
passive:
  enabled: true
  interfaces: ["any"]  # Monitor all interfaces
  bpf: "port 53 or port 853"
  
# Combined metrics
metrics:
  combine_active_passive: true  # Merge metrics from both sources
  correlation:
    enabled: true              # Correlate active/passive results
    window: 5s                  # Correlation time window
```

### Zabbix Integration
```yaml
# zabbix-config.yaml
metrics:
  export:
    zabbix:
      enabled: true
      server: "zabbix.example.com"
      port: 10051
      hostname: "dns-monitor-01"
      send_interval: 60s
      item_prefix: "dns"
      
      # Batch processing
      batch_size: 100
      max_retries: 3
      retry_interval: 5s
      
      # Item configuration
      items:
        - key: "dns.latency.p95"
          type: float
        - key: "dns.queries.total"
          type: counter
        - key: "dns.cache.hit_rate"
          type: float
```

```bash
# Start with Zabbix export
./dns-monitor monitor -c zabbix-config.yaml

# Test Zabbix connectivity
./dns-monitor test-zabbix -c zabbix-config.yaml
```

## Configuration Guide

### DNS Servers Configuration
```yaml
dns:
  servers:
    # UDP Server
    - name: "Google Primary"
      address: "8.8.8.8"
      port: 53
      protocol: "udp"
      enabled: true
      timeout: 5s
      retries: 3
    
    # TCP Server
    - name: "Google TCP"
      address: "8.8.8.8"
      port: 53
      protocol: "tcp"
      enabled: true
      tcp_keep_alive: true
      tcp_timeout: 10s
    
    # DNS over TLS (DoT)
    - name: "Cloudflare DoT"
      address: "1.1.1.1"
      port: 853
      protocol: "dot"
      enabled: true
      tls:
        server_name: "cloudflare-dns.com"
        insecure_skip_verify: false
        min_version: "TLS1.2"
        cipher_suites: []  # Use default secure ciphers
    
    # DNS over HTTPS (DoH)
    - name: "Cloudflare DoH"
      address: "https://cloudflare-dns.com"
      port: 443
      protocol: "doh"
      doh_endpoint: "/dns-query"
      doh_method: "POST"  # GET or POST
      enabled: true
      headers:
        User-Agent: "DNS-Monitor/1.0"
      http_timeout: 10s

  # Query configuration
  queries:
    types: ["A", "AAAA", "MX", "TXT", "NS", "CNAME", "SOA", "PTR", "SRV", "CAA"]
    domains:
      - "example.com"
      - "google.com"
      - "cloudflare.com"
    timeout: 5s
    retries: 2
    retry_interval: 1s
    exponential_backoff: true
```

### Monitoring Settings
```yaml
monitor:
  # Basic settings
  interval: 30s                  # Query interval
  max_concurrent: 10             # Parallel queries
  alert_threshold: 3             # Consecutive failures before alert
  
  # Advanced settings
  worker_pool_size: 20           # Worker threads for processing
  query_timeout: 10s             # Individual query timeout
  batch_size: 100                # Queries per batch
  
  # Circuit breaker
  circuit_breaker:
    enabled: true
    failure_threshold: 5         # Failures to open circuit
    success_threshold: 2         # Successes to close circuit
    timeout: 30s                 # Circuit reset timeout
  
  # Rate limiting
  rate_limit:
    enabled: true
    queries_per_second: 100     # Max QPS per server
    burst_size: 200             # Burst capacity
  
  # Health checks
  health_check:
    enabled: true
    interval: 60s
    timeout: 5s
    healthy_threshold: 2        # Checks to mark healthy
    unhealthy_threshold: 3      # Checks to mark unhealthy
```

### Cache Analysis Configuration
```yaml
cache:
  enabled: true
  analysis_interval: 30s
  cache_hit_threshold: 10ms
  recursive_servers: []    # Empty = all servers
  ttl_tracking: true
  cache_efficiency_metrics: true
  detection_methods:
    - timing              # Response time analysis
    - ttl                 # TTL decrement tracking
    - pattern             # Response pattern matching
```

### Network Metrics Configuration
```yaml
network:
  enabled: true
  
  # Packet loss detection
  packet_loss_detection: true
  packet_loss_threshold: 0.05    # 5% threshold for alerts
  loss_calculation_window: 5m    # Time window for loss calculation
  min_samples_for_loss: 100      # Minimum samples for accurate calculation
  
  # Jitter analysis
  jitter_calculation: true
  jitter_threshold: 50ms          # Alert threshold
  jitter_window: 100              # Number of samples for jitter calculation
  jitter_percentiles: [50, 95, 99]
  
  # Network quality scoring
  quality_scoring:
    enabled: true
    weights:
      latency: 0.4
      packet_loss: 0.4
      jitter: 0.2
    thresholds:
      excellent: 90
      good: 70
      fair: 50
      poor: 30
  
  # Hop count analysis
  hop_count:
    enabled: true
    ttl_start: 64                # Initial TTL for hop estimation
    traceroute_integration: false # Use actual traceroute
  
  # Interface-specific monitoring
  interface_stats:
    enabled: true
    interfaces: ["eth0", "eth1"]
    metrics:
      - packets_sent
      - packets_received
      - bytes_sent
      - bytes_received
      - errors
      - drops
```

### Adaptive Sampling Configuration
```yaml
sampling:
  enabled: true
  
  # Memory-aware sampling
  memory_aware:
    enabled: true
    max_memory_mb: 100           # Maximum memory for samples
    high_memory_threshold: 0.8   # Reduce sampling at 80% memory
    low_memory_threshold: 0.5    # Increase sampling below 50%
  
  # QPS-based adaptation
  qps_based:
    enabled: true
    thresholds:
      - qps: 100
        sample_rate: 1.0         # 100% sampling below 100 QPS
      - qps: 1000
        sample_rate: 0.1         # 10% sampling at 1000 QPS
      - qps: 10000
        sample_rate: 0.01        # 1% sampling at 10000 QPS
  
  # Sampling configuration
  min_sample_rate: 0.001         # Minimum 0.1% sampling
  max_sample_rate: 1.0           # Maximum 100% sampling
  adjustment_interval: 10s       # Rate adjustment frequency
  
  # Buffer management
  buffer_size: 10000             # Initial buffer size
  max_buffer_size: 100000        # Maximum buffer size
  buffer_cleanup_interval: 60s   # Old sample cleanup
```

### Export Configuration

#### Prometheus
```yaml
metrics:
  export:
    prometheus:
      enabled: true
      port: 9090
      path: "/metrics"
      update_interval: 30s
      metric_prefix: "dns"
```

#### Zabbix
```yaml
metrics:
  export:
    zabbix:
      enabled: true
      server: "zabbix.example.com"
      port: 10051
      hostname: "dns-monitor"
      send_interval: 60s
      item_prefix: "dns"
```

## Command-Line Interface

### Main Commands
```bash
# Monitor command - Start DNS monitoring
dns-monitor monitor [options]

# Version command - Show version information
dns-monitor version

# Test command - Test configuration and connectivity
dns-monitor test -c config.yaml

# Validate command - Validate configuration file
dns-monitor validate -c config.yaml
```

### Monitor Command Options
```bash
# Configuration
  -c, --config string         Configuration file path (default "config.yaml")
  -m, --minimal              Use minimal configuration template
  
# Server configuration
  -s, --servers string       Comma-separated DNS servers (e.g., "8.8.8.8:53,1.1.1.1:53")
  -p, --protocol string      DNS protocol: udp, tcp, dot, doh (default "udp")
  --tls-skip-verify         Skip TLS certificate verification (DoT/DoH)
  
# Query configuration  
  -d, --domains string       Comma-separated domains to query
  -t, --query-types string  Query types (e.g., "A,AAAA,MX")
  --timeout duration        Query timeout (default 5s)
  --retries int            Number of retries (default 2)
  
# Monitoring modes
  --active                  Enable active monitoring (default true)
  --passive                 Enable passive monitoring (requires root)
  --hybrid                  Enable both active and passive monitoring
  
# Passive monitoring options
  -i, --interface string    Network interface for capture (default "any")
  -b, --bpf string         BPF filter expression (default "port 53")
  --tcp-reassembly         Enable TCP stream reassembly
  --query-matching         Enable query/response matching
  
# Output options
  -o, --output string      Output format: json, text, csv, cli (default "cli")
  --color                  Enable colored output (CLI mode)
  --refresh duration       CLI refresh interval (default 5s)
  --compact               Compact CLI display mode
  
# Export options
  --prometheus            Enable Prometheus exporter
  --prometheus-port int   Prometheus port (default 9090)
  --zabbix               Enable Zabbix sender
  --zabbix-server string  Zabbix server address
  
# Performance options
  -w, --workers int        Number of worker threads (default 10)
  --max-concurrent int    Maximum concurrent queries (default 10)
  --buffer-size int       Result buffer size (default 10000)
  
# Monitoring options
  --interval duration     Query interval (default 30s)
  --window duration      Metrics calculation window (default 5m)
  
# Feature flags
  --enable-cache         Enable cache analysis
  --enable-network       Enable network metrics
  --enable-sampling      Enable adaptive sampling
  --enable-all          Enable all features
  
# Debugging
  -v, --verbose         Enable verbose logging
  --debug              Enable debug mode
  --trace              Enable trace logging
  --dry-run            Validate config without monitoring
  
# Help
  -h, --help           Show help message
  --version           Show version information
```

### Usage Examples
```bash
# Basic monitoring with CLI output
dns-monitor monitor -c config.yaml --output cli --color

# Quick test with minimal config
dns-monitor monitor --minimal -s "8.8.8.8:53" -d "google.com"

# Passive monitoring with filters
sudo dns-monitor monitor --passive -i eth0 -b "port 53 and host 8.8.8.8"

# Full-featured monitoring
dns-monitor monitor -c config.yaml --enable-all --prometheus --verbose

# Test configuration
dns-monitor test -c config.yaml

# Validate and show parsed config
dns-monitor validate -c config.yaml --show-parsed
```

### Environment Variables
```bash
# Configuration
DNS_MONITOR_CONFIG=/path/to/config.yaml
DNS_MONITOR_CONFIG_DIR=/etc/dns-monitor

# Logging
DNS_MONITOR_LOG_LEVEL=debug|info|warn|error
DNS_MONITOR_LOG_FILE=/var/log/dns-monitor.log
DNS_MONITOR_LOG_FORMAT=json|text

# Prometheus
DNS_MONITOR_PROMETHEUS_ENABLED=true
DNS_MONITOR_PROMETHEUS_PORT=9090
DNS_MONITOR_PROMETHEUS_PATH=/metrics
DNS_MONITOR_PROMETHEUS_AUTH_USER=prometheus
DNS_MONITOR_PROMETHEUS_AUTH_PASS=secure-password

# Zabbix
DNS_MONITOR_ZABBIX_ENABLED=true
DNS_MONITOR_ZABBIX_SERVER=zabbix.example.com
DNS_MONITOR_ZABBIX_PORT=10051
DNS_MONITOR_ZABBIX_HOSTNAME=dns-monitor-01

# Performance
DNS_MONITOR_MAX_WORKERS=20
DNS_MONITOR_MAX_MEMORY_MB=500
DNS_MONITOR_BUFFER_SIZE=50000

# Network
DNS_MONITOR_INTERFACE=eth0
DNS_MONITOR_BPF_FILTER="port 53"

# Features
DNS_MONITOR_ENABLE_CACHE=true
DNS_MONITOR_ENABLE_NETWORK=true
DNS_MONITOR_ENABLE_SAMPLING=true
```

## Metrics and Monitoring

### Core Metrics (Always Available)

#### Latency Metrics
- `dns_latency_p50_seconds{server,protocol}`: 50th percentile latency
- `dns_latency_p95_seconds{server,protocol}`: 95th percentile latency  
- `dns_latency_p99_seconds{server,protocol}`: 99th percentile latency
- `dns_latency_p999_seconds{server,protocol}`: 99.9th percentile latency
- `dns_latency_min_seconds{server,protocol}`: Minimum latency
- `dns_latency_max_seconds{server,protocol}`: Maximum latency
- `dns_latency_mean_seconds{server,protocol}`: Mean latency

#### Rate Metrics
- `dns_queries_total{server,protocol,type,rcode}`: Total query count by dimensions
- `dns_success_rate_ratio{server,protocol}`: Success rate (0-1)
- `dns_error_rate_ratio{server,protocol}`: Error rate (0-1)
- `dns_queries_per_second{server,protocol}`: Current QPS
- `dns_queries_per_second_avg`: Average QPS across all servers
- `dns_queries_per_second_peak`: Peak QPS observed

#### Response Code Metrics
- `dns_response_code_total{rcode}`: Count by response code
- `dns_response_code_noerror_total`: Successful responses
- `dns_response_code_nxdomain_total`: Non-existent domain responses
- `dns_response_code_servfail_total`: Server failure responses
- `dns_response_code_refused_total`: Query refused responses

#### Query Type Metrics
- `dns_query_type_total{type}`: Count by query type
- `dns_query_type_distribution{type}`: Percentage distribution

### Optional Metric Groups

#### Cache Metrics (enable_cache_metrics: true)
- `dns_cache_hit_rate_ratio{server}`: Cache hit rate per server
- `dns_cache_efficiency_score{server}`: Cache efficiency (0-1)
- `dns_cache_ttl_seconds{server,percentile}`: TTL distribution
- `dns_cache_age_seconds{server,percentile}`: Cache age distribution
- `dns_cache_hits_total{server}`: Total cache hits
- `dns_cache_misses_total{server}`: Total cache misses
- `dns_cache_expired_total{server}`: Expired cache entries
- `dns_cache_size_bytes{server}`: Estimated cache size
- `dns_cache_evictions_total{server}`: Cache evictions
- `dns_cache_refresh_total{server}`: Cache refreshes

#### Network Metrics (enable_network_metrics: true)
- `dns_packet_loss_ratio{server,protocol}`: Packet loss rate
- `dns_packet_loss_consecutive{server}`: Consecutive losses
- `dns_network_latency_seconds{server,percentile}`: Network latency
- `dns_network_quality_score{server}`: Quality score (0-100)
- `dns_network_hop_count{server}`: Estimated hop count
- `dns_network_rtt_seconds{server}`: Round-trip time
- `dns_network_timeout_total{server}`: Network timeouts
- `dns_network_retransmits_total{server}`: Retransmissions
- `dns_interface_packets_sent{interface}`: Packets sent per interface
- `dns_interface_packets_received{interface}`: Packets received
- `dns_interface_bytes_sent{interface}`: Bytes sent
- `dns_interface_bytes_received{interface}`: Bytes received
- `dns_interface_errors{interface}`: Interface errors
- `dns_interface_drops{interface}`: Packet drops

#### Jitter Metrics (enable_jitter_metrics: true)
- `dns_jitter_seconds{server,percentile}`: Network jitter percentiles
- `dns_jitter_mean_seconds{server}`: Mean jitter
- `dns_jitter_stddev_seconds{server}`: Jitter standard deviation
- `dns_jitter_coefficient_variation{server}`: Jitter CV

#### TTL Metrics (enable_ttl_metrics: true)
- `dns_ttl_distribution{server,bucket}`: TTL value distribution
- `dns_ttl_min_seconds{server,type}`: Minimum TTL by type
- `dns_ttl_max_seconds{server,type}`: Maximum TTL by type

#### Monitoring Health Metrics (enable_monitoring_metrics: true)
- `dns_monitor_up`: Monitor status (1=up, 0=down)
- `dns_monitor_workers_active`: Active worker threads
- `dns_monitor_memory_bytes`: Memory usage
- `dns_monitor_goroutines`: Number of goroutines
- `dns_monitor_buffer_size`: Current buffer size
- `dns_monitor_buffer_usage_ratio`: Buffer utilization
- `dns_monitor_errors_total{type}`: Monitor errors by type
- `dns_monitor_restarts_total`: Monitor restart count
- `dns_monitor_uptime_seconds`: Monitor uptime

#### Sampling Metrics (When Adaptive Sampling Enabled)
- `dns_sampling_rate{server}`: Current sampling rate
- `dns_sampling_samples_total`: Total samples collected
- `dns_sampling_drops_total`: Dropped samples
- `dns_sampling_memory_bytes`: Memory used by samples
- `dns_sampling_adjustment_total`: Sample rate adjustments

### Interpreting Metrics

#### Latency Thresholds
- **< 10ms**: Excellent (likely cached or local)
- **10-50ms**: Good performance
- **50-100ms**: Acceptable for most applications
- **100-300ms**: Noticeable delay
- **> 300ms**: Poor performance

#### Cache Hit Rate
- **> 90%**: Exceptional caching (recursive resolver)
- **80-90%**: Excellent caching performance
- **60-80%**: Good, typical for mixed workloads
- **40-60%**: Fair, room for improvement
- **< 40%**: Poor cache utilization

#### Network Quality Score
- **95-100**: Enterprise-grade network
- **90-95**: Excellent network conditions
- **80-90**: Good network performance
- **70-80**: Acceptable with minor issues
- **50-70**: Fair, occasional problems
- **< 50**: Poor network quality

#### Packet Loss Thresholds
- **0%**: Perfect connectivity
- **< 0.1%**: Excellent
- **0.1-1%**: Very good
- **1-2.5%**: Acceptable for most applications
- **2.5-5%**: Noticeable impact
- **5-10%**: Significant issues
- **> 10%**: Critical problems

#### Jitter Thresholds
- **< 5ms**: Excellent stability
- **5-20ms**: Good for most applications
- **20-50ms**: May affect real-time applications
- **50-100ms**: Noticeable instability
- **> 100ms**: Severe network instability

#### QPS (Queries Per Second) Capacity
- **< 10 QPS**: Light load
- **10-100 QPS**: Moderate load
- **100-1000 QPS**: Heavy load
- **1000-10000 QPS**: Very heavy load
- **> 10000 QPS**: Extreme load (requires optimization)

## Troubleshooting

### Common Issues

#### Permission Denied for Passive Monitoring
```bash
# Linux: Grant CAP_NET_RAW capability
sudo setcap cap_net_raw+ep ./dns-monitor

# Or run as root
sudo ./dns-monitor -passive

# For systemd service
# Add to service file:
AmbientCapabilities=CAP_NET_RAW
```

#### High CPU Usage
```yaml
# Reduce calculation frequency
metrics:
  calculation_interval: 30s    # Increase interval
  max_stored_results: 10000    # Reduce buffer size
```

#### Prometheus Metrics Not Updating
```bash
# Check Prometheus endpoint
curl http://localhost:9090/metrics

# Verify scrape configuration
# Check Prometheus targets page
```

#### DNS Timeout Issues
```yaml
# Increase timeouts
dns:
  queries:
    timeout: 10s      # Increase from 5s
    retries: 5        # Increase retry count
```

### Debug Mode
```bash
# Enable debug logging
DNS_MONITOR_LOG_LEVEL=debug ./dns-monitor -verbose

# Trace network issues
./dns-monitor -config config.yaml -verbose -trace
```

## Performance Tuning

### For High-Volume Environments (>1000 QPS)
```yaml
# High-performance configuration
monitor:
  max_concurrent: 100           # High parallelism
  worker_pool_size: 50          # Large worker pool
  interval: 5s                  # Frequent monitoring
  batch_size: 500               # Large batches

metrics:
  max_stored_results: 1000000   # 1M result buffer
  window_duration: 60m          # Long analysis window
  calculation_interval: 30s     # Less frequent calculations

passive:
  workers: 32                   # Many packet workers
  buffer_size: 100000           # Large packet buffer
  tcp_reassembly:
    max_streams: 10000          # Handle many TCP streams
    max_total_memory: 104857600 # 100MB for reassembly

sampling:
  enabled: true                 # Essential for high volume
  memory_aware:
    max_memory_mb: 500          # Allow more memory
  qps_based:
    thresholds:
      - qps: 1000
        sample_rate: 0.1        # 10% at 1000 QPS
      - qps: 10000
        sample_rate: 0.01       # 1% at 10000 QPS

# Resource limits
resources:
  max_memory_mb: 2048           # 2GB limit
  max_goroutines: 1000          # Goroutine limit
  gc_interval: 60s              # Force GC regularly
```

### For Low-Resource Systems (Raspberry Pi, etc.)
```yaml
# Minimal resource configuration
monitor:
  max_concurrent: 2             # Minimal parallelism
  worker_pool_size: 2           # Small worker pool
  interval: 60s                 # Infrequent queries
  batch_size: 10                # Small batches

metrics:
  enabled: true
  max_stored_results: 1000      # Small buffer
  window_duration: 5m           # Short window
  calculate_percentiles: false  # Skip percentile calculations
  
cache:
  enabled: false                # Disable cache analysis
  
network:
  enabled: false                # Disable network metrics

passive:
  enabled: false                # Disable passive monitoring

sampling:
  enabled: false                # No sampling

# Strict resource limits
resources:
  max_memory_mb: 128            # 128MB limit
  max_goroutines: 50            # Low goroutine count
```

### For Real-Time Analysis
```yaml
# Real-time monitoring configuration
output:
  cli:
    refresh_interval: 1s        # 1 second refresh
    detailed_view: true         # Show all metrics
    show_distributions: true    # Include histograms
    show_sparklines: true       # Visual trends
    terminal_width: auto        # Auto-detect width

metrics:
  calculation_interval: 1s      # Frequent updates
  real_time_percentiles: true   # Live percentile updates
  sliding_window: true          # Use sliding windows

monitor:
  interval: 5s                  # Frequent queries
  real_time_mode: true          # Optimize for real-time

# Stream processing
streaming:
  enabled: true
  buffer_size: 100              # Small buffer for low latency
  flush_interval: 100ms         # Frequent flushes
```

### For Long-Term Monitoring
```yaml
# Long-term stability configuration
monitor:
  interval: 60s                 # Conservative interval
  max_concurrent: 10            # Moderate parallelism

metrics:
  window_duration: 24h          # Daily windows
  max_stored_results: 86400     # 24 hours of per-second data
  archive:
    enabled: true
    interval: 1h                # Hourly archives
    retention: 30d              # 30 day retention

# Memory management
memory:
  cleanup_interval: 1h          # Regular cleanup
  compact_interval: 6h          # Periodic compaction
  max_age: 7d                   # Maximum data age

# Stability features
stability:
  auto_restart: true            # Auto-restart on failure
  health_check_interval: 5m     # Regular health checks
  memory_limit_action: gc       # Force GC on memory pressure
```

### Optimization Tips

#### CPU Optimization
- Use `GOMAXPROCS` environment variable to control CPU cores
- Enable sampling for high QPS scenarios
- Adjust worker pool sizes based on CPU count
- Use batch processing for bulk operations

#### Memory Optimization
- Enable adaptive sampling to control memory usage
- Use circular buffers with appropriate sizes
- Configure cleanup intervals for long-running instances
- Set memory limits to prevent OOM

#### Network Optimization
- Use connection pooling for TCP/DoT/DoH
- Enable keep-alive for persistent connections
- Adjust timeout values based on network latency
- Use appropriate buffer sizes for packet capture

#### Storage Optimization
- Disable unnecessary metric groups
- Use appropriate retention periods
- Enable compression for archived data
- Implement log rotation for output files

## Security Considerations

### Passive Monitoring Privileges
- Requires CAP_NET_RAW on Linux or root access
- Can capture all network traffic on interface
- Use BPF filters to limit capture scope

### DNS-over-TLS/HTTPS
```yaml
# Verify certificates
dns:
  servers:
    - protocol: "dot"
      insecure_skip_verify: false    # Always verify in production
```

### Sensitive Data
- DNS queries may contain sensitive information
- Use appropriate access controls on logs and metrics
- Consider data retention policies

### Network Access
- Firewall rules may be needed for DNS servers
- Prometheus/Zabbix ports need to be secured
- Use TLS for metric export endpoints when possible

## Project Structure

```
dns-monitoring/
├── cmd/
│   └── monitor/           # Main application entry point
├── internal/
│   ├── config/           # Configuration management
│   │   ├── config.go
│   │   ├── config_test.go
│   │   └── validation.go
│   ├── dns/              # DNS engine and protocols
│   │   ├── active.go     # Active monitoring
│   │   ├── passive.go    # Passive monitoring
│   │   ├── cache_analysis.go
│   │   ├── network_analysis.go
│   │   ├── packet_capture.go
│   │   ├── packet_parser.go
│   │   ├── query_matcher.go
│   │   └── types.go
│   ├── metrics/          # Metrics calculation
│   │   ├── collector.go
│   │   ├── percentiles.go
│   │   ├── rates.go
│   │   ├── throughput.go
│   │   └── types.go
│   └── exporters/        # Output formatters
│       ├── cli.go
│       ├── layout.go
│       ├── prometheus.go
│       ├── prometheus_metrics.go
│       ├── prometheus_server.go
│       ├── prometheus_updater.go
│       ├── zabbix.go
│       ├── zabbix_batch.go
│       └── zabbix_items.go
├── config.yaml           # Default configuration
├── go.mod               # Go dependencies
├── go.sum               # Dependency checksums
└── README.md            # This file
```

## Current Features Status

### Implemented ✅
- **Core DNS Engine**: Active monitoring with UDP, TCP, DoT, DoH support
- **Passive Monitoring**: Packet capture and analysis with BPF filtering
- **Metrics Collection**: Comprehensive metrics with percentile calculations
- **Prometheus Export**: Full metrics export with customizable labels
- **Zabbix Export**: Enterprise monitoring integration
- **CLI Display**: Real-time dashboard with color-coded output
- **Cache Analysis**: Multi-method cache detection and efficiency scoring
- **Network Metrics**: Packet loss, jitter, and quality analysis
- **Configuration Validation**: Comprehensive validation with actionable errors

### In Progress 🚧
- Unit test coverage
- Integration tests
- Docker compose examples
- Kubernetes deployment manifests

### Planned 📋
- Grafana dashboard templates
- Alert manager integration
- Historical data storage
- REST API for metrics access
- Web UI dashboard

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

### Development Setup
```bash
# Clone repository
git clone https://github.com/randax/dns-monitoring.git

# Install development dependencies
go mod download

# Run tests
go test ./...

# Run with race detector
go run -race ./cmd/monitor

# Build for different platforms
GOOS=linux GOARCH=amd64 go build -o dns-monitor-linux
GOOS=darwin GOARCH=amd64 go build -o dns-monitor-mac
GOOS=windows GOARCH=amd64 go build -o dns-monitor.exe
```

## License

Apache License 2.0 - See LICENSE file for details

## Support

For issues, questions, or contributions, please visit:
- GitHub Issues: https://github.com/randax/dns-monitoring/issues
- Documentation: https://github.com/randax/dns-monitoring/wiki

## Acknowledgments

- Built with Go and love for DNS
- Uses miekg/dns for DNS protocol handling
- Prometheus client library for metrics export
- Google's gopacket for packet capture
- Cobra for CLI framework
- Viper for configuration management