# Prometheus Exporter Documentation

The DNS Monitoring tool includes a built-in Prometheus exporter for exposing DNS metrics in a format compatible with Prometheus monitoring system.

## Overview

The Prometheus exporter provides comprehensive DNS monitoring metrics including:
- Query latency (percentiles and histograms)
- Success/error rates
- Query throughput
- Response code distribution
- Query type distribution
- Cache metrics
- Network interface statistics

## Configuration

### Basic Configuration

Add the following to your `config.yaml`:

```yaml
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

### Advanced Configuration

```yaml
metrics:
  enabled: true
  window_duration: 15m
  calculation_interval: 10s
  export:
    prometheus:
      enabled: true
      port: 9090
      path: "/metrics"
      update_interval: 30s
      include_server_labels: true
      include_protocol_labels: true
      metric_prefix: "dns"
      enable_latency_sampling: false
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | true | Enable Prometheus exporter |
| `port` | integer | 9090 | HTTP port for metrics endpoint |
| `path` | string | /metrics | URL path for metrics endpoint |
| `update_interval` | duration | 30s | How often to update metrics |
| `include_server_labels` | boolean | true | Add server label to metrics |
| `include_protocol_labels` | boolean | true | Add protocol label to metrics |
| `metric_prefix` | string | dns | Prefix for all metric names |
| `enable_latency_sampling` | boolean | false | Enable histogram/summary metrics (higher overhead) |

## Metrics Exposed

### Latency Metrics

#### Percentile Gauges (Always Available)
- `dns_latency_p50_seconds` - 50th percentile latency
- `dns_latency_p95_seconds` - 95th percentile latency
- `dns_latency_p99_seconds` - 99th percentile latency
- `dns_latency_p999_seconds` - 99.9th percentile latency
- `dns_latency_mean_seconds` - Mean latency
- `dns_latency_min_seconds` - Minimum latency
- `dns_latency_max_seconds` - Maximum latency

#### Histogram/Summary (When `enable_latency_sampling: true`)
- `dns_query_duration_seconds` - Histogram of query durations
- `dns_query_duration_summary` - Summary with quantiles

### Counter Metrics

- `dns_queries_total` - Total number of DNS queries
- `dns_success_total` - Total successful queries
- `dns_error_total{error_type}` - Total failed queries by error type
- `dns_response_code_total{code}` - Queries by response code
- `dns_query_type_total{type}` - Queries by query type

### Rate Metrics

- `dns_query_rate` - Current queries per second
- `dns_success_rate` - Successful queries per second
- `dns_error_rate` - Failed queries per second

### Throughput Metrics

- `dns_throughput_sent_bytes_per_second` - Bytes sent per second
- `dns_throughput_received_bytes_per_second` - Bytes received per second

### Cache Metrics (When cache analysis is enabled)

- `dns_cache_hits_total` - Total cache hits detected
- `dns_cacheable_queries_total` - Total cacheable queries
- `dns_cache_hit_ratio` - Cache hit ratio

### Network Metrics (When network analysis is enabled)

- `dns_network_packets_sent_total{interface}` - Packets sent per interface
- `dns_network_packets_received_total{interface}` - Packets received per interface
- `dns_network_packet_loss_ratio{interface}` - Packet loss ratio
- `dns_network_jitter_seconds{interface}` - Network jitter

## Usage Examples

### Starting the Monitor with Prometheus Export

```bash
# Start monitoring with Prometheus exporter on default port 9090
dns-monitor monitor -k -c config.yaml

# The metrics will be available at http://localhost:9090/metrics
```

### Prometheus Configuration

Add to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'dns-monitor'
    static_configs:
      - targets: ['localhost:9090']
    scrape_interval: 30s
