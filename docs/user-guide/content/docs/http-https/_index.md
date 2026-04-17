---
title: "HTTP/HTTPS Configuration"
weight: 3
---

# HTTP/HTTPS Configuration

SenHub Agent exposes a REST API that monitoring systems (PRTG and Nagios) and administrators use to collect metrics, check agent health, and manage configuration. By default, the agent listens on HTTP port 8080.

## HTTP Mode (Default)

When you install the agent, the HTTP API is available immediately on port 8080 with no additional configuration:

```
http://agent-server:8080/api/{authentication-key}/
```

The default HTTP configuration in `agent-config.yaml`:

```yaml
storage:
  - name: http
    params:
      port: 8080
      bind_address: "0.0.0.0"
      endpoints: ["prtg", "web", "nagios"]
```

### Storage Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `port` | `8080` | TCP port for the HTTP API |
| `bind_address` | `0.0.0.0` | Network interface to bind to (use `127.0.0.1` for local access only) |
| `endpoints` | `["prtg", "web"]` | Enabled endpoint types (prtg, web, nagios) |

To change the port or other parameters, edit the `storage` section in `agent-config.yaml`. The change is applied automatically without restarting the service.

## HTTPS Mode

For production environments, enable HTTPS to encrypt API traffic between the agent and your monitoring system.

### Enabling HTTPS During Installation

The simplest way to enable HTTPS is during installation:

**Windows:**
```powershell
.\senhub-agent.exe install --offline --enable-https
```

**Linux:**
```bash
sudo /opt/senhub/bin/senhub-agent install --offline --enable-https
```

The agent automatically generates a self-signed certificate valid for 365 days and saves it in a `certs/` subdirectory:

- `certs/agent-cert.pem` (certificate)
- `certs/agent-key.pem` (private key)

The default HTTPS port is 8443.

### HTTPS Installation Options

| Flag | Default | Description |
|------|---------|-------------|
| `--enable-https` | - | Enable HTTPS for the agent API |
| `--https-port` | `8443` | HTTPS listening port |
| `--https-hosts` | `localhost,127.0.0.1` | Hostnames included in the certificate SAN (comma-separated) |
| `--cert-file` | auto-generated | Path to a custom TLS certificate (skips auto-generation) |
| `--key-file` | auto-generated | Path to a custom TLS private key (skips auto-generation) |
| `--min-tls-version` | `1.2` | Minimum TLS version (1.2 or 1.3) |

Example with custom hostnames for the certificate:

```bash
sudo /opt/senhub/bin/senhub-agent install --offline \
  --enable-https \
  --https-hosts "agent.company.com,192.168.1.100" \
  --https-port 8443
```

The generated certificate will include `agent.company.com` and `192.168.1.100` as Subject Alternative Names, so your monitoring system can connect using either the hostname or the IP address without certificate warnings.

### Manual HTTPS Configuration

You can also configure HTTPS directly in `agent-config.yaml`:

```yaml
storage:
  - name: http
    params:
      port: 8443
      endpoints: ["prtg", "web", "nagios"]
      tls:
        enabled: true
        min_tls_version: "1.2"
        cert_file: "/opt/senhub/certs/agent-cert.pem"
        key_file: "/opt/senhub/certs/agent-key.pem"
```

### TLS Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `tls.enabled` | `false` | Enable HTTPS |
| `tls.min_tls_version` | `1.2` | Minimum TLS version (1.2 or 1.3) |
| `tls.cert_file` | - | Absolute path to the certificate file (.pem or .crt) |
| `tls.key_file` | - | Absolute path to the private key file (.pem or .key) |

### Using a CA-Signed Certificate

If you have a certificate issued by your internal Certificate Authority:

```yaml
storage:
  - name: http
    params:
      port: 8443
      tls:
        enabled: true
        cert_file: "/etc/pki/tls/certs/senhub-agent.crt"
        key_file: "/etc/pki/tls/private/senhub-agent.key"
```

### Verifying HTTPS

```bash
curl -k https://agent-server:8443/health
```

The `-k` flag is required for self-signed certificates. For CA-signed certificates, omit this flag.

Expected response:
```json
{"status":"ok","version":"0.1.80","uptime":"2h30m","probes_active":4,"metrics_cached":156}
```

## API Endpoints Reference

All API endpoints require the authentication key in the URL path, except `/health`.

### Health and System Information

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/health` | GET | No | Agent health check with basic status |
| `/api/{key}/info/system` | GET | Yes | Detailed system information (version, uptime, memory, CPU) |
| `/api/{key}/info/probes` | GET | Yes | List of configured probes and metric counts |
| `/api/{key}/info/endpoints` | GET | Yes | List of available API endpoints |
| `/api/{key}/info/tags/{probe}` | GET | Yes | Available tag values for a probe (useful for discovery) |
| `/api/{key}/info/schema/{probe}` | GET | Yes | Full metric schema for a probe with examples |

### Metrics Endpoints (Monitoring System Integration)

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/{key}/prtg/metrics/{probe}` | GET | Yes | Metrics in PRTG JSON format |
| `/api/{key}/prtg/probes` | GET | Yes | List of available PRTG probe names |
| `/api/{key}/nagios/metrics/{probe}` | GET | Yes | Metrics in Nagios plugin output format |
| `/api/{key}/nagios/checks` | GET | Yes | List of available Nagios checks |

### Configuration and Administration

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/{key}/config/probes` | GET | Yes | Active probe configuration |
| `/api/{key}/config/validate` | POST | Yes | Validate a probe configuration (dry run) |
| `/api/{key}/config/test` | POST | Yes | Test probe connectivity to a target system |
| `/api/{key}/license/status` | GET | Yes | License details, tier, and authorized probes |
| `/api/{key}/admin/cache/clear` | POST | Yes | Clear the metrics cache |
| `/api/{key}/stats/cache` | GET | Yes | Cache statistics (size, memory usage) |

### Debug Endpoints

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/{key}/debug/logs` | GET | Yes | Current log levels per module |
| `/api/{key}/debug/logs` | POST | Yes | Change log levels at runtime (no restart) |
| `/api/{key}/debug/cache` | GET | Yes | Current cache contents |

### Web Interface

| Endpoint | Description |
|----------|-------------|
| `/web/{key}/` | Main dashboard |
| `/web/{key}/dashboard` | Dashboard (same as above) |
| `/web/{key}/explorer` | Sensor Builder (interactive API testing) |
| `/web/{key}/docs` | Embedded documentation |

### PRTG Lookups

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/{key}/lookups` | GET | List all available lookup definitions |
| `/api/{key}/lookups/prtg` | GET | Download all PRTG lookups as a ZIP file |
| `/api/{key}/lookups/prtg/{lookup_id}` | GET | Download a specific lookup as XML |

## API Response Examples

### Health Response

```bash
curl http://localhost:8080/health
```

```json
{
  "status": "ok",
  "version": "0.1.80",
  "commit": "a1b2c3d",
  "uptime": "2h30m15s",
  "probes_active": 4,
  "metrics_cached": 156
}
```

### Probes Information

```bash
curl http://localhost:8080/api/{key}/info/probes
```

```json
{
  "probes": ["CPU", "Memory", "Citrix Production", "NetScaler LB"],
  "probe_metrics": {
    "CPU": 4,
    "Memory": 3,
    "Citrix Production": 85,
    "NetScaler LB": 64
  },
  "total_metrics": 156
}
```

### System Information

```bash
curl http://localhost:8080/api/{key}/info/system
```

```json
{
  "status": "running",
  "version": "0.1.80",
  "os": "linux",
  "arch": "amd64",
  "port": 8080,
  "uptime": "2h30m15s",
  "health": {
    "status": "healthy",
    "services": {
      "http_server": "running",
      "cache": "running",
      "mode": "offline"
    }
  },
  "cache": {
    "total_metrics": 156,
    "ttl": "5m0s",
    "memory_usage": "2.45 MB"
  },
  "resources": {
    "memory_usage_mb": 45.67,
    "cpu_percent": 2.5,
    "goroutines": 42
  }
}
```

### PRTG Metrics Response

```bash
curl http://localhost:8080/api/{key}/prtg/metrics/CPU
```

```json
{
  "prtg": {
    "result": [
      {
        "channel": "CPU Usage",
        "value": 45.2,
        "float": 1,
        "unit": "Percent"
      },
      {
        "channel": "CPU Queue Length",
        "value": 2,
        "unit": "Count"
      }
    ]
  }
}
```

For status-type metrics, PRTG lookups are used instead of numeric values:

```json
{
  "prtg": {
    "result": [
      {
        "channel": "Service State",
        "value": 1,
        "valuelookup": "senhub.netscaler.lbvserver.state"
      }
    ]
  }
}
```

### PRTG Metrics with Tag Filtering

You can filter PRTG metrics by tag values using query parameters:

```bash
curl "http://localhost:8080/api/{key}/prtg/metrics/NetScaler%20LB?tags=vserver_name:vs_web,vs_api"
```

This returns metrics only for the specified virtual servers.

### Nagios Response

```bash
curl http://localhost:8080/api/{key}/nagios/metrics/CPU
```

```
OK - CPU has 4 metrics | cpu_usage=45.2% cpu_queue=2
```

The response follows the standard Nagios plugin output format: `STATUS - message | performance_data`.

## Firewall Configuration

The agent port must be accessible from your monitoring system.

**Windows:**
```powershell
netsh advfirewall firewall add rule name="SenHub Agent" dir=in action=allow protocol=TCP localport=8080
```

**Linux (Ubuntu/Debian):**
```bash
sudo ufw allow 8080/tcp
```

**Linux (RHEL/CentOS):**
```bash
sudo firewall-cmd --permanent --add-port=8080/tcp
sudo firewall-cmd --reload
```

For HTTPS, replace `8080` with your configured HTTPS port (default: `8443`).

## Security Recommendations

- Use HTTPS in production environments
- Bind the agent to a specific interface (`bind_address: "127.0.0.1"`) when remote access is not needed
- Restrict firewall rules to allow only your monitoring system's IP address
- Keep the authentication key confidential (it provides full API access)
- Use TLS 1.2 or higher (the agent defaults to TLS 1.2 minimum)
- For self-signed certificates, distribute the CA certificate to your monitoring system rather than disabling certificate verification
