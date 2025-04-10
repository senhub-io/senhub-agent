# SenHub Agent

SenHub Agent is a powerful monitoring and observability agent that collects metrics and events from various sources and sends them to multiple destinations. It's designed to be extensible, configurable, and efficient.

[![Build Status](https://github.com/senhub-io/senhubagent/actions/workflows/go-test.yml/badge.svg)](https://github.com/senhub-io/senhubagent/actions/workflows/go-test.yml)

## Installation

### For Development

```bash
make install
```

### Build

```bash
# Standard build for your platform
make build

# Platform-specific builds
make build-windows    # Windows build
make build-linux      # Linux build
make build-darwin     # macOS build
```

## Running the Project

You need to have Go installed on your machine (Go 1.23.2+ recommended).

### Development Mode

```bash
make watch
```

This will start the project in development mode with live reloading on code changes.

### Production Mode

```bash
make build
./senhub-agent run --authentication-key <your_key> --server-url "https://your-server-url.com"
```

### Build Environment

The project can be built in `development` or `production` mode by setting the `ENV` variable:

```bash
ENV=development make build
```

## Testing

```bash
# Run all tests
make test

# Run a specific test
go test -v ./path/to/package -run TestName
```

## Configuration

Agent configuration is loaded from the SenHub server. The agent pulls its configuration dynamically and can adjust to changes without restarting.

A configuration has three main sections:
- `agent`: General agent settings
- `probes`: Data collection components
- `storage`: Data destination components

Here's a sample configuration:

```json
{
  "agent": {
    "version": "0.1.0",
    "registry_url": "https://eu-west-1.intake.senhub.io/",
    "update_check_interval": "1h"
  },
  "probes": [
    { "name": "cpu", "params": {} },
    { "name": "memory", "params": {} },
    { "name": "network", "params": {} },
    { "name": "logicaldisk", "params": {} },
    { "name": "ping_gateway", "params": {} },
    { "name": "wifi_signal_strength", "params": {} },
    
    { "name": "ping_webapp", "params": { "url": "https://example.com" } },
    { "name": "ping_webapp", "params": { "url": "https://company-intranet.local" } },
    
    { "name": "load_webapp", "params": { "url": "https://example.com", "timeout": 30 } },
    { "name": "load_webapp", "params": { "url": "https://company-intranet.local", "timeout": 10 } },
    
    { "name": "syslog", "params": { "port": 514, "protocol": "udp" } },
    { "name": "event", "params": {} },
    { 
      "name": "systemlogs",
      "params": {
        "interval": 60,
        "max_events": 100,
        "windows": {
          "channels": ["Application", "System", "Security"],
          "levels": ["Critical", "Error", "Warning"]
        }
      }
    },
    
    { 
      "name": "redfish", 
      "params": { 
        "endpoint": "https://192.168.1.100", 
        "username": "admin", 
        "password": "password",
        "cache_duration": 240,
        "collections": ["system", "thermal", "power", "processor", "memory", "storage", "network"]
      } 
    }
  ],
  "storage": [
    { "name": "senhub", "params": {} },
    {
      "name": "prtg",
      "params": {
        "data_retention_period": "2m",
        "server_url": "http://prtg-server:8080"
      }
    },
    {
      "name": "event",
      "params": {
        "queue_size": 1000,
        "server_url": "https://eu-west-1.intake.senhub.io",
        "sync_interval": "30s"
      }
    }
  ]
}
```

## Available Probes

The agent supports multiple probe instances, including multiple instances of the same probe type with different parameters.

### System Monitoring

#### CPU Probe
Collects CPU usage metrics, including per-core utilization and system load.

- **Configuration**
  ```json
  { "name": "cpu", "params": {} }
  ```
- **Metrics**
  - `cpu_usage_percent`: Overall CPU usage percentage
  - `cpu_core_usage_percent`: Per-core usage percentage
  - `cpu_load_1min`, `cpu_load_5min`, `cpu_load_15min`: System load averages

#### Memory Probe
Monitors system memory usage and availability.

- **Configuration**
  ```json
  { "name": "memory", "params": {} }
  ```
- **Metrics**
  - `memory_total_bytes`: Total physical memory
  - `memory_used_bytes`: Used physical memory
  - `memory_free_bytes`: Free physical memory
  - `memory_usage_percent`: Memory usage percentage
  - `swap_total_bytes`, `swap_used_bytes`, `swap_free_bytes`: Swap memory metrics

#### Network Probe
Monitors network interface activity and throughput.

- **Configuration**
  ```json
  { "name": "network", "params": {} }
  ```
- **Metrics**
  - `network_bytes_sent`, `network_bytes_received`: Traffic volume
  - `network_packets_sent`, `network_packets_received`: Packet counts
  - `network_errors_in`, `network_errors_out`: Error counts
  - `network_dropped_in`, `network_dropped_out`: Dropped packet counts

#### Logical Disk Probe
Monitors disk space and IO performance.

- **Configuration**
  ```json
  { "name": "logicaldisk", "params": {} }
  ```
- **Metrics**
  - `disk_total_bytes`, `disk_used_bytes`, `disk_free_bytes`: Disk space
  - `disk_usage_percent`: Disk usage percentage
  - `disk_io_reads`, `disk_io_writes`: IO operations
  - `disk_io_read_bytes`, `disk_io_write_bytes`: IO throughput
  - `disk_io_time`: Time spent on IO operations

#### WiFi Signal Strength Probe
Measures WiFi signal quality and connection metrics.

- **Configuration**
  ```json
  { "name": "wifi_signal_strength", "params": {} }
  ```
- **Metrics**
  - `wifi_signal_strength_dbm`: Signal strength in dBm
  - `wifi_signal_quality_percent`: Signal quality percentage
  - `wifi_link_speed_mbps`: Connection speed

### Network Connectivity

#### Ping Gateway Probe
Tests connectivity to the default network gateway.

- **Configuration**
  ```json
  { "name": "ping_gateway", "params": {} }
  ```
- **Metrics**
  - `gateway_latency_ms`: Round-trip time to gateway
  - `gateway_packet_loss_percent`: Packet loss percentage
  - `gateway_reachable`: Boolean (1/0) indicating gateway reachability

### Web Monitoring

#### Ping Web App Probe
Tests web application availability and measures latency. Multiple instances with different URLs can be configured.

- **Configuration**
  ```json
  { "name": "ping_webapp", "params": { "url": "https://example.com" } }
  ```
- **Required Parameters**
  - `url`: The URL to ping/test
- **Metrics**
  - `averageLatency`: Average round-trip time in milliseconds
  - `packetLoss`: Packet loss percentage

#### Load Web App Probe
Measures detailed web application loading performance metrics. Multiple instances with different URLs can be configured.

- **Configuration**
  ```json
  { "name": "load_webapp", "params": { "url": "https://example.com", "timeout": 30 } }
  ```
- **Required Parameters**
  - `url`: The URL to measure loading time
- **Optional Parameters**
  - `timeout`: Request timeout in seconds (default: 30, range: 1-300)
- **Metrics**
  - `dnstime`: DNS resolution time in milliseconds
  - `connecttime`: TCP connection time in milliseconds
  - `tlstime`: TLS handshake time in milliseconds (for HTTPS)
  - `ttfb`: Time to first byte in milliseconds
  - `total_time`: Total page load time in milliseconds

### Event Collection

#### SystemLogs Probe
Collects system logs from the appropriate platform-specific source (Windows Event Log, systemd journal, or Apple System Log).

- **Configuration**
  ```json
  { "name": "systemlogs", "params": {} }
  ```
- **Optional Parameters**
  - `sources`: Array of log sources to collect from (auto-detected if not specified)
  - `max_events`: Maximum number of events to collect per interval (default: 100)
  - `interval`: Collection interval in seconds (default: 60)
  - `filter`: Optional filter expression for events
  - **Windows-specific settings**:
    ```json
    "windows": {
      "channels": ["Application", "System", "Security"],
      "event_ids": [1000, 1001],
      "levels": ["Critical", "Error", "Warning"]
    }
    ```
  - **Linux-specific settings**:
    ```json
    "journald": {
      "units": ["systemd", "ssh"],
      "priority": ["emerg", "alert", "crit", "err"]
    }
    ```
- **Events**
  - System log events with source, ID, level, message, and timestamp
  - Platform-specific metadata

#### Syslog Probe
Collects system logs via Syslog protocol.

- **Configuration**
  ```json
  { "name": "syslog", "params": { "port": 514, "protocol": "udp" } }
  ```
- **Optional Parameters**
  - `port`: Port to listen on (default: 514)
  - `protocol`: Either "udp" or "tcp" (default: "udp")
- **Events**
  - Structured Syslog messages with facility, severity, hostname, and message content

#### Event Probe
Collects custom events via HTTP endpoint.

- **Configuration**
  ```json
  { "name": "event", "params": {} }
  ```
- **Events**
  - Custom events sent to the agent's HTTP endpoint

### Hardware Monitoring

#### Redfish Probe
Monitors server hardware via the Redfish API with vendor-specific collectors for Dell, HPE, Cisco, and Lenovo.

- **Configuration**
  ```json
  { 
    "name": "redfish", 
    "params": { 
      "endpoint": "https://192.168.1.100", 
      "username": "admin", 
      "password": "password",
      "cache_duration": 240,
      "collections": ["system", "thermal", "power", "processor", "memory", "storage", "network"]
    } 
  }
  ```
- **Required Parameters**
  - `endpoint`: URL of the Redfish API endpoint
  - `username`: Username for Redfish API authentication
  - `password`: Password for Redfish API authentication
- **Optional Parameters**
  - `cache_duration`: Duration in seconds to cache data (default: 240 seconds)
  - `collections`: Array of collections to monitor (default includes "system", "thermal", "power", "processor", "memory")
    - Available collections: "system", "thermal", "power", "processor", "memory", "storage", "network"
- **Metrics**
  - System information (model, serial, firmware versions)
  - Temperature sensors and thresholds
  - Fan speeds and status
  - Power consumption and supply status
  - Processor utilization and health
  - Memory module status
  - Storage device health and capacity
  - Network interface status and throughput

## Storage Options

### SenHub Storage
Sends metrics to the SenHub platform.

- **Configuration**
  ```json
  { "name": "senhub", "params": {} }
  ```
  
### PRTG Storage
Forwards metrics to PRTG Network Monitor.

- **Configuration**
  ```json
  {
    "name": "prtg",
    "params": {
      "server_url": "http://prtg-server:8080",
      "data_retention_period": "5m"
    }
  }
  ```
- **Required Parameters**
  - `server_url`: URL of the PRTG server
- **Optional Parameters**
  - `data_retention_period`: How long to keep data (default: "5m")
  
### Event Storage
Manages event data collected by event-oriented probes.

- **Configuration**
  ```json
  {
    "name": "event",
    "params": {
      "server_url": "https://eu-west-1.intake.senhub.io",
      "queue_size": 1000,
      "sync_interval": "30s"
    }
  }
  ```
- **Required Parameters**
  - `server_url`: URL where to send events
- **Optional Parameters**
  - `queue_size`: Size of the event queue (default: 1000)
  - `sync_interval`: How frequently to sync events (default: "30s")

## Agent Configuration

### General Options
- `version` (optional): Required version for the agent. Can be in the form of:
  - Exact version: `0.1.0`
  - Latest: `latest`
  - Constraints: `>=0.1.0`, `<=0.1.0`, `>0.1.0`, `<0.1.0`, `!=0.1.0`
- `registry_url` (optional): URL to the registry server (default: `https://eu-west-1.intake.senhub.io/`)
- `update_check_interval` (optional): Interval to check for updates, can be seconds (integer) or duration string like "1h", "30m" (default: 1h)

## Architecture

The agent follows a modular architecture with these key components:

1. **Probes**: Collect data from various sources
2. **Probe Poller**: Manages probe lifecycle and scheduling
3. **DataStore**: Routes data to appropriate strategies
4. **Storage Strategies**: Send data to different destinations

### Data Flow
1. Configuration is loaded from server
2. Probes are initialized and scheduled
3. Data is collected from probes
4. DataStore routes data to configured strategies
5. Strategies send data to their destinations

## Contributing

### Setup
1. Fork the repository
2. Install Go 1.23.2 or later
3. Run `make install` to set up development environment
4. Run `make test` to ensure everything works

### Code Style
- Use `gofmt` for formatting (enforced by pre-commit hook)
- Follow standard Go conventions:
  - PascalCase for exported identifiers
  - camelCase for unexported identifiers
  - Group imports: standard library, third-party, internal
  - Document exported functions and types
  - Write tests for new functionality

### Pull Requests
1. Create a feature branch
2. Implement your changes with tests
3. Run `make test` to verify all tests pass
4. Submit a pull request with a clear description

## License

© SenHub. All rights reserved.