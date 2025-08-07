# DNS Monitoring Tool

A command-line tool for monitoring DNS servers and tracking DNS query performance.

## Overview

DNS Monitoring is a Go-based CLI application that provides comprehensive monitoring capabilities for DNS servers. It can track query response times, server availability, and DNS resolution patterns across multiple DNS servers simultaneously.

## Features (Planned)

- Monitor multiple DNS servers concurrently
- Support for various DNS record types (A, AAAA, MX, TXT, NS, CNAME)
- Configurable monitoring intervals and timeouts
- Multiple output formats (JSON, Text, CSV)
- Metrics export for monitoring systems
- Alert thresholds for failure detection
- Retry logic for transient failures

## Installation

### Prerequisites

- Go 1.21 or higher

### Build from Source

```bash
git clone https://github.com/randax/dns-monitoring.git
cd dns-monitoring
go build -o dns-monitoring cmd/dns-monitoring/main.go
```

## Usage

### Basic Commands

```bash
# Display help
./dns-monitoring --help

# Show version information
./dns-monitoring version

# Start monitoring with default configuration
./dns-monitoring monitor

# Use custom configuration file
./dns-monitoring monitor --config /path/to/config.yaml

# Enable verbose output
./dns-monitoring monitor --verbose
```

## Configuration

The tool uses a YAML configuration file to define monitoring parameters. By default, it looks for `config.yaml` in the current directory.

### Configuration Structure

```yaml
dns:
  servers:           # List of DNS servers to monitor
  queries:           # Query types and domains to test
    
monitor:
  interval:          # Monitoring cycle interval
  max_concurrent:    # Maximum concurrent queries
  alert_threshold:   # Consecutive failures before alerting

output:
  format:           # Output format (json/text/csv)
  file:             # Log file path
  console:          # Enable console output
```

See `config.yaml` for a complete example configuration.

### DNS Servers

Configure multiple DNS servers with custom names, addresses, and ports:

```yaml
dns:
  servers:
    - name: "Google DNS"
      address: "8.8.8.8"
      port: 53
      enabled: true
```

### Query Configuration

Define which DNS record types and domains to monitor:

```yaml
dns:
  queries:
    types: ["A", "AAAA", "MX"]
    domains: ["example.com", "google.com"]
    timeout: 5s
    retries: 3
```

## Project Structure

```
dns-monitoring/
├── cmd/
│   └── dns-monitoring/    # Main application entry point
├── internal/
│   ├── cmd/              # CLI command implementations
│   └── config/           # Configuration management
├── pkg/                  # Public libraries (future)
├── config.yaml           # Sample configuration
├── go.mod               # Go module definition
└── README.md            # This file
```

## Development Status

This project is in its initial setup phase (Phase 1). The basic project structure, CLI framework, and configuration system have been implemented. DNS monitoring functionality will be added in subsequent phases.

### Completed
- ✅ Go project structure
- ✅ Cobra CLI framework integration
- ✅ YAML configuration support
- ✅ Basic command structure

### Upcoming
- ⏳ DNS query implementation
- ⏳ Monitoring engine
- ⏳ Output formatters
- ⏳ Metrics collection
- ⏳ Alert system

## Contributing

Contributions are welcome! Please feel free to submit issues and pull requests.

## License

This project is licensed under the Apache License 2.0 - see the LICENSE file for details.