# SenHub Agent Documentation

Welcome to the SenHub Agent documentation wiki. This guide provides comprehensive information for users, administrators, and developers.

## Quick Links

- **[Getting Started](./user-guide/GETTING-STARTED.md)** - Installation and first steps
- **[User Guide](./user-guide/)** - End-user documentation
- **[Admin Guide](./admin-guide/)** - Configuration and administration
- **[Developer Guide](./developer-guide/)** - Contributing and development
- **[Troubleshooting](./troubleshooting/)** - Common issues and solutions

## What is SenHub Agent?

SenHub Agent is a cross-platform monitoring agent that collects metrics and events from various sources and routes them to different storage and monitoring systems.

### Key Features

- **Multi-Probe Architecture**: CPU, memory, network, Redfish, Citrix, and more
- **Flexible Deployment**: Online (SenHub platform) or offline (standalone) modes
- **Multiple Integrations**: PRTG, Nagios, Grafana, SenHub, and custom monitoring systems
- **HTTPS/TLS Support**: Secure communication with certificate management
- **Web Interface**: Local dashboard for configuration and monitoring
- **Cross-Platform**: Windows, macOS, and Linux support

## Documentation Structure

### For Users
- **[Getting Started](./user-guide/GETTING-STARTED.md)** - Quick start guide
- **[Installation](./user-guide/INSTALLATION.md)** - Detailed installation instructions
- **[Offline Mode](./user-guide/OFFLINE-MODE.md)** - Standalone deployment guide
- **[Configuration](./user-guide/CONFIGURATION.md)** - Configuration overview

### For Administrators
- **[HTTPS Configuration](./admin-guide/HTTPS-CONFIGURATION.md)** - TLS/SSL setup
- **[Universal Configuration API](./admin-guide/UNIVERSAL-CONFIGURATION.md)** - Configuration validation
- **[Monitoring Integration](./admin-guide/)** - Integration with monitoring systems

### For Developers
- **[Architecture](./developer-guide/architecture.md)** - System design and components
- **[Development Workflow](./developer-guide/development-workflow.md)** - Git workflow and process
- **[Build System](./developer-guide/build-system.md)** - Compilation and testing
- **[Design Patterns](./developer-guide/design-patterns.md)** - Code patterns and best practices

### Probe Documentation
- **[Redfish Probe](./probes/redfish/)** - Server hardware monitoring
- **[Citrix Probe](./probes/citrix/)** - CVAD monitoring
- **[System Probes](./probes/system/)** - CPU, memory, disk, network
- **[Network Probes](./probes/network/)** - Gateway and webapp monitoring
- **[Event Probes](./probes/events/)** - Syslog and Windows events

### Release Information
- **[Release Notes](./releases/)** - Version history and changelogs
- **[Latest Release](./releases/0.1.66-beta.md)** - Current beta version

## Quick Start

### Online Mode (with SenHub Platform)
```bash
# Install with agent key from SenHub platform
./senhub-agent install --authentication-key YOUR_AGENT_KEY

# Start the agent
./senhub-agent start
```

### Offline Mode (Standalone)
```bash
# Install in offline mode
./senhub-agent install --offline

# Start the agent
./senhub-agent start

# Access web interface
# http://localhost:8080/web/{agentkey}/dashboard
```

### With HTTPS
```bash
# Install with HTTPS support
./senhub-agent install --offline --enable-https

# Access web interface
# https://localhost:8443/web/{agentkey}/dashboard
```

## Common Tasks

### Check Agent Status
```bash
./senhub-agent status
```

### View Logs
```bash
# Linux/Mac
tail -f /var/log/senhub-agent.log

# Windows
type "C:\ProgramData\SenHub\logs\senhubagent.log"
```

### Enable Debug Logging
```bash
./senhub-agent run --verbose --debug-modules probe.redfish,strategy.http
```

### Update Configuration
Edit the configuration file:
- **Online mode**: Configuration managed by SenHub platform
- **Offline mode**: Edit `agent-config.yaml`

## Support

### Documentation
- Browse this wiki for detailed information
- Check [Troubleshooting](./troubleshooting/) for common issues

### Community
- GitHub Issues: Report bugs and request features
- GitHub Discussions: Ask questions and share ideas

### Commercial Support
Contact SenHub for commercial support and enterprise features.

## Contributing

We welcome contributions! See the [Developer Guide](./developer-guide/) for:
- Development workflow
- Code style guidelines
- Testing requirements
- Pull request process

## License

See LICENSE file in the repository.

---

**Documentation Version**: 2025-11-06
**Agent Version**: 0.1.66-beta
