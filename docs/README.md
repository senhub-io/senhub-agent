# SenHub Agent Documentation

Welcome to the SenHub Agent documentation. This documentation is organized into different sections based on your needs.

## 📚 Documentation Structure

### 👤 [User Guide](./user-guide/)
Documentation for end users and basic configuration:
- [Probe Configuration](./user-guide/docs/configuration.md) - Configure monitoring probes

### ⚙️ [Admin Guide](./admin-guide/)
Documentation for system administrators and advanced configuration:
- [HTTP Strategy](./admin-guide/HTTP-STRATEGY.md) - HTTP monitoring strategy configuration
- [HTTPS Configuration](./admin-guide/HTTPS-CONFIGURATION.md) - SSL/TLS and HTTPS setup
- [HTTP Bind Address](./admin-guide/HTTP-BIND-ADDRESS.md) - Network binding configuration
- [Logging](./admin-guide/LOGGING.md) - Logging configuration and management
- [Universal Configuration API](./admin-guide/UNIVERSAL-CONFIGURATION.md) - Configuration validation API
- [Config Version Changelog](./admin-guide/CONFIG-VERSION-CHANGELOG.md) - Configuration format changes

### Probe Documentation
The current probe pages (parameters, metrics, examples for every probe) are
part of the [User Guide](./user-guide/docs/probes/index.md). The pages below are
older, longer reference documents kept for the probes they cover:

- System: [CPU](./probes/system/cpu/README.md), [Memory](./probes/system/memory/README.md), [Network](./probes/system/network/README.md), [LogicalDisk](./probes/system/logicaldisk/README.md)
- Network: [Gateway ping](./probes/ping_gateway/README.md), [WiFi signal strength](./probes/wifi_signal_strength/README.md)
- Web applications: [Ping](./probes/ping_webapp/README.md), [Load](./probes/load_webapp/README.md)
- Events: [Syslog](./probes/syslog/README.md), [Event](./probes/event/README.md), [Windows event log](./probes/windows_eventlog/README.md)
- [Citrix CVAD](./probes/citrix/README.md), [NetScaler ADC](./probes/netscaler/README.md), [Redfish](./probes/redfish/README.md)

### 👨‍💻 [Developer Guide](./developer-guide/)
Documentation for contributors and developers:
- [Overview](./developer-guide/README.md) - Developer guide home
- [Architecture](./developer-guide/architecture.md) - System design and components
- [Development Workflow](./developer-guide/development-workflow.md) - Git workflow and branching
- [Build System](./developer-guide/build-system.md) - Compilation and testing
- [Design Patterns](./developer-guide/design-patterns.md) - Code patterns and best practices
- [Current Development](./developer-guide/current-development.md) - Where the line stands
- [Engineering](./developer-guide/engineering/) - Technical deep-dives

### 📦 [Releases](./releases/)
Release notes and version history:
- [All Releases](./releases/README.md) - Release index and guidelines
- [Release notes](./user-guide/docs/whats-new/index.md) - Current release notes (this directory archives up to 0.3.2)

### 📽️ [Presentations](./presentations/)
Technical presentations and demos:
- [Redfish Client Demo](./presentations/redfish-client-demo.md) - Redfish implementation showcase

### 🚨 [Troubleshooting](./troubleshooting/)
Troubleshooting guides and common issues:
- [Troubleshooting](./troubleshooting/TROUBLESHOOTING.md)

### 📁 [Archive](./archive/)
Historical documentation and development notes (for reference only)

## 📖 Additional Resources

- [Main README](../README.md) - Project overview and basic setup
- [Documentation Index](./DOCUMENTATION-INDEX.md) - Complete documentation index
- [CLAUDE.md](../CLAUDE.md) - Development guide and architecture
- [License System](./LICENSE-SYSTEM.md) - JWT-based license validation and management

## 🚀 Quick Navigation

| I want to... | Go to... |
|--------------|----------|
| Configure probes | [Probe Configuration](./user-guide/docs/configuration.md) |
| Monitor system resources (CPU, RAM, Network, Disk) | [System Probes](./probes/system/) |
| Monitor network connectivity (Gateway, WiFi) | [Gateway ping](./probes/ping_gateway/README.md) |
| Monitor web applications (Ping, Load) | [WebApp ping](./probes/ping_webapp/README.md) |
| Collect events and logs (Syslog, Custom) | [Syslog](./probes/syslog/README.md) |
| Monitor Citrix CVAD | [Citrix Documentation](./probes/citrix/) |
| Monitor NetScaler ADC (load balancers) | [NetScaler Documentation](./probes/netscaler/) |
| Monitor hardware (servers) | [Redfish Documentation](./probes/redfish/) |
| Setup HTTPS | [HTTPS Configuration](./admin-guide/HTTPS-CONFIGURATION.md) |
| Manage licenses | [License System](./LICENSE-SYSTEM.md) |
| Fix issues | [Troubleshooting](./troubleshooting/TROUBLESHOOTING.md) |
| Validate configurations | [Universal Configuration API](./admin-guide/UNIVERSAL-CONFIGURATION.md) |
| Contribute to development | [Developer Guide](./developer-guide/) |
| Check release notes | [Releases](./releases/) |

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
- **developer-guide/** - For contributors and developers
- **releases/** - For version history and release notes
- **presentations/** - For technical presentations
- **troubleshooting/** - For problem-solving guides
- **archive/** - For historical reference only
- **.internal/** - For internal tools and maintenance (not synced to wiki)

---

*This documentation is organized to help you find information quickly based on your role and needs.*