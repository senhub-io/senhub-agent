# Citrix Virtual Apps and Desktops (CVAD) Probe

The Citrix probe monitors Citrix Virtual Apps and Desktops environments through the Director OData API and DDC REST API, providing comprehensive metrics for sessions, infrastructure, logon performance, and connection failures.

## Quick Start

### Basic Configuration

```yaml
probes:
  - name: citrix
    params:
      base_url: "https://citrix-director.company.com"
      interval: 120  # 2 minutes recommended
      auth:
        username: "DOMAIN\\svc-monitoring"
        password: "secure-password"
      tls:
        verify_ssl: true
```

### With Site Filtering (Multi-Site Deployments)

```yaml
probes:
  - name: citrix
    params:
      base_url: "https://citrix-director.company.com"
      delivery_controller:
        url: "https://citrix-ddc.company.com"
        site_filter: "PROD"  # Filter by specific site
      interval: 120
      auth:
        username: "DOMAIN\\svc-monitoring"
        password: "secure-password"
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

## Authentication

The probe supports automatic authentication method selection:
- **Director OData API**: NTLM authentication
- **DDC REST API**: Basic authentication (when site filtering enabled)

## Requirements

- Service account with Citrix monitoring permissions
- Network access to Citrix Director (port 443)
- Optional: DDC access for site filtering (port 443)

## Monitoring Integration

- **PRTG**: `http://agent:8080/api/{key}/prtg/metrics`
- **Nagios**: `http://agent:8080/api/{key}/nagios/metrics`
- **Web Interface**: `http://agent:8080/web/{key}/dashboard`

## Support

For issues or questions:
1. Check [troubleshooting documentation](../../troubleshooting/)
2. Review [production fixes](../../CITRIX-PRODUCTION-FIXES.md)
3. Enable debug logging: `--verbose --debug-modules probe.citrix`