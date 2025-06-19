# SenHub Agent - User Guide

## Introduction

SenHub Agent is a powerful and flexible monitoring system that collects real-time system metrics and exposes them via REST APIs compatible with major monitoring tools (PRTG, Nagios, Prometheus, etc.).

## Quick Start

### Online Mode (Recommended)

```bash
# Installation with authentication key
./agent install --authentication-key "your-agent-key"

# Start the agent
./agent start

# Check status
./agent status
```

### Offline Mode (Standalone)

```bash
# Offline installation with local configuration
./agent install --offline

# Secure HTTPS installation
./agent install --offline --enable-https

# Start the agent
./agent run --offline
```

## Web Interface

Once the agent is running, access the web interface:

- **Dashboard**: `http://localhost:8080/web/{your-key}/dashboard`
- **API Explorer**: `http://localhost:8080/web/{your-key}/explorer`
- **Documentation**: `http://localhost:8080/web/{your-key}/docs`

### Main Dashboard

The dashboard displays in real-time:

- **Agent status** (version, uptime, port)
- **Health check** (HTTP server, cache, metrics)
- **System resources** (memory, goroutines, CPU)
- **Active probes** with metrics count

### API Explorer

Interactive tool for:

- Testing API endpoints live
- Filtering metrics by tags
- Viewing request examples
- Exploring data schemas

## Configuration

### Automatic Configuration (Online Mode)

The agent automatically retrieves its configuration from SenHub Platform.

### Manual Configuration (Offline Mode)

Create an `agent-config.yaml` file:

```yaml
agent:
  key: "offline-hostname-timestamp-random"
  mode: offline

storage:
  - name: http
    params:
      port: 8080
      bind_address: "127.0.0.1"
      endpoints: ["prtg", "nagios", "web"]

probes:
  - name: cpu
    params: {interval: 30}
  - name: memory  
    params: {interval: 30}
  - name: network
    params: {interval: 60}
  - name: logicaldisk
    params: {interval: 30}
```

## Available Probes

### System Probes

- **cpu**: CPU usage per core and total
- **memory**: Physical and virtual memory
- **network**: Network traffic per interface
- **logicaldisk**: Disk space and I/O

### Infrastructure Probes

- **redfish**: Server monitoring (Dell, HPE, Lenovo, Cisco)
- **syslog**: Syslog event collection
- **otel**: OpenTelemetry metrics

### Application Probes

- **ping_webapp**: URL availability
- **load_webapp**: Response time and HTTP codes
- **ping_gateway**: Network connectivity

## Output Formats

### PRTG Network Monitor

```bash
# Get metrics by probe
curl "http://localhost:8080/api/{key}/prtg/metrics/cpu"

# Apply filters
curl "http://localhost:8080/api/{key}/prtg/metrics/cpu?tags=instance:0"
```

**PRTG Response Example:**

```json
{
  "prtg": {
    "result": [
      {
        "channel": "CPU Usage - Core 0",
        "value": 15.2,
        "unit": "custom",
        "customunit": "%",
        "float": 1
      }
    ]
  }
}
```

### Nagios/Icinga

```bash
curl "http://localhost:8080/api/{key}/nagios/metrics/system_health"
```

**Nagios Response Example:**

```json
{
  "status": 0,
  "status_text": "OK", 
  "message": "System health: CPU 15.2%, Memory 45.1%",
  "perfdata": "cpu_usage=15.2%;80;90 memory_used=45.1%;85;95"
}
```

### Prometheus

```bash
curl "http://localhost:8080/api/{key}/prometheus/metrics"
```

## Tag System

### Automatic Tags

- `probe_name`: Source probe name
- `hostname`: Host name
- `instance`: Component instance/index

### Specialized Tags

- **Network**: `interface`, `adapter_type`
- **Disk**: `drive_letter`, `filesystem`
- **Redfish**: `component_type`, `vendor`, `model`

### Tag Filtering Examples

```bash
# Specific CPU core
curl "...?tags=instance:0"

# Specific network interface  
curl "...?tags=interface:eth0"

# Multiple filters
curl "...?tags=probe_name:redfish,component_type:thermal"
```

## Debug and Logging

### Enable Debug at Startup

```bash
# Enable global debug
./agent run --authentication-key "key" --verbose

# Selective debug by module
./agent run --authentication-key "key" --verbose --debug-modules "strategy.http,probe.redfish"
```

### Runtime Debug API

```bash
# View current log levels
curl "http://localhost:8080/api/{key}/debug/logs"

# Modify levels at runtime
curl -X POST "http://localhost:8080/api/{key}/debug/logs" \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [{"module": "strategy.http", "level": "debug"}]}'
```

### Available Debug Modules

- `strategy.http` - HTTP strategy and cache
- `probe.redfish` - Redfish probe  
- `probe.host` - System probes
- `cache` - Cache operations
- `configuration` - Configuration management
- `scheduler` - Probe scheduling

## HTTPS Security

### Auto-generated Certificates

```bash
./agent install --offline --enable-https
```

### Custom Certificates

```bash
./agent install --offline --enable-https \
  --cert-file /path/to/cert.pem \
  --key-file /path/to/key.pem \
  --min-tls-version 1.3
```

### HTTPS Configuration

```yaml
storage:
  - name: http
    params:
      port: 8443
      endpoints: ["prtg", "web", "nagios"]
      tls:
        enabled: true
        mode: "auto"
        auto_cert:
          organization: "My Company"
          common_name: "agent.company.com"
          san_hosts: ["localhost", "192.168.1.100"]
          validity_days: 365
```

## Redfish Monitoring

### Basic Configuration

```yaml
probes:
  - name: redfish
    params:
      endpoint: "https://192.168.1.100/redfish/v1/"
      username: "admin"
      password: "password"
      collections: ["system", "thermal", "power"]
      interval: 300
      insecure: true
```

### Available Collections

- **system**: General status, processors, memory
- **thermal**: Temperatures and fans
- **power**: Power supply and consumption
- **processor**: Detailed processor information
- **memory**: Detailed memory modules
- **storage**: Disks and controllers
- **network**: Network interfaces

### Supported Vendors

- **Dell**: PowerEdge, PowerVault ME5024
- **HPE**: ProLiant, Synergy
- **Lenovo**: ThinkSystem
- **Cisco**: UCS
- **Generic**: Redfish-compatible servers

## Event Monitoring

### Syslog Configuration

```yaml
probes:
  - name: syslog
    params:
      port: 514
      protocol: "udp"
      bind_address: "0.0.0.0"
```

### Redfish Events

System events are automatically collected from Redfish servers and converted to numerical metrics.

## Troubleshooting

### Common Issues

#### Agent Won't Start

```bash
# Check logs with verbose output
./agent run --authentication-key "key" --verbose

# Test with offline configuration
./agent run --offline --config-path ./test-config.yaml
```

#### No Metrics Available

1. Check that probes are active in the dashboard
2. Review logs with targeted debugging:
   ```bash
   ./agent run --verbose --debug-modules "probe.host,cache"
   ```
3. Test API endpoints directly:
   ```bash
   curl "http://localhost:8080/api/{key}/info/probes"
   ```

#### HTTPS Issues

```bash
# Verify certificates
openssl x509 -in cert.pem -text -noout

# Test with curl
curl -k https://localhost:8443/health
```

### Test URLs

```bash
# Global health check
curl "http://localhost:8080/health"

# List active probes
curl "http://localhost:8080/api/{key}/info/probes"

# Get probe metrics
curl "http://localhost:8080/api/{key}/prtg/metrics/cpu"

# View probe schema
curl "http://localhost:8080/api/{key}/info/schema/cpu"
```

## Support Resources

### Diagnostics

- **Detailed logs**: Use `--verbose --debug-modules "module1,module2"`
- **Health check**: Access `/health` endpoint
- **System info**: Check `/api/{key}/info/system`

### Available Interfaces

- **Web interface**: Integrated dashboard at `/web/{key}/`
- **API Explorer**: Real-time testing at `/web/{key}/explorer`
- **Documentation**: Complete API reference at `/web/{key}/docs`

## Production Configuration

### Recommendations

- Use HTTPS in production environments
- Configure appropriate collection intervals (30-300 seconds)
- Monitor memory consumption regularly
- Backup configuration files

### Production Example

```yaml
agent:
  key: "production-key"
  mode: online

storage:
  - name: http
    params:
      port: 8443
      bind_address: "0.0.0.0"
      endpoints: ["prtg", "nagios"]
      tls:
        enabled: true
        cert_file: "/etc/ssl/agent.crt"
        key_file: "/etc/ssl/agent.key"
        min_tls_version: "1.3"

probes:
  - name: cpu
    params: {interval: 60}
  - name: memory
    params: {interval: 60}
  - name: redfish
    params:
      endpoint: "https://server.lan/redfish/v1/"
      username: "monitoring"
      password: "secure-password"
      interval: 300
      collections: ["system", "thermal"]
```