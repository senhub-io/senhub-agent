# SenHub Agent - Offline Mode Documentation

## Overview

The SenHub Agent now supports **offline mode**, enabling zero-configuration deployment without requiring connectivity to the SenHub platform. This mode is perfect for:

- **Air-gapped environments** where internet connectivity is restricted
- **Edge computing** deployments in remote locations  
- **Local testing** and development without platform accounts
- **Network-isolated** systems requiring local monitoring
- **Standalone monitoring** with local web interface

## Quick Start

### Basic Offline Installation

```bash
# Install agent in offline mode
./agent install --offline

# Start the service
./agent start

# Check status
./agent status
```

**Access your local dashboard**: `http://localhost:8080/web/{agentkey}/dashboard`

### HTTPS Installation (Recommended for Production)

```bash
# Install with auto-generated HTTPS certificates
./agent install --offline --enable-https

# Start the service  
./agent start
```

**Access via HTTPS**: `https://localhost:8443/web/{agentkey}/dashboard`

## Installation Options

### 1. HTTP Mode (Default)

```bash
./agent install --offline
```

- **Protocol**: HTTP only
- **Bind Address**: `127.0.0.1` (localhost only)
- **Port**: `8080`
- **Security**: Basic (suitable for localhost access)

### 2. HTTPS with Auto-Generated Certificates

```bash
./agent install --offline --enable-https
```

- **Protocol**: HTTPS with self-signed certificates
- **Bind Address**: `0.0.0.0` (all interfaces)
- **Port**: `8443` (configurable with `--https-port`)
- **Certificates**: Auto-generated in `./certs/`
- **Security**: TLS 1.2+ with secure cipher suites

### 3. HTTPS with Custom Certificates

```bash
./agent install --offline --enable-https \
  --cert-file /path/to/certificate.pem \
  --key-file /path/to/private-key.pem
```

- **Protocol**: HTTPS with provided certificates
- **Certificates**: User-provided (Let's Encrypt, corporate CA, etc.)
- **Security**: Production-ready with valid certificates

### 4. Advanced Configuration

```bash
./agent install --offline --enable-https \
  --https-port 9443 \
  --https-hosts "agent.company.com,192.168.1.100,server.local" \
  --min-tls-version 1.3 \
  --config-path /etc/senhub-agent/config.yaml
```

- **Custom Port**: `--https-port 9443`
- **Multiple Hostnames**: For certificate SAN (Subject Alternative Names)
- **TLS Version**: Minimum TLS 1.3 for enhanced security
- **Custom Config Path**: Specify where to store configuration

## CLI Options Reference

### Core Offline Options

| Option | Description | Default |
|--------|-------------|---------|
| `--offline` | Enable offline mode | `false` |
| `--config-path PATH` | Configuration file location | `./agent-config.yaml` |

### HTTPS/TLS Options

| Option | Description | Default |
|--------|-------------|---------|
| `--enable-https` | Enable HTTPS with TLS | `false` |
| `--https-port PORT` | HTTPS port | `8443` |
| `--https-hosts HOSTS` | Comma-separated hostnames for certificate SAN | `localhost,127.0.0.1` |
| `--cert-file PATH` | Custom TLS certificate file | *(auto-generated)* |
| `--key-file PATH` | Custom TLS private key file | *(auto-generated)* |
| `--min-tls-version VER` | Minimum TLS version (1.2, 1.3) | `1.2` |

### Debug Options

| Option | Description |
|--------|-------------|
| `--verbose` | Enable debug logging for all modules |
| `--debug-modules LIST` | Enable debug for specific modules only |

## Configuration File Structure

When you install in offline mode, the agent generates a comprehensive YAML configuration file:

```yaml
# Agent identity and mode
agent:
  key: "offline-hostname-1625097600-a1b2c3d4"  # Auto-generated
  mode: offline
  generated: true

# Local HTTP/HTTPS server
storage:
  - name: http
    params:
      port: 8080
      bind_address: "127.0.0.1"
      endpoints: ["prtg", "senhub", "web", "nagios"]
      
      # TLS configuration (if --enable-https)
      tls:
        enabled: true
        mode: "auto"  # or "provided"
        auto_cert:
          organization: "SenHub Agent"
          common_name: "localhost"
          san_hosts: ["localhost", "127.0.0.1"]
          validity_days: 365
          key_size: 2048
        min_tls_version: "1.2"

# Active monitoring probes
probes:
  # System monitoring (enabled by default)
  - name: cpu
    params:
      interval: 30
  - name: memory
    params:
      interval: 30
  - name: network
    params:
      interval: 60
  - name: logicaldisk
    params:
      interval: 30

# ===== COMMENTED EXAMPLES =====
# All other probe types are included as comments for easy activation
```

## Available Monitoring Probes

### Active by Default
- **CPU**: Processor utilization and performance
- **Memory**: RAM usage and swap metrics  
- **Network**: Interface statistics and throughput
- **Logical Disk**: Disk space, I/O operations, and health

### Available (Uncomment to Enable)
- **WiFi Signal**: Wireless connection strength
- **Gateway Ping**: Network connectivity monitoring
- **WebApp Ping/Load**: Application response time testing
- **Redfish Hardware**: Server hardware monitoring (iDRAC, iLO, BMC)
- **Syslog Events**: System log collection and processing
- **Custom Events**: HTTP endpoint for external events
- **OpenTelemetry**: OTEL metrics, traces, and logs

## Web Interface Features

The local web interface provides comprehensive monitoring and management:

### Dashboard (`/web/{agentkey}/dashboard`)
- **System Overview**: Agent status, version, uptime
- **Probe Status**: Active probes and their metrics
- **Real-time Metrics**: Live system monitoring data
- **Performance Charts**: Historical data visualization

### API Explorer (`/web/{agentkey}/explorer`)
- **Interactive Testing**: Test all API endpoints
- **Probe Selection**: Filter by specific probes
- **Tag Filtering**: Dynamic metric filtering
- **URL Generation**: Copy API calls for external tools

### Administration (`/web/{agentkey}/admin`)
- **Cache Management**: View and clear metric cache
- **Log Level Control**: Runtime logging configuration
- **System Information**: Agent and system details
- **Certificate Status**: TLS certificate information

## API Endpoints

The offline agent exposes multiple API formats for integration:

### PRTG Network Monitor Format
```bash
# Get all metrics for PRTG
GET http://localhost:8080/api/{agentkey}/prtg/metrics/cpu

# Response: PRTG JSON with channels, units, limits
{
  "result": [
    {
      "channel": "CPU Usage",
      "value": 45.2,
      "unit": "Percent",
      "limitmode": 1,
      "limitmaxwarning": 80,
      "limitmaxerror": 95
    }
  ]
}
```

### SenHub Raw Format
```bash
# Get raw metrics
GET http://localhost:8080/api/{agentkey}/senhub/metrics/memory

# Response: SenHub native format
[
  {
    "metric_name": "memory.available",
    "value": 8192,
    "unit": "MB",
    "timestamp": "2025-01-01T12:00:00Z",
    "tags": {"probe_name": "memory"}
  }
]
```

### Nagios Format
```bash
# Nagios check format
GET http://localhost:8080/api/{agentkey}/nagios/check/cpu_usage

# Response: Nagios exit codes and messages
{
  "status": 0,
  "message": "OK - CPU usage is 45.2%",
  "perfdata": "cpu_usage=45.2%;80;95;0;100"
}
```

## Certificate Management

### Auto-Generated Certificates

When using `--enable-https`, certificates are automatically generated:

```bash
# Location
./certs/agent-cert.pem    # Public certificate
./certs/agent-key.pem     # Private key (600 permissions)

# Properties
Organization: SenHub Agent
Common Name: localhost  
SAN Hosts: localhost, 127.0.0.1, [custom hosts]
Validity: 365 days
Key Size: RSA 2048
```

### Certificate Renewal

Certificates are automatically renewed when they expire within 30 days of expiration.

### Custom Certificates

For production environments, use certificates from your CA:

```bash
# Install with custom certificates
./agent install --offline --enable-https \
  --cert-file /etc/ssl/certs/agent.pem \
  --key-file /etc/ssl/private/agent.key

# Certificate requirements
- PEM format
- RSA or ECDSA keys
- Valid for configured hostnames
- Readable by agent process
```

## Security Considerations

### Network Security
- **HTTP Mode**: Localhost-only binding (`127.0.0.1`)
- **HTTPS Mode**: All interfaces binding (`0.0.0.0`) with TLS encryption
- **Firewall**: Configure firewall rules for external access

### Authentication
- **Agent Key**: Acts as API authentication token
- **URL Protection**: Agent key in URL path prevents unauthorized access
- **No Default Credentials**: Each installation has unique generated key

### TLS Security
- **Minimum TLS 1.2**: Prevents downgrade attacks
- **Secure Cipher Suites**: Modern encryption algorithms
- **Perfect Forward Secrecy**: ECDHE key exchange
- **Certificate Validation**: Proper SAN handling for multiple hostnames

## Integration Examples

### PRTG Network Monitor

1. **Add HTTP Advanced Sensor**
2. **URL**: `http://agent-host:8080/api/{agentkey}/prtg/metrics/cpu`
3. **Method**: GET
4. **Data Processing**: JSON format
5. **Scanning Interval**: 60 seconds

### Nagios/Icinga

```bash
# Define command
define command {
    command_name    check_senhub_cpu
    command_line    /usr/lib/nagios/plugins/check_http \
                    -H $HOSTADDRESS$ -p 8080 \
                    -u "/api/{agentkey}/nagios/check/cpu_usage" \
                    -f follow
}

# Define service
define service {
    service_description     CPU Usage
    check_command          check_senhub_cpu
    check_interval         5
}
```

### Prometheus Scraping

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'senhub-agent'
    static_configs:
      - targets: ['agent-host:8080']
    metrics_path: '/api/{agentkey}/prometheus/metrics'
    scrape_interval: 30s
```

## Troubleshooting

### Common Issues

#### 1. Configuration File Not Generated
```bash
# Check permissions
ls -la ./agent-config.yaml

# Verify directory is writable
touch ./test-file && rm ./test-file

# Check logs
./agent run --offline --verbose
```

#### 2. HTTPS Certificate Issues
```bash
# Verify certificate files
ls -la ./certs/

# Check certificate validity
openssl x509 -in ./certs/agent-cert.pem -text -noout

# Regenerate certificates
rm -rf ./certs/
./agent run --offline --enable-https
```

#### 3. Port Already in Use
```bash
# Check what's using the port
lsof -i :8080

# Use custom port
./agent run --offline --https-port 9443
```

#### 4. Permission Denied Errors
```bash
# Install without service registration
./agent run --offline

# Check file permissions
chmod 644 ./agent-config.yaml
chmod 600 ./certs/agent-key.pem
```

### Debug Mode

Enable detailed logging for troubleshooting:

```bash
# Full debug mode
./agent run --offline --verbose

# Specific module debugging
./agent run --offline --debug-modules strategy.http,cache,configuration

# Check specific components
./agent run --offline --debug-modules probe.cpu,probe.memory
```

### Log Locations

- **Service Mode**: `/var/log/senhub-agent/` (Linux) or Event Viewer (Windows)
- **Console Mode**: Standard output with timestamps
- **Debug Logs**: Real-time with module identification

## Migration and Compatibility

### From Online to Offline Mode

1. **Export Configuration** (if needed)
2. **Stop Online Agent**
3. **Install Offline Agent**
4. **Migrate Custom Settings**
5. **Update Monitoring Tools** to use new endpoints

### From Offline to Online Mode

1. **Stop Offline Agent**
2. **Obtain SenHub Agent Key**
3. **Install Online Agent**
4. **Configure Probes** in SenHub platform
5. **Remove Local Configuration**

### Version Compatibility

- **Agent Versions**: Offline mode available in v0.8.0+
- **Configuration Format**: Forward compatible
- **API Endpoints**: Stable across versions
- **Certificate Format**: Standard X.509 PEM

## Performance and Limits

### Resource Usage
- **Memory**: ~50MB base + probe overhead
- **CPU**: <1% on modern systems
- **Disk**: ~10MB binary + logs + configuration
- **Network**: Local interface only (HTTP) or configurable (HTTPS)

### Scalability
- **Concurrent Connections**: 1000+ simultaneous API requests
- **Metric Storage**: 5-minute TTL, automatic cleanup
- **Probe Limit**: No hard limit (depends on system resources)
- **Data Retention**: In-memory only (no persistence)

### Recommendations
- **Production**: Use HTTPS mode with custom certificates
- **Development**: HTTP mode for simplicity
- **Monitoring**: Configure probe intervals based on requirements
- **Security**: Restrict network access via firewall rules

## Support and Resources

### Community Resources
- **GitHub Repository**: [SenHub Agent](https://github.com/senhub/agent)
- **Documentation**: [docs.senhub.io](https://docs.senhub.io)
- **Issue Tracker**: Report bugs and feature requests

### Enterprise Support
- **Commercial Support**: Available for enterprise customers
- **Professional Services**: Custom deployment and integration
- **Training**: Offline mode best practices and advanced configuration

### Contributing
- **Feature Requests**: Submit via GitHub Issues
- **Bug Reports**: Include logs and configuration
- **Pull Requests**: Follow contribution guidelines
- **Documentation**: Help improve this guide

---

**Last Updated**: January 2025  
**Version**: SenHub Agent v0.8.0+  
**License**: Commercial - SenHub Platform