# SenHub Agent

SenHub Agent is a powerful monitoring and observability agent that collects metrics and events from various sources and sends them to multiple destinations. It's designed to be extensible, configurable, and efficient.

[![Build Status](https://github.com/senhub-io/senhubagent/actions/workflows/go-test.yml/badge.svg)](https://github.com/senhub-io/senhubagent/actions/workflows/go-test.yml)

## 🚀 Quick Start

### Offline Mode (Recommended for Quick Testing)

```bash
# Download and install
wget https://releases.senhub.io/agent/latest/senhub-agent-linux-amd64
chmod +x senhub-agent-linux-amd64

# Install in offline mode with HTTPS
sudo ./senhub-agent-linux-amd64 install --offline --enable-https

# Start the agent
sudo ./senhub-agent-linux-amd64 start

# Access your dashboard
open https://localhost:8443/web/{agentkey}/dashboard
```

**📚 [5-Minute Setup Guide](docs/user-guide/QUICK-START-OFFLINE.md)** | **📖 [Complete Offline Documentation](docs/user-guide/OFFLINE-MODE.md)**

### Online Mode (SenHub Platform)

```bash
# Option 1: Direct command line
sudo ./senhub-agent run --authentication-key <your_key> --server-url "https://your-server-url.com"

# Option 2: Authentication key from config file (agent-config.yaml)
sudo ./senhub-agent run  # Automatically loads key from config file
```

## 📋 Documentation

### User Guides
- **[🏃 Quick Start (5 min)](docs/user-guide/QUICK-START-OFFLINE.md)** - Get running in 5 minutes
- **[💻 Offline Mode Guide](docs/user-guide/OFFLINE-MODE.md)** - Complete standalone deployment guide
- **[🔒 HTTPS Configuration](docs/admin-guide/HTTPS-CONFIGURATION.md)** - TLS/SSL setup and security
- **[🛠️ Troubleshooting](docs/troubleshooting/TROUBLESHOOTING-OFFLINE.md)** - Common issues and solutions

### Developer Resources
- **[👨‍💻 Development Guide](CLAUDE.md)** - Architecture, development, and build instructions
- **[📝 Logging System](docs/admin-guide/LOGGING.md)** - Advanced logging and debugging
- **[🔌 Probe Configuration](docs/user-guide/PROBE-CONFIGURATION.md)** - Configure monitoring probes
- **[🏷️ Redfish Metrics](docs/probes/redfish/METRICS.md)** - Hardware monitoring documentation

## ✨ Key Features

### 🌐 Dual Mode Operation
- **Online Mode**: Integrated with SenHub platform for centralized management
- **Offline Mode**: Standalone operation with local web interface and APIs

### 📊 Comprehensive Monitoring
- **System Metrics**: CPU, memory, disk, network monitoring
- **Hardware Monitoring**: Server hardware via Redfish (iDRAC, iLO, BMC)
- **Application Monitoring**: Website performance and availability
- **Event Collection**: Syslog, Windows events, custom events
- **OpenTelemetry**: Metrics, traces, and logs collection

### 🔌 Multiple API Formats
- **PRTG Network Monitor**: Native integration
- **Nagios/Icinga**: Status codes and performance data
- **Prometheus**: Metrics for Grafana dashboards
- **SenHub**: Native platform format

### 🔒 Security & TLS
- **Auto-generated certificates** for quick HTTPS setup
- **Custom certificate support** for production deployments
- **TLS 1.2/1.3 support** with secure cipher suites
- **Automatic certificate renewal**

### 🌍 Cross-Platform
- **Linux**: Full support (systemd, init.d)
- **Windows**: Windows Service integration
- **macOS**: LaunchDaemon support
- **Docker**: Container-ready deployment

## 🛠️ Installation Methods

### Binary Installation

```bash
# Linux
wget https://releases.senhub.io/agent/latest/senhub-agent-linux-amd64
chmod +x senhub-agent-linux-amd64
sudo mv senhub-agent-linux-amd64 /usr/local/bin/agent

# Windows
curl -O https://releases.senhub.io/agent/latest/senhub-agent-windows-amd64.exe

# macOS
curl -O https://releases.senhub.io/agent/latest/senhub-agent-darwin-amd64
```

### Package Managers

```bash
# Ubuntu/Debian
wget -qO- https://packages.senhub.io/key.asc | sudo apt-key add -
echo "deb https://packages.senhub.io/ubuntu focal main" | sudo tee /etc/apt/sources.list.d/senhub.list
sudo apt update && sudo apt install senhub-agent

# CentOS/RHEL
sudo yum install -y https://packages.senhub.io/rpm/senhub-agent-latest.rpm

# Homebrew (macOS)
brew tap senhub/tap
brew install senhub-agent
```

### Docker

```bash
# Offline mode
docker run -d \
  --name senhub-agent \
  -p 8443:8443 \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  senhub/agent:latest --offline --enable-https

# Online mode  
docker run -d \
  --name senhub-agent \
  -e SENHUB_KEY="your-key" \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  senhub/agent:latest
```

## 🔧 Development

### Build Requirements

- Go 1.23.2+
- Make
- Git

### Build Commands

```bash
# Standard build for your platform
make build

# Platform-specific builds
make build-windows    # Windows build
make build-linux      # Linux build
make build-darwin     # macOS build

# Cross-compile all platforms
make build-all

# Run tests
make test

# Development with live reload
make watch
```

### Debug Logging with DebugLogShipper

The agent includes a powerful DebugLogShipper component that can send logs to remote endpoints like VictoriaLogs, Grafana Loki, or Elasticsearch. This is useful for centralized log collection and remote debugging.

#### Basic Usage

```bash
# Enable debug logging to a remote endpoint
./senhub-agent start --authentication-key <your_key> \
    --debug-log-shipper-url "http://logserver:9428" \
    --debug-log-shipper-tags "env=production,component=agent"
```

#### Configuration Options

| Option | Environment Variable | Description | Default |
|--------|---------------------|-------------|---------|
| `--debug-log-shipper-url` | `SENHUB_DEBUG_LOG_SHIPPER_URL` | URL of the log collection endpoint | - |
| `--debug-log-shipper-tags` | `SENHUB_DEBUG_LOG_SHIPPER_TAGS` | Custom tags (format: key1=value1,key2=value2) | - |
| `--debug-log-shipper-buffer` | `SENHUB_DEBUG_LOG_SHIPPER_BUFFER` | Buffer size before sending logs | 100 |

#### Compatible Log Collection Systems

##### VictoriaLogs (Recommended)

For VictoriaLogs, simply specify the base URL:

```bash
--debug-log-shipper-url "http://victorialogs:9428"
```

The shipper automatically converts this to the JSON Stream API endpoint:
```
http://victorialogs:9428/insert/jsonline?_stream_fields=stream&_time_field=timestamp&_msg_field=message
```

##### Grafana Loki

For Loki, specify the push API endpoint:

```bash
--debug-log-shipper-url "http://loki:3100/loki/api/v1/push"
```

##### Elasticsearch

For Elasticsearch, specify the document API endpoint:

```bash
--debug-log-shipper-url "http://elasticsearch:9200/logs/_doc"
```

#### Automatic Tag Enrichment

The following tags are automatically added if not explicitly specified:

- `agent_name`: "senhub-agent"
- `host_name`: Hostname of the machine
- `env`: Environment (production/development)

#### Usage with Service or Console Mode

The DebugLogShipper works in both service mode and console mode:

```bash
# With service start
./senhub-agent start --authentication-key <your_key> \
    --debug-log-shipper-url "http://logserver:9428"

# With console mode
./senhub-agent run --authentication-key <your_key> \
    --debug-log-shipper-url "http://logserver:9428" \
    --debug-log-shipper-tags "env=production,component=agent" \
    --verbose
```

#### Complete Configuration Example

```bash
./senhub-agent run --authentication-key <your_key> \
    --server-url "https://api.senhub.io" \
    --debug-log-shipper-url "http://victorialogs:9428" \
    --debug-log-shipper-tags "env=production,component=agent,region=europe,customer=acme" \
    --debug-log-shipper-buffer 200 \
    --verbose
```

For more technical details on the DebugLogShipper implementation, see `internal/agent/services/debugshipper/README.md`.

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

## Available Probes

The agent includes various probes for collecting different types of metrics:

### System Probes
- CPU, Memory, Disk, and Network metrics
- Process monitoring
- Event logs

### Application Probes
- Web application monitoring
- Syslog collection

### Infrastructure Probes
- Redfish monitoring for servers and storage systems (see [REDFISH-METRICS.md](docs/probes/redfish/METRICS.md) for details on available metrics)
- Gateway connectivity monitoring

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
        "initial_lookback": 60,
        "sources": ["windowsevents", "journald", "asl"],
        "windows": {
          "channels": ["Application", "System", "Security"],
          "event_ids": [1000, 1001, 7036, 7040],
          "levels": ["Critical", "Error", "Warning"]
        },
        "journald": {
          "units": ["systemd", "ssh"],
          "priority": ["emerg", "alert", "crit", "err"],
          "identifiers": ["sshd"]
        },
        "asl": {
          "processes": ["com.apple.xpc.launchd"],
          "levels": ["Emergency", "Alert", "Critical", "Error"]
        }
      }
    },
    
    { 
      "name": "redfish", 
      "params": { 
        "endpoint": "https://192.168.1.100", 
        "username": "admin", 
        "password": "password",
        "verify_ssl": true,
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
    },
    {
      "name": "http",
      "params": {
        "port": 8080,
        "endpoints": ["prtg", "senhub"]
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
    - Windows: `["windowsevents"]`
    - Linux: `["journald"]`
    - macOS: `["asl"]`
  - `max_events`: Maximum number of events to collect per interval (default: 100)
  - `interval`: Collection interval in seconds (default: 60)
  - `filter`: Optional filter expression for events
  - `initial_lookback`: Initial lookback period in minutes for first collection (default: 1440 - 24 hours)
  
  - **Windows-specific settings**:
    ```json
    "windows": {
      "channels": ["Application", "System", "Security"],
      "event_ids": [1000, 1001],
      "levels": ["Critical", "Error", "Warning", "Information"]
    }
    ```
    - `channels`: Windows Event Log channels to monitor (default: `["Application", "System"]`)
    - `event_ids`: Specific event IDs to filter (optional, collects all IDs if not specified)
    - `levels`: Event severity levels to collect (default: `["Critical", "Error", "Warning", "Information"]`)
      - Available levels: `"LogAlways"`, `"Critical"`, `"Error"`, `"Warning"`, `"Information"`, `"Verbose"`
  
  - **Linux-specific settings**:
    ```json
    "journald": {
      "units": ["systemd", "ssh"],
      "priority": ["emerg", "alert", "crit", "err"],
      "identifiers": ["sshd", "cron"],
      "boot_offset": 0
    }
    ```
    - `units`: Systemd units to monitor (optional, collects all units if not specified)
    - `priority`: Priority levels to collect (default: `["emerg", "alert", "crit", "err"]`)
      - Available levels: `"emerg"`, `"alert"`, `"crit"`, `"err"`, `"warning"`, `"notice"`, `"info"`, `"debug"`
    - `identifiers`: Journal source identifiers to filter (optional, collects all if not specified)
    - `boot_offset`: Offset in number of boots to collect from (0=current boot, 1=previous boot, etc.)
      
  - **macOS-specific settings**:
    ```json
    "asl": {
      "processes": ["com.apple.xpc.launchd", "kernel"],
      "facility": ["auth", "daemon"],
      "levels": ["Emergency", "Alert", "Critical", "Error"]
    }
    ```
    - `processes`: Specific processes to collect logs from (optional, collects all if not specified)
    - `facility`: Log facilities to collect (optional, collects all if not specified)
      - Common facilities: `"auth"`, `"daemon"`, `"user"`, `"local0"` through `"local7"`
    - `levels`: Log severity levels to collect (default: `["Emergency", "Alert", "Critical", "Error"]`)
      - Available levels: `"Emergency"`, `"Alert"`, `"Critical"`, `"Error"`, `"Warning"`, `"Notice"`, `"Info"`, `"Debug"`

- **Example Full Configuration**:
  ```json
  {
    "name": "systemlogs",
    "params": {
      "sources": ["windowsevents", "journald", "asl"],
      "interval": 60,
      "max_events": 100,
      "initial_lookback": 60,
      "windows": {
        "channels": ["Application", "System", "Security"],
        "event_ids": [1000, 1001, 7036, 7040],
        "levels": ["Critical", "Error"]
      },
      "journald": {
        "units": ["systemd", "ssh", "httpd"],
        "priority": ["emerg", "alert", "crit", "err"],
        "identifiers": ["sshd", "cron"],
        "boot_offset": 0
      },
      "asl": {
        "processes": ["com.apple.xpc.launchd", "kernel"],
        "facility": ["auth", "daemon"],
        "levels": ["Emergency", "Alert", "Critical", "Error"]
      }
    }
  }
  ```
  
  Note: The agent will automatically detect the appropriate source based on the operating system. The cross-platform configuration above includes settings for all platforms, but only the settings for the current platform will be used.

- **Events**
  - System log events with source, ID, level, message, and timestamp
  - Platform-specific metadata including channel/facility, host, and structured data
  - Events are routed to the "event" strategy for processing and storage

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
      "verify_ssl": true,
      "collections": ["system", "thermal", "power", "processor", "memory", "storage", "network"]
    } 
  }
  ```
- **Required Parameters**
  - `endpoint`: URL of the Redfish API endpoint
  - `username`: Username for Redfish API authentication
  - `password`: Password for Redfish API authentication
- **Optional Parameters**
  - `verify_ssl`: Whether to verify SSL certificates (default: true)
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

### HTTP Storage
Exposes agent metrics via HTTP REST API for external monitoring tools like PRTG Network Monitor.

- **Configuration**
  ```json
  {
    "name": "http",
    "params": {
      "port": 8080,
      "bind_address": "0.0.0.0",
      "endpoints": ["prtg", "senhub", "prometheus"]
    }
  }
  ```
- **Optional Parameters**
  - `port`: HTTP server port (default: 8080)
  - `bind_address`: IP address to bind to (default: "0.0.0.0", use "127.0.0.1" for loopback only)
  - `endpoints`: List of monitoring tool endpoints to enable (default: ["senhub"])
    - `prtg`: PRTG-compatible JSON format with friendly names
    - `senhub`: SenHub raw format with technical + display names
    - `prometheus`: Prometheus metrics format (future)
    - `nagios`: Nagios check format (future)
    - `zabbix`: Zabbix format (future)

- **Endpoints**
  - `POST /api/{agentkey}/prtg/metrics`: PRTG-compatible endpoint for metric retrieval
  - `GET /health`: Health check endpoint

- **Features**
  - **Authentication**: Agent key validation in URL path
  - **Caching**: 5-minute TTL for metrics with automatic cleanup
  - **Transformations**: Configurable metric name transformations per probe
  - **PRTG Format**: JSON response with channels, values, units, and limits
  - **Configuration Emulation**: Dynamic probe configuration via POST body (ready for future implementation)

- **PRTG Integration Example**
  ```bash
  curl -X POST "http://agent-host:8080/api/your-agent-key/prtg/metrics" \
    -H "Content-Type: application/json" \
    -d '{
      "probe": "redfish",
      "target": "server1",
      "config": {
        "host": "192.168.1.100",
        "username": "admin",
        "password": "secret"
      }
    }'
  ```

- **Response Format**
  ```json
  {
    "prtg": {
      "result": [
        {
          "channel": "CPU Temperature - Processor 0",
          "value": 65.2,
          "unit": "°C",
          "limitmode": 1,
          "limitmaxwarning": 70,
          "limitmaxerror": 85
        }
      ]
    }
  }
  ```

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

## HTTP API Documentation

When the HTTP strategy is enabled, SenHub Agent exposes a comprehensive REST API for retrieving metrics and managing the agent.

### Base Configuration

Enable HTTP strategy in your agent configuration:

```json
{
  "storage_config": [{
    "name": "http",
    "params": {
      "port": 8080,
      "bind_address": "127.0.0.1",
      "endpoints": ["prtg", "senhub", "prometheus"]
    }
  }]
}
```

### API Endpoints Overview

#### System Endpoints

##### Health Check
```http
GET /health
```

Returns comprehensive system information:

```json
{
  "status": "ok",
  "version": "0.1.22-beta",
  "uptime": "2h34m12s",
  "probes_active": 3,
  "metrics_cached": 47
}
```

#### Discovery Endpoints

##### List Available Probes
```http
GET /api/{agentkey}/info/probes
```

Returns all available probes and their metric counts:

```json
{
  "probes": ["cpu", "memory", "network", "redfish"],
  "probe_metrics": {
    "cpu": 20,
    "memory": 11,
    "network": 16
  },
  "total_metrics": 47
}
```

##### Probe Tag Discovery
```http
GET /api/{agentkey}/info/tags/{probe}
```

Discover available tags and their values for a specific probe:

```json
{
  "probe": "cpu",
  "tags": {
    "core": {
      "values": ["0", "1", "2", "3", "4", "5", "6", "7"],
      "description": "CPU core identifier",
      "sample_count": 8
    },
    "instance": {
      "values": ["0", "1", "_Total"],
      "description": "CPU instance identifier (Windows)",
      "sample_count": 3
    }
  },
  "metrics": ["cpu_usage_total", "cpu_core_usage", "cpu_user"],
  "total_metrics": 15
}
```

##### Complete Schema with Examples
```http
GET /api/{agentkey}/info/schema/{probe}
```

Returns complete schema information with usage examples:

```json
{
  "probe": "cpu",
  "tags": { /* same as /info/tags */ },
  "metrics": ["cpu_usage_total", "cpu_core_usage"],
  "total_metrics": 15,
  "examples": [
    {
      "description": "Get all metrics for this probe",
      "url": "/api/{agentkey}/prtg/metrics/cpu",
      "estimated_results": 15
    },
    {
      "description": "Filter by core=0",
      "url": "/api/{agentkey}/prtg/metrics/cpu?tags=core:0",
      "estimated_results": 3
    }
  ]
}
```

#### Metrics Endpoints

##### PRTG Format
```http
GET /api/{agentkey}/prtg/metrics/{probe}
```

Returns metrics in PRTG-compatible format with friendly names and custom units:

```json
{
  "prtg": {
    "result": [
      {
        "channel": "CPU Core 0 Usage",
        "value": 23.5,
        "float": 1,
        "unit": "custom",
        "customunit": "%"
      }
    ]
  }
}
```

##### SenHub Format
```http
GET /api/{agentkey}/senhub/metrics/{probe}
```

Returns metrics in SenHub raw format for internal processing.

##### Prometheus Format
```http
GET /api/{agentkey}/prometheus/metrics
```

Returns all metrics in Prometheus exposition format.

### Query Parameters

All metrics endpoints support powerful filtering via query parameters:

#### Tag Filtering
```http
# Filter by specific tag values
GET /api/{agentkey}/prtg/metrics/cpu?tags=core:0,1,2

# Multiple tag filters
GET /api/{agentkey}/prtg/metrics/network?tags=interface:en0

# Exclude specific values
GET /api/{agentkey}/prtg/metrics/cpu?exclude_tags=instance:_Total
```

#### Metric Filtering
```http
# Select specific metrics only
GET /api/{agentkey}/prtg/metrics/cpu?metrics=cpu_usage_total,cpu_load1
```

#### Pagination
```http
# Limit results
GET /api/{agentkey}/prtg/metrics/redfish?limit=20

# Pagination with offset
GET /api/{agentkey}/prtg/metrics/redfish?limit=20&offset=40
```

#### Combined Filtering
```http
# Complex filtering example
GET /api/{agentkey}/prtg/metrics/cpu?tags=core:0,1&metrics=cpu_core_usage&limit=5
```

### Administrative Endpoints

#### Cache Information
```http
GET /api/{agentkey}/admin/cache
```

Returns detailed cache statistics and stored metrics for debugging.

#### Log Level Management
```http
# Get current log levels
GET /api/{agentkey}/admin/logs

# Set log levels
POST /api/{agentkey}/admin/logs
Content-Type: application/json

{
  "module_levels": [
    {"module": "strategy.http", "level": "debug"},
    {"module": "probe.redfish", "level": "info"}
  ]
}
```

### Authentication

All API endpoints (except `/health`) require authentication via the agent key in the URL path:

```http
GET /api/your-secret-agent-key/info/probes
```

### Error Responses

The API returns standard HTTP status codes:

- `200 OK` - Success
- `400 Bad Request` - Invalid parameters
- `401 Unauthorized` - Invalid or missing agent key
- `404 Not Found` - Probe or resource not found
- `500 Internal Server Error` - Server error

### Usage Examples

#### Discover Available Data
```bash
# Find all probes
curl "http://localhost:8080/api/your-key/info/probes"

# Explore CPU probe tags
curl "http://localhost:8080/api/your-key/info/tags/cpu"

# Get usage examples
curl "http://localhost:8080/api/your-key/info/schema/network"
```

#### Retrieve Metrics
```bash
# Get all CPU metrics
curl "http://localhost:8080/api/your-key/prtg/metrics/cpu"

# Get specific CPU cores only
curl "http://localhost:8080/api/your-key/prtg/metrics/cpu?tags=core:0,1,2"

# Get specific network interface
curl "http://localhost:8080/api/your-key/prtg/metrics/network?tags=interface:en0"

# Get first 10 Redfish metrics
curl "http://localhost:8080/api/your-key/prtg/metrics/redfish?limit=10"
```

#### Monitor System Health
```bash
# Check agent health
curl "http://localhost:8080/health"

# View cache contents
curl "http://localhost:8080/api/your-key/admin/cache"
```

This HTTP API provides complete programmatic access to all agent metrics and system information, making it easy to integrate SenHub Agent with monitoring tools, dashboards, and automation systems.

## License

© SenHub. All rights reserved.