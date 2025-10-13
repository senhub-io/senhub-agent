# SenHub Agent Documentation

Welcome to the SenHub Agent documentation. This documentation is organized into different sections based on your needs.

## 📚 Documentation Structure

### 👤 [User Guide](./user-guide/)
Documentation for end users and basic configuration:
- [Quick Start (Offline Mode)](./user-guide/QUICK-START-OFFLINE.md) - Get started quickly with offline mode
- [Offline Mode](./user-guide/OFFLINE-MODE.md) - Complete offline mode configuration
- [Probe Configuration](./user-guide/PROBE-CONFIGURATION.md) - Configure monitoring probes

### ⚙️ [Admin Guide](./admin-guide/)
Documentation for system administrators and advanced configuration:
- [HTTP Strategy](./admin-guide/HTTP-STRATEGY.md) - HTTP monitoring strategy configuration
- [HTTPS Configuration](./admin-guide/HTTPS-CONFIGURATION.md) - SSL/TLS and HTTPS setup
- [HTTP Bind Address](./admin-guide/HTTP-BIND-ADDRESS.md) - Network binding configuration
- [Logging](./admin-guide/LOGGING.md) - Logging configuration and management
- [Universal Configuration API](./admin-guide/UNIVERSAL-CONFIGURATION.md) - Configuration validation API
- [Config Version Changelog](./admin-guide/CONFIG-VERSION-CHANGELOG.md) - Configuration format changes

### 📊 [Probe Documentation](./probes/)
Specific documentation for monitoring probes:

- **[System Probes](./probes/system/)** - System resource monitoring (CPU, Memory, Network, Disk)
  - **[CPU](./probes/system/cpu/)** - Processor monitoring
    - [README](./probes/system/cpu/README.md) - Overview and configuration
    - [Metrics](./probes/system/cpu/METRICS.md) - Complete metrics reference
  - **[Memory](./probes/system/memory/)** - RAM and swap monitoring
    - [README](./probes/system/memory/README.md) - Overview and configuration
    - [Metrics](./probes/system/memory/METRICS.md) - Complete metrics reference
  - **[Network](./probes/system/network/)** - Network interface monitoring
    - [README](./probes/system/network/README.md) - Overview and configuration
    - [Metrics](./probes/system/network/METRICS.md) - Complete metrics reference
  - **[LogicalDisk](./probes/system/logicaldisk/)** - Disk and filesystem monitoring
    - [README](./probes/system/logicaldisk/README.md) - Overview and configuration
    - [Metrics](./probes/system/logicaldisk/METRICS.md) - Complete metrics reference

- **[Network Probes](./probes/network/)** - Network connectivity and quality monitoring
  - **[Gateway Ping](./probes/network/)** - Default gateway connectivity
    - [README](./probes/network/README.md) - Overview and configuration
    - [Metrics](./probes/network/METRICS.md) - Complete metrics reference
  - **[WiFi Signal Strength](./probes/network/wifi/)** - WiFi signal quality monitoring
    - [README](./probes/network/wifi/README.md) - Overview and configuration
    - [Metrics](./probes/network/wifi/METRICS.md) - Complete metrics reference

- **[WebApp Probes](./probes/webapp/)** - Web application monitoring
  - **[Ping](./probes/webapp/)** - HTTP/HTTPS availability monitoring
    - [README](./probes/webapp/PING-README.md) - Overview and configuration
    - [Metrics](./probes/webapp/PING-METRICS.md) - Complete metrics reference
  - **[Load](./probes/webapp/)** - Web application performance monitoring
    - [README](./probes/webapp/LOAD-README.md) - Overview and configuration
    - [Metrics](./probes/webapp/LOAD-METRICS.md) - Complete metrics reference

- **[Event Probes](./probes/events/)** - Event collection and log aggregation
  - **[Syslog](./probes/events/)** - Syslog server (RFC 3164/5424)
    - [README](./probes/events/README.md) - Overview and configuration
    - [Metrics](./probes/events/METRICS.md) - Complete event reference
  - **[Event](./probes/events/)** - Custom HTTP event endpoint
    - [README](./probes/events/EVENT-README.md) - Overview and configuration
    - [Metrics](./probes/events/EVENT-METRICS.md) - Complete event reference

- **[Citrix CVAD](./probes/citrix/)** - Citrix Virtual Apps and Desktops monitoring
  - [README](./probes/citrix/README.md) - Overview and setup
  - [Metrics](./probes/citrix/METRICS.md) - Complete metrics reference
  - [Debug Mode](./probes/citrix/DEBUG-MODE.md) - Debugging guide
  - [Site Filtering](./probes/citrix/SITE_FILTERING_PLAN.md) - Multi-site filtering

- **[Redfish](./probes/redfish/)** - Hardware monitoring via Redfish API
  - [README](./probes/redfish/README.md) - Redfish probe overview
  - [Metrics](./probes/redfish/METRICS.md) - Complete metrics reference
  - [Tags](./probes/redfish/REDFISH-TAGS.md) - Redfish tagging system
  - [Tag Enhancement](./probes/redfish/REDFISH-TAG-ENHANCEMENT.md) - Advanced tagging

- **[OpenTelemetry](./probes/otel/)** - OTEL metrics collection
  - [README](./probes/otel/README.md) - Overview and configuration
  - [Metrics](./probes/otel/METRICS.md) - Complete metrics reference

### 🚨 [Troubleshooting](./troubleshooting/)
Troubleshooting guides and common issues:
- [Offline Mode Issues](./troubleshooting/TROUBLESHOOTING-OFFLINE.md) - Solve offline mode problems

### 📁 [Archive](./archive/)
Historical documentation and development notes (for reference only)

### 🤖 [Claude Development Notes](./Claude/)
AI-assisted development documentation and personal configuration

## 📖 Additional Resources

- [Main README](../README.markdown) - Project overview and basic setup
- [Documentation Index](./DOCUMENTATION-INDEX.md) - Complete documentation index
- [CLAUDE.md](../CLAUDE.md) - Development guide and architecture

## 🚀 Quick Navigation

| I want to... | Go to... |
|--------------|----------|
| Get started quickly | [Quick Start Guide](./user-guide/QUICK-START-OFFLINE.md) |
| Configure probes | [Probe Configuration](./user-guide/PROBE-CONFIGURATION.md) |
| Monitor system resources (CPU, RAM, Network, Disk) | [System Probes](./probes/system/) |
| Monitor network connectivity (Gateway, WiFi) | [Network Probes](./probes/network/) |
| Monitor web applications (Ping, Load) | [WebApp Probes](./probes/webapp/) |
| Collect events and logs (Syslog, Custom) | [Event Probes](./probes/events/) |
| Monitor Citrix CVAD | [Citrix Documentation](./probes/citrix/) |
| Monitor hardware (servers) | [Redfish Documentation](./probes/redfish/) |
| Integrate OpenTelemetry | [OTEL Documentation](./probes/otel/) |
| Setup HTTPS | [HTTPS Configuration](./admin-guide/HTTPS-CONFIGURATION.md) |
| Fix offline issues | [Offline Troubleshooting](./troubleshooting/TROUBLESHOOTING-OFFLINE.md) |
| Validate configurations | [Universal Configuration API](./admin-guide/UNIVERSAL-CONFIGURATION.md) |

## 📝 Contributing to Documentation

When adding new documentation:
1. Place it in the appropriate category folder
2. Update this README with a link to your new document
3. Follow the existing naming conventions
4. Include clear titles and section headers
5. Add cross-references to related documentation

### Documentation Categories
- **user-guide/** - For end users (installation, basic usage)
- **admin-guide/** - For administrators (advanced config, APIs)
- **probes/** - For probe-specific documentation
- **troubleshooting/** - For problem-solving guides
- **archive/** - For historical reference only

---

*This documentation is organized to help you find information quickly based on your role and needs.*