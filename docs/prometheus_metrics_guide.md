# Prometheus Metrics Guide

## Overview

The DNS Monitoring tool exports metrics in Prometheus format with a prioritized metric system that focuses on core DNS metrics while making advanced metrics optional to reduce memory overhead.

## Metric Categories

### Core Metrics (Always Enabled)

These metrics are essential for basic DNS monitoring and are always available:

#### Latency Metrics
- `dns_latency_p50_seconds` - 50th percentile latency
- `dns_latency_p95_seconds` - 95th percentile latency  
- `dns_latency_p99_seconds` - 99th percentile latency
- `dns_latency_p999_seconds` - 99.9th percentile latency

#### Rate Metrics
- `dns_queries_per_second` - Current QPS
- `dns_qps_average` - Average QPS over time window
- `dns_qps_peak` - Peak QPS observed
- `dns_success_rate_ratio` - Query success rate (0-1)
- `dns_error_rate_ratio` - Query error rate (0-1)
- `dns_timeout_rate_ratio` - Query timeout rate (0-1)

#### Counter Metrics
- `dns_queries_total` - Total number of queries
- `dns_queries_success_total` - Total successful queries
- `dns_queries_error_total{error_type}` - Total errors by type
- `dns_response_codes_total{response_code}` - Responses by code
- `dns_query_types_total{query_type}` - Queries by type (A, AAAA, etc.)

#### Active Monitoring
- `dns_active_queries` - Currently active queries

### Advanced Metrics (Optional)

These metrics provide deeper insights but consume additional memory. Enable them based on your monitoring needs.

#### Cache Metrics (`enable_cache_metrics: true`)
- `dns_cache_hit_rate_ratio` - Cache hit rate (0-1)
- `dns_cache_miss_rate_ratio` - Cache miss rate (0-1)
- `dns_cache_efficiency_score` - Cache efficiency score (0-1)
- `dns_cache_stale_rate_ratio` - Stale entry rate (0-1)
- `dns_cache_avg_ttl_seconds` - Average cache TTL
- `dns_cache_queries_total` - Total cacheable queries
- `dns_cache_ttl_seconds` - TTL distribution histogram
- `dns_cache_age_seconds` - Cache age distribution histogram

#### Network Metrics (`enable_network_metrics: true`)
- `dns_packet_loss_ratio` - Packet loss rate (0-1)
- `dns_network_quality_score` - Network quality (0-100)
- `dns_network_error_rate_ratio` - Network error rate (0-1)
- `dns_connection_error_rate_ratio` - Connection errors (0-1)
- `dns_network_latency_p50_seconds` - Network latency P50
- `dns_network_latency_p95_seconds` - Network latency P95
- `dns_network_latency_p99_seconds` - Network latency P99
- `dns_network_packets_sent_total{interface}` - Packets sent per interface
- `dns_network_packets_received_total{interface}` - Packets received per interface
- `dns_interface_packet_loss_ratio{interface}` - Loss per interface

#### Jitter Metrics (`enable_jitter_metrics: true`)
- `dns_jitter_p50_seconds` - Jitter 50th percentile
- `dns_jitter_p95_seconds` - Jitter 95th percentile
- `dns_jitter_p99_seconds` - Jitter 99th percentile
- `dns_jitter_average_seconds` - Average jitter
- `dns_jitter_seconds` - Jitter distribution histogram

#### TTL Metrics (`enable_ttl_metrics: true`)
- `dns_ttl_average_seconds` - Average record TTL
- `dns_ttl_min_seconds` - Minimum TTL observed
- `dns_ttl_max_seconds` - Maximum TTL observed

#### Monitoring Health Metrics (`enable_monitoring_metrics: true`)
- `dns_monitor_uptime_seconds` - Monitor uptime
- `dns_monitor_health_score` - Health score (0-100)
- `dns_monitor_memory_usage_bytes` - Memory usage
- `dns_monitor_cpu_usage_percent` - CPU usage percentage
- `dns_monitor_goroutines` - Active goroutines
- `dns_monitor_buffer_usage_ratio` - Buffer usage (0-1)
- `dns_monitor_dropped_queries_total` - Dropped queries
- `dns_monitoring_update_errors_total` - Update errors
- `dns_monitoring_validation_errors_total` - Validation errors

## Configuration

### Minimal Configuration (Core Metrics Only)

```yaml
prometheus:
  enabled: true
  port: 9090
  path: "/metrics"
  update_interval: 30s
  metric_prefix: "dns"
  # All advanced features disabled by default
```

### Cache Analysis Configuration

```yaml
prometheus:
  metric_features:
    enable_cache_metrics: true    # Enable cache performance metrics
    enable_ttl_metrics: true       # Often used with cache metrics
```

### Network Troubleshooting Configuration

```yaml
prometheus:
  metric_features:
    enable_network_metrics: true  # Network quality and packet metrics
    enable_jitter_metrics: true   # Jitter analysis for stability
```

### Full Monitoring Configuration

```yaml
prometheus:
  metric_features:
    enable_cache_metrics: true
    enable_network_metrics: true
    enable_jitter_metrics: true
    enable_ttl_metrics: true
    enable_monitoring_metrics: true
```

## Memory Considerations

### Core Metrics Only
- ~100 metric series total
- Memory usage: ~1-2 MB
- Suitable for: Basic DNS monitoring, high-cardinality environments

### With Cache Metrics
- Adds ~10-15 metric series
- Additional memory: ~0.5 MB
- Use case: Recursive resolver monitoring, cache optimization

### With Network Metrics
- Adds ~15-20 metric series (more with multiple interfaces)
- Additional memory: ~1 MB
- Use case: Network troubleshooting, multi-interface systems

### With All Features
- ~150-200 metric series total
- Memory usage: ~3-5 MB
- Use case: Comprehensive monitoring, debugging

## Best Practices

1. **Start with Core Metrics**: Begin with the default configuration and enable advanced metrics as needed.

2. **Enable Features Selectively**: Only enable the metric groups relevant to your monitoring needs.

3. **Monitor Memory Usage**: Use `enable_monitoring_metrics` temporarily to track the tool's resource usage.

4. **Use Labels Wisely**: 
   - `include_server_labels`: Enable for multi-server monitoring
   - `include_protocol_labels`: Enable when comparing UDP/TCP/DoT/DoH

5. **Histogram/Summary Metrics**: Only enable `enable_latency_sampling` if you need:
   - `histogram_quantile()` calculations in PromQL
   - Heatmaps in Grafana
   - Custom quantile aggregations

## Example Prometheus Queries

### Core Metrics Queries

```promql
# Current QPS
dns_queries_per_second

# Success rate over time
rate(dns_queries_success_total[5m]) / rate(dns_queries_total[5m])

# P95 latency trend
dns_latency_p95_seconds

# Error rate by type
rate(dns_queries_error_total[5m]) by (error_type)
```

### Advanced Metrics Queries

```promql
# Cache hit rate (requires enable_cache_metrics)
dns_cache_hit_rate_ratio

# Network packet loss by interface (requires enable_network_metrics)
dns_interface_packet_loss_ratio by (interface)

# Jitter stability (requires enable_jitter_metrics)
dns_jitter_p99_seconds / dns_jitter_p50_seconds

# TTL distribution (requires enable_ttl_metrics)
dns_ttl_average_seconds
```

## Migration from Previous Versions

If upgrading from a version without feature flags, all metrics were previously enabled by default. To maintain the same behavior:

```yaml
prometheus:
  metric_features:
    enable_cache_metrics: true
    enable_network_metrics: true
    enable_jitter_metrics: true
    enable_ttl_metrics: true
    enable_monitoring_metrics: true
```

To optimize for lower memory usage, keep the default configuration which only enables core metrics.