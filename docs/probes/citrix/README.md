# Citrix Virtual Apps and Desktops (CVAD) Probe

The Citrix probe monitors Citrix Virtual Apps and Desktops environments through the Director OData API and DDC REST API, providing comprehensive metrics for sessions, infrastructure, logon performance, and connection failures.

## Quick Start

### Basic Configuration (Recommended)

```yaml
# probes.d/10-citrix.yaml — each file under probes.d/ is a YAML array of probes
- name: citrix
  params:
    interval: 120  # 2 minutes recommended
    timeout: 30

    director:
      url: "https://citrix-director.company.com"
      verify_ssl: true
      auth:
        username: "DOMAIN\\svc-monitoring"
        password: ${secret:citrix.password}   # OS secret store; inline plaintext is auto-sealed on install
```

### With Site Filtering and License Monitoring

```yaml
# probes.d/10-citrix.yaml
- name: citrix
  params:
    interval: 120
    timeout: 30

    director:
      url: "https://citrix-director.company.com"
      verify_ssl: false
      auth:
        username: "DOMAIN\\svc-monitoring"
        password: ${secret:citrix.director_password}   # OS secret store; inline plaintext is auto-sealed on install

    delivery_controller:         # Optional
      url: "https://citrix-ddc.company.com"
      fallback_urls:
        - "https://citrix-ddc2.company.com"
      verify_ssl: false
      site_filter: "PROD"
      auth:
        username: "DOMAIN\\svc-ddc"
        password: ${secret:citrix.ddc_password}   # OS secret store; inline plaintext is auto-sealed on install

    license_server:              # Optional
      url: "https://citrix-license-server:8083"
      verify_ssl: false
      auth:
        username: "DOMAIN\\svc-lic"
        password: ${secret:citrix.license_password}   # OS secret store; inline plaintext is auto-sealed on install
```

### Legacy Configuration (Deprecated)

The flat format with global `auth` and `tls` blocks is still supported but deprecated.
A deprecation warning will be logged at startup. See [Configuration Examples](./configuration-examples.yaml) for migration guidance.

```yaml
# probes.d/10-citrix.yaml
- name: citrix
  params:
    director_url: "https://citrix-director.company.com"
    interval: 120
    auth:
      username: "DOMAIN\\svc-monitoring"
      password: ${secret:citrix.password}   # OS secret store; inline plaintext is auto-sealed on install
    tls:
      verify_ssl: true
```

## Documentation Index

### Configuration
- **[Configuration Examples](./configuration-examples.yaml)** - Complete YAML examples for various deployment scenarios
- **[Site Filtering Setup](./SITE_FILTERING_PLAN.md)** - Multi-site monitoring configuration

### Advanced Features
- **[Debug Mode](./DEBUG-MODE.md)** - Identifier extraction and API analysis

### Architecture & Metrics
- **[Session Metrics Reference](../../technical-reference/CITRIX-SESSION-METRICS.md)** - Detailed session metrics documentation
- **[Complete Metrics Reference](../../CITRIX-METRICS-COMPLETE-REFERENCE.md)** - All available metrics with descriptions

### Troubleshooting & Analysis
- **[Production Fixes](../../CITRIX-PRODUCTION-FIXES.md)** - Common issues and solutions
- **[Session Analysis](../../CITRIX-SESSION-ANALYSIS.md)** - Session state analysis and debugging
- **[Advanced Metrics](../../CITRIX-ADVANCED-METRICS.md)** - Performance optimization insights

## Key Metrics Collected

### Sessions
- `sessions_connected` - Active user sessions
- `sessions_disconnected` - Suspended sessions
- `logon_duration_total` - Average logon time (2-minute window)
- `logon_duration_avg_1h` - 1-hour rolling average

### Infrastructure
- `machines_registered` / `machines_unregistered` - VDA capacity
- `machines_faulty` - Failed machines requiring attention
- Connection failure metrics by category

### Performance Phases
- Detailed logon phase breakdown (brokering, HDX, authentication, GPO, scripts, profile, interactive)

### Load Index
- `load_index_effective` - Average VDA load (0-100%)
- `load_index_cpu` / `memory` / `disk` / `network` - Per-component load
- `load_overloaded_machines` - Machines above 80% load

### Licensing (requires `license_server` or DDC with usage fields)
- `license_sessions_active` - Active licensed sessions
- `license_peak_concurrent` - Peak concurrent users
- `license_grace_period_active` - Grace period alert (0/1)

## Authentication

The probe supports automatic authentication method selection:
- **Director OData API**: NTLM authentication
- **DDC REST API**: Basic authentication (when site filtering enabled)

## Requirements

- Service account with Citrix monitoring permissions
- Network access to Citrix Director (port 443)
- Optional: DDC access for site filtering (port 443)
- Optional: License Server access for license monitoring (port 8083)

## Monitoring Integration

- **PRTG**: `http://agent:8080/api/{key}/prtg/metrics`
- **Nagios**: `http://agent:8080/api/{key}/nagios/metrics`
- **Web Interface**: `http://agent:8080/web/{key}/dashboard`

## Support

For issues or questions:
1. Check [troubleshooting documentation](../../troubleshooting/)
2. Review [production fixes](../../CITRIX-PRODUCTION-FIXES.md)
3. Enable debug logging: `--verbose --debug-modules probe.citrix`