```

### Example Prometheus Queries

#### Average Query Latency
```promql
dns_latency_mean_seconds
```

#### Query Rate by Server
```promql
rate(dns_queries_total[5m])
```

#### Error Rate
```promql
rate(dns_error_total[5m])
```

#### 95th Percentile Latency
```promql
dns_latency_p95_seconds
```

#### Success Rate Percentage
```promql
rate(dns_success_total[5m]) / rate(dns_queries_total[5m]) * 100
```

#### Response Code Distribution
```promql
sum by (code) (rate(dns_response_code_total[5m]))
```

### Grafana Dashboard

You can create a Grafana dashboard with panels for:

1. **Query Performance**
   - P50, P95, P99 latency gauges
   - Mean latency over time
   - Min/Max latency range

2. **Query Volume**
   - Total queries per second
   - Success vs Error rate
   - Query type distribution

3. **Response Codes**
   - NOERROR, NXDOMAIN, SERVFAIL distribution
   - Error types breakdown

4. **Network Statistics** (if enabled)
   - Packet throughput
   - Packet loss percentage
   - Network jitter

## Performance Considerations

### Latency Sampling

The `enable_latency_sampling` option controls whether individual latency samples are tracked:

- **When `false` (default)**: Only percentile gauges are updated, lower overhead
- **When `true`**: Full histogram/summary metrics enabled, higher memory usage

For most use cases, the percentile gauges provide sufficient detail with minimal overhead.

### Update Interval

The `update_interval` setting controls how often metrics are recalculated:

- Shorter intervals (< 10s): More responsive metrics, higher CPU usage
- Longer intervals (> 30s): Lower overhead, less responsive to changes
- Recommended: 30s for production environments

### Label Cardinality

Be cautious with labels to avoid high cardinality:

- `include_server_labels`: Adds a label per DNS server
- `include_protocol_labels`: Adds labels for UDP, TCP, DoT, DoH
- High cardinality can impact Prometheus performance

## Troubleshooting

### Metrics Not Updating

1. Check the exporter is enabled:
   ```yaml
   metrics:
     export:
       prometheus:
         enabled: true
   ```

2. Verify the port is not in use:
   ```bash
   lsof -i :9090
   ```

3. Check application logs for errors:
   ```bash
   dns-monitor monitor -k -c config.yaml 2>&1 | grep -i prometheus
   ```

### High Memory Usage

If experiencing high memory usage:

1. Disable latency sampling:
   ```yaml
   enable_latency_sampling: false
   ```

2. Increase update interval:
   ```yaml
   update_interval: 60s
   ```

3. Reduce metric window:
   ```yaml
   metrics:
     window_duration: 5m
   ```

### Port Conflicts

If port 9090 is in use, change to another port:

```yaml
prometheus:
  port: 9091  # Or any available port
```

## Integration with Alerting

### Example Prometheus Alert Rules

```yaml
groups:
  - name: dns_alerts
    rules:
      - alert: HighDNSLatency
        expr: dns_latency_p95_seconds > 0.5
        for: 5m
        annotations:
          summary: "High DNS query latency detected"
          description: "95th percentile latency is {{ $value }}s"
      
      - alert: HighDNSErrorRate
        expr: rate(dns_error_total[5m]) > 0.1
        for: 5m
        annotations:
          summary: "High DNS error rate"
          description: "Error rate is {{ $value }} queries/sec"
      
      - alert: DNSServerDown
        expr: up{job="dns-monitor"} == 0
        for: 1m
        annotations:
          summary: "DNS monitor is down"
```

## Best Practices

1. **Start with default settings** - The defaults are optimized for most use cases
2. **Monitor resource usage** - Adjust settings based on your environment
3. **Use appropriate scrape intervals** - Match Prometheus scrape_interval to update_interval
4. **Leverage percentile gauges** - They provide good insights with low overhead
5. **Set up alerts** - Configure Prometheus alerts for critical thresholds
6. **Use Grafana** - Visualize metrics for better insights
7. **Regular maintenance** - Periodically review and optimize metric collection

## Security Considerations

1. **Bind to localhost** - If metrics don't need external access:
   ```yaml
   prometheus:
     port: 9090
     # Bind to localhost only (requires code modification)
   ```

2. **Use reverse proxy** - Place behind nginx/Apache for authentication:
   ```nginx
   location /metrics {
       proxy_pass http://localhost:9090/metrics;
       auth_basic "Prometheus Metrics";
       auth_basic_user_file /etc/nginx/.htpasswd;
   }
   ```

3. **Network isolation** - Run on internal network only
4. **Monitor access logs** - Track who accesses metrics endpoint