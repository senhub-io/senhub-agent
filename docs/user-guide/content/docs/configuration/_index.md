---
title: "Configuration"
weight: 2
---

# Configuration

SenHub Agent uses a YAML configuration file (`agent-config.yaml`) to define monitoring probes, data storage, and agent settings. Changes to this file are detected automatically and applied without restarting the service.

## Configuration File Location

The configuration file `agent-config.yaml` is located in the agent's installation directory:

- **Windows**: Same directory as the agent binary (e.g., `C:\SenHub\agent-config.yaml`)
- **Linux**: Same directory as the agent binary (e.g., `/opt/senhub/bin/agent-config.yaml`)

You can specify a custom path during installation:
```bash
senhub-agent install --offline --config-path /etc/senhub/agent-config.yaml
```

## Configuration Modes

SenHub Agent supports two operating modes:

**Online mode** (default): The agent connects to the SenHub platform and receives its configuration remotely. Probes and settings are managed centrally from the SenHub platform.

**Offline mode**: The agent reads its configuration entirely from the local `agent-config.yaml` file. This mode is for air-gapped environments or local testing. This documentation focuses on offline mode configuration.

## Configuration Structure Overview

The configuration file has four main sections:

```yaml
config_version: 2

agent:
  key: "550e8400-e29b-41d4-a716-446655440000"
  mode: offline

storage:
  - name: http
    params:
      port: 8080
      endpoints: ["prtg", "web", "nagios"]

cache:
  retention_minutes: 5

probes:
  - name: "Server CPU"
    type: cpu
    params:
      interval: 60
```

## Agent Section

The `agent` section defines the agent identity and operating mode.

| Parameter | Required | Description |
|-----------|----------|-------------|
| `key` | Yes | Authentication key (UUID format), provided by SenHub support |
| `mode` | Yes | `offline` for local configuration, `online` for remote management |
| `license` | No | License token for premium probes (see License section) |

## Probes Section

Each probe entry defines a monitoring target. The agent collects metrics at regular intervals from the configured probes.

### Probe Parameters

| Parameter | Required | Description |
|-----------|----------|-------------|
| `name` | Yes | Unique display name for this probe instance |
| `type` | Yes | Probe type (see Available Probe Types below) |
| `params` | Yes | Probe-specific parameters |
| `custom_tags` | No | Additional key-value tags attached to all metrics from this probe |

### Available Probe Types

| Type | License | Description |
|------|---------|-------------|
| `cpu` | Free | CPU utilization |
| `memory` | Free | Memory usage (physical and swap) |
| `logicaldisk` | Free | Disk space and I/O metrics |
| `network` | Free | Network interface metrics (bandwidth, errors, packets) |
| `veeam` | Pro | Veeam Backup & Replication v13 monitoring (via REST API) |
| `citrix` | Pro | Citrix Virtual Apps and Desktops monitoring (via Director API) |
| `netscaler` | Pro | Citrix ADC / NetScaler monitoring (via NITRO API) |
| `redfish` | Pro | Hardware monitoring via Redfish API (Dell iDRAC, HPE iLO, etc.) |
| `ping_webapp` | Pro | Web application availability check |
| `load_webapp` | Pro | Web page load time measurement |
| `ping_gateway` | Pro | Network gateway connectivity monitoring |
| `syslog` | Pro | Syslog message collection (UDP/TCP) |
| `event` | Pro | Custom event collection via HTTP |
| `wifi_signal_strength` | Pro | WiFi signal quality monitoring |
| `otel` | Enterprise | OpenTelemetry metrics collection |

### Common Probe Parameters

All probes support the following parameters in the `params` section:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `interval` | `60` | Collection interval in seconds |
| `timeout` | `30` | Request timeout in seconds |

### Custom Tags

You can attach custom tags to all metrics from a specific probe. Tags are included in the metric output for filtering and grouping in your monitoring system:

```yaml
probes:
  - name: "Production CPU"
    type: cpu
    params:
      interval: 60
    custom_tags:
      environment: "production"
      location: "datacenter-paris"
      team: "infrastructure"
```

Tags appear in PRTG, Nagios, and other monitoring tool outputs, allowing you to filter and organize your metrics.

## Storage Section

The `storage` section defines how the agent exposes collected metrics. The main storage type is `http`, which provides the REST API and web dashboard.

```yaml
storage:
  - name: http
    params:
      port: 8080
      bind_address: "0.0.0.0"
      endpoints: ["prtg", "web", "nagios"]
```

### HTTP Storage Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `port` | `8080` | TCP port for the HTTP API |
| `bind_address` | `0.0.0.0` | Network interface to bind to (use `127.0.0.1` to restrict to localhost) |
| `endpoints` | `["prtg", "web"]` | Enabled endpoint types |

### Available Endpoint Types

| Endpoint | Description |
|----------|-------------|
| `prtg` | PRTG-formatted JSON API for PRTG Network Monitor integration |
| `web` | Built-in web dashboard (Dashboard, Sensor Builder, Documentation) |
| `nagios` | Nagios-compatible check output |

### HTTPS Configuration

To enable HTTPS, add a `tls` section to the HTTP storage parameters:

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

If you installed with `--enable-https`, the agent generated self-signed certificates automatically in the `certs/` directory. You can replace them with your own certificates.

| TLS Parameter | Default | Description |
|---------------|---------|-------------|
| `enabled` | `false` | Enable HTTPS |
| `min_tls_version` | `1.2` | Minimum TLS version (1.2 or 1.3) |
| `cert_file` | - | Path to TLS certificate file (.pem or .crt) |
| `key_file` | - | Path to TLS private key file (.pem or .key) |

## Cache Section

The `cache` section controls how long collected metrics are kept in memory before being discarded.

```yaml
cache:
  retention_minutes: 5
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `retention_minutes` | `5` | Number of minutes to keep metrics in cache |

Monitoring systems (PRTG, Nagios, etc.) read metrics from the cache. Set the retention period longer than the longest polling interval of your monitoring system.

## Configuration Examples

### Minimal Offline Configuration

The simplest configuration with system monitoring probes:

```yaml
config_version: 2

agent:
  key: "550e8400-e29b-41d4-a716-446655440000"
  mode: offline

probes:
  - name: "CPU"
    type: cpu
    params:
      interval: 60

  - name: "Memory"
    type: memory
    params:
      interval: 60
```

### System Monitoring Configuration

A complete system monitoring setup with all free-tier probes:

```yaml
config_version: 2

agent:
  key: "550e8400-e29b-41d4-a716-446655440000"
  mode: offline

storage:
  - name: http
    params:
      port: 8080
      endpoints: ["prtg", "web", "nagios"]

cache:
  retention_minutes: 5

probes:
  - name: "CPU"
    type: cpu
    params:
      interval: 60

  - name: "Memory"
    type: memory
    params:
      interval: 60

  - name: "Disk"
    type: logicaldisk
    params:
      interval: 60

  - name: "Network"
    type: network
    params:
      interval: 60
```

### Production Configuration with Premium Probes

A production setup monitoring Citrix and NetScaler infrastructure:

```yaml
config_version: 2

agent:
  key: "550e8400-e29b-41d4-a716-446655440000"
  mode: offline
  license: "SH-XXXXXX-XXXXXX-XXXXXX-XXXXXX-XXXXXX-XX"

storage:
  - name: http
    params:
      port: 8443
      endpoints: ["prtg", "web", "nagios"]
      tls:
        enabled: true
        min_tls_version: "1.2"

cache:
  retention_minutes: 5

probes:
  - name: "CPU"
    type: cpu
    params:
      interval: 60

  - name: "Memory"
    type: memory
    params:
      interval: 60

  - name: "Disk"
    type: logicaldisk
    params:
      interval: 60

  - name: "Network"
    type: network
    params:
      interval: 60

  - name: "Citrix Production"
    type: citrix
    params:
      base_url: "https://director.company.com"
      interval: 120
      auth:
        username: "DOMAIN\\svc-monitoring"
        password: "SecurePassword123"
      tls:
        verify_ssl: true
      timeout: 30
    custom_tags:
      environment: "production"
      site: "paris"

  - name: "NetScaler LB"
    type: netscaler
    params:
      base_url: "https://netscaler.company.com"
      username: "monitoring-user"
      password: "SecurePassword123"
      interval: 60
    custom_tags:
      environment: "production"

  - name: "Hardware iDRAC"
    type: redfish
    params:
      base_url: "https://idrac-server01.company.com"
      username: "monitoring"
      password: "SecurePassword123"
      interval: 300
      tls:
        verify_ssl: false
```

### Web Application Monitoring Configuration

Monitoring web application availability and load times:

```yaml
config_version: 2

agent:
  key: "550e8400-e29b-41d4-a716-446655440000"
  mode: offline
  license: "SH-XXXXXX-XXXXXX-XXXXXX-XXXXXX-XXXXXX-XX"

probes:
  - name: "Website Availability"
    type: ping_webapp
    params:
      url: "https://www.company.com"
      interval: 30
      timeout: 10

  - name: "Website Load Time"
    type: load_webapp
    params:
      url: "https://www.company.com"
      interval: 60
      timeout: 30

  - name: "Gateway Paris"
    type: ping_gateway
    params:
      target: "192.168.1.1"
      interval: 30
```

## Applying Configuration Changes

The agent watches the configuration file for changes. When you modify `agent-config.yaml`, the changes are detected and applied automatically within a few seconds. **No service restart is required.**

This applies to:

- Adding, removing, or modifying probes
- Changing probe parameters (intervals, credentials, URLs)
- Modifying storage configuration (port, endpoints, TLS)
- Adding or removing custom tags

## License

### Free Tier

Without a license, the agent runs with free-tier probes only: `cpu`, `memory`, `logicaldisk`, `network`. These probes are always available regardless of license status.

### Obtaining a License

Contact SenHub support (support@senhub.io) to request a license token. Specify the probe types you need:

- **Pro license**: adds Citrix, NetScaler, Redfish, Ping, SNMP, Syslog, Event
- **Enterprise license**: all current and future probe types

### License Formats

SenHub supports two license formats:

**Compact key** (recommended): a short 40-character key bound to your agent key.
```yaml
agent:
  key: "17b3cf0a-91b1-486d-8209-90ffe00ece5e"
  license: "SH-040GMS-000100-02S3S2-HC3HMV-7RBZ4Y-PY"
```

**JWT token** (legacy): a longer token (~700 characters) starting with `eyJ`. Both formats are auto-detected and fully supported.

### Activating a License

Once you receive the license from support, activate it with:

```bash
senhub-agent license activate SH-040GMS-000100-02S3S2-HC3HMV-7RBZ4Y-PY
```

This validates the license, verifies the agent key binding, and saves it in the configuration file. The license takes effect automatically.

### Verifying License Status

You can check the license status at any time:

```bash
senhub-agent license show
```

Or via the API:
```bash
curl http://localhost:8080/api/{key}/license/status
```

Example API response:
```json
{
  "status": "active",
  "tier": "pro",
  "expires_at": "2026-06-30T23:59:59Z",
  "days_remaining": 120,
  "authorized_probes": ["cpu", "memory", "logicaldisk", "network", "citrix", "netscaler", "redfish", "ping_webapp", "syslog"],
  "free_tier_probes": ["cpu", "memory", "logicaldisk", "network"]
}
```

### License Tiers

| Tier | Available Probes |
|------|-----------------|
| **Free** | cpu, memory, logicaldisk, network |
| **Pro** | All free + veeam, citrix, netscaler, redfish, ping_webapp, load_webapp, ping_gateway, syslog, event, wifi_signal_strength |
| **Enterprise** | All probes (including future additions) |

### Grace Period

When a license expires, there is a 7-day grace period during which premium probes continue to work. After the grace period, the agent reverts to free-tier probes only.

### Other License Commands

```bash
senhub-agent license show                # Show current license details
senhub-agent license remove              # Remove license (reverts to free tier)
senhub-agent license remove --force      # Remove without confirmation prompt
```

## Auto-Update

The agent can check for new versions and optionally install them automatically.

```yaml
auto_update:
  enabled: false          # Automatic installation of new versions
  include_beta: false     # Include beta versions in update checks
  url: "https://eu-west-1.intake.senhub.io/releases"
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `enabled` | `false` | Automatically install new versions when available |
| `include_beta` | `false` | Include beta versions in update checks |
| `url` | SenHub releases | Update server URL |

Even with `enabled: false`, the agent checks for new versions at startup and logs a message if an update is available. Use `senhub-agent update --list` to see available versions and `senhub-agent update <version>` to install manually.

## Validating Configuration

Use the built-in configuration checker before deploying changes:

```bash
senhub-agent config check agent-config.yaml
```

This validates:
- YAML syntax (with line-level error context for syntax errors)
- Required fields and values
- License validity and agent key binding
- Probe types and required parameters
- Storage strategy names

Example output:
```
Checking configuration: C:\SenHub\agent-config.yaml

  [OK]   config_version: 2
  [OK]   agent.key: 17b3cf0a-91b1-486d-8209-90ffe00ece5e
  [OK]   agent.mode: offline
  [OK]   agent.license: compact format, tier=pro, expires=2031-04-14
  [OK]   License binding verified
  [OK]   1 probe(s) configured
  [OK]   Probe "veeam-prod" (type: veeam)
  [OK]   Storage: http

Configuration is valid.
```

## Security Recommendations

- Protect configuration file permissions: Windows (Administrators only), Linux (`chmod 600`)
- Use service accounts with minimal permissions for probe credentials (Citrix, NetScaler, Redfish)
- Use HTTPS in production for the agent API
- Never commit configuration files containing passwords to version control
- Bind the agent to a specific network interface (`bind_address: "127.0.0.1"`) when remote access is not needed

## Troubleshooting Configuration

### Configuration Not Loading

Check agent logs for errors:

- Windows: `%ProgramData%\SenHub\logs\senhubagent.log`
- Linux: `/var/log/senhub/senhubagent.log`

If there is a YAML syntax error, the agent keeps the previous valid configuration and logs the error.

### Probe Not Starting

Common causes:

- Invalid credentials (check logs for authentication errors)
- Network connectivity issues (verify the agent can reach the target system)
- License restrictions (verify the probe type is authorized in your license tier)
- Missing required parameters (check the probe-specific documentation)

Check probe-specific troubleshooting in the [Citrix Guide]({{< relref "/docs/probes/citrix" >}}) or [NetScaler Guide]({{< relref "/docs/probes/netscaler" >}}).
