# SenHub Agent - Documentation Index

> **📌 Documentation Reorganized!** 
> 
> For better navigation, documentation has been reorganized into the **[docs/](./docs/)** directory with dedicated sections for different user types. This page remains as a comprehensive index of all documentation.
> 
> **🚀 Start here: [docs/README.md](./docs/README.md)** for organized documentation by user role.

Welcome to the comprehensive documentation for SenHub Agent. This index will help you find the right documentation for your needs.

## 🚀 Getting Started

### For New Users
1. **[Quick Start Guide (5 min)](user-guide/QUICK-START-OFFLINE.md)** - Get your agent running in 5 minutes
2. **[Offline Mode Guide](user-guide/OFFLINE-MODE.md)** - Complete standalone deployment documentation
3. **[Probe Configuration](user-guide/PROBE-CONFIGURATION.md)** - Configure monitoring probes

### For Production Deployment
1. **[HTTPS Configuration](admin-guide/HTTPS-CONFIGURATION.md)** - TLS/SSL setup and security
2. **[Troubleshooting Guide](troubleshooting/TROUBLESHOOTING-OFFLINE.md)** - Common issues and solutions
3. **[Universal Configuration API](admin-guide/UNIVERSAL-CONFIGURATION.md)** - Configuration validation

## 📚 User Documentation

### Core Guides
| Document | Description | Audience | Time to Read |
|----------|-------------|----------|--------------|
| **[QUICK-START-OFFLINE.md](user-guide/QUICK-START-OFFLINE.md)** | 5-minute setup guide | All users | 5 min |
| **[OFFLINE-MODE.md](user-guide/OFFLINE-MODE.md)** | Complete offline mode documentation | Users, Admins | 30 min |
| **[HTTPS-CONFIGURATION.md](admin-guide/HTTPS-CONFIGURATION.md)** | TLS/SSL configuration guide | Admins, DevOps | 20 min |
| **[TROUBLESHOOTING-OFFLINE.md](troubleshooting/TROUBLESHOOTING-OFFLINE.md)** | Troubleshooting and debugging | All users | 15 min |

### Configuration References
| Document | Description | Use Case |
|----------|-------------|----------|
| **[PROBE-CONFIGURATION.md](user-guide/PROBE-CONFIGURATION.md)** | Probe configuration guide | Custom monitoring |
| **[HTTP-STRATEGY.md](admin-guide/HTTP-STRATEGY.md)** | HTTP strategy configuration | API integration |
| **[HTTP-BIND-ADDRESS.md](admin-guide/HTTP-BIND-ADDRESS.md)** | Network binding configuration | Network setup |
| **[UNIVERSAL-CONFIGURATION.md](admin-guide/UNIVERSAL-CONFIGURATION.md)** | Configuration validation API | Config testing |

## 👨‍💻 Developer Documentation

### Core Development
| Document | Description | Audience |
|----------|-------------|----------|
| **[Developer Guide](developer-guide/README.md)** | Complete developer documentation | Developers |
| **[Architecture](developer-guide/architecture.md)** | System design and components | Developers |
| **[Development Workflow](developer-guide/development-workflow.md)** | Git workflow and branching | Contributors |
| **[Build System](developer-guide/build-system.md)** | Compilation and testing | Developers |
| **[Design Patterns](developer-guide/design-patterns.md)** | Code patterns and best practices | Developers |
| **[Current Development](developer-guide/current-development.md)** | Active work and roadmap | All |
| **[CLAUDE.md](../CLAUDE.md)** | Quick reference (points to developer guide) | Developers |
| **[LOGGING.md](admin-guide/LOGGING.md)** | Logging system and debugging | Developers, DevOps |

### Probe Documentation
| Probe | Documentation | Description |
|-------|---------------|-------------|
| **System Probes** | | System resource monitoring (CPU, Memory, Network, Disk) |
| **CPU** | [README](probes/system/cpu/README.md) | Processor monitoring (usage, load, per-core metrics) |
| | [Metrics](probes/system/cpu/METRICS.md) | Complete CPU metrics reference |
| **Memory** | [README](probes/system/memory/README.md) | RAM and swap monitoring |
| | [Metrics](probes/system/memory/METRICS.md) | Complete memory metrics reference |
| **Network** | [README](probes/system/network/README.md) | Network interface monitoring (bandwidth, packets, errors) |
| | [Metrics](probes/system/network/METRICS.md) | Complete network metrics reference |
| **LogicalDisk** | [README](probes/system/logicaldisk/README.md) | Disk and filesystem monitoring (space, IOPS, latency) |
| | [Metrics](probes/system/logicaldisk/METRICS.md) | Complete disk metrics reference |
| **Network Probes** | | Network connectivity and quality monitoring |
| **Gateway Ping** | [README](probes/network/README.md) | Default gateway connectivity (latency, packet loss) |
| | [Metrics](probes/network/METRICS.md) | Complete gateway metrics reference |
| **WiFi Signal** | [README](probes/wifi_signal_strength/README.md) | WiFi signal strength and quality monitoring |
| | [Metrics](probes/wifi_signal_strength/METRICS.md) | Complete WiFi metrics reference |
| **WebApp Probes** | | Web application monitoring |
| **WebApp Ping** | [README](probes/ping_webapp/README.md) | HTTP/HTTPS availability monitoring |
| | [Metrics](probes/ping_webapp/METRICS.md) | Complete ping metrics reference |
| **WebApp Load** | [README](probes/load_webapp/README.md) | Web application performance monitoring |
| | [Metrics](probes/load_webapp/METRICS.md) | Complete load metrics reference |
| **Event Probes** | | Event collection and log aggregation |
| **Syslog** | [README](probes/syslog/README.md) | Syslog server (RFC 3164/5424) |
| | [Metrics](probes/syslog/METRICS.md) | Complete event structure reference |
| **Event** | [README](probes/event/README.md) | Custom HTTP event endpoint |
| | [Metrics](probes/event/METRICS.md) | Complete event API reference |
| **Citrix CVAD** | [README](probes/citrix/README.md) | Citrix Virtual Apps and Desktops monitoring |
| | [Metrics](probes/citrix/METRICS.md) | Complete metrics reference |
| | [Debug Mode](probes/citrix/DEBUG-MODE.md) | Debugging guide |
| | [Site Filtering](probes/citrix/SITE_FILTERING_PLAN.md) | Multi-site filtering |
| **Redfish** | [README](probes/redfish/README.md) | Hardware monitoring via Redfish API |
| | [Metrics](probes/redfish/METRICS.md) | Complete metrics reference |
| | [Tags](probes/redfish/REDFISH-TAGS.md) | Redfish tagging system |
| | [Tag Enhancement](probes/redfish/REDFISH-TAG-ENHANCEMENT.md) | Advanced tagging |
| **OpenTelemetry** | [README](probes/otel/README.md) | Overview and configuration |
| | [Metrics](probes/otel/METRICS.md) | Complete metrics reference |

## 🎯 Quick Navigation by Use Case

### "I want to get started quickly"
1. **[QUICK-START-OFFLINE.md](user-guide/QUICK-START-OFFLINE.md)** (5 minutes)
2. **[PROBE-CONFIGURATION.md](user-guide/PROBE-CONFIGURATION.md)** (probe setup)

### "I need to deploy in production"
1. **[OFFLINE-MODE.md](user-guide/OFFLINE-MODE.md)** (comprehensive guide)
2. **[HTTPS-CONFIGURATION.md](admin-guide/HTTPS-CONFIGURATION.md)** (security setup)
3. **[LOGGING.md](admin-guide/LOGGING.md)** (logging configuration)

### "I have an issue to resolve"
1. **[TROUBLESHOOTING-OFFLINE.md](troubleshooting/TROUBLESHOOTING-OFFLINE.md)** (troubleshooting guide)
2. **[LOGGING.md](admin-guide/LOGGING.md)** (debugging information)
3. **[Developer Guide](developer-guide/README.md)** (development context)

### "I want to integrate with monitoring tools"
1. **[OFFLINE-MODE.md#api-endpoints](user-guide/OFFLINE-MODE.md#api-endpoints)** (API reference)
2. **[HTTP-STRATEGY.md](admin-guide/HTTP-STRATEGY.md)** (HTTP strategy details)
3. **[UNIVERSAL-CONFIGURATION.md](admin-guide/UNIVERSAL-CONFIGURATION.md)** (configuration validation)

### "I need hardware monitoring"
1. **[Redfish README](probes/redfish/README.md)** (Redfish overview)
2. **[Redfish Metrics](probes/redfish/METRICS.md)** (complete metrics reference)
3. **[REDFISH-TAGS.md](probes/redfish/REDFISH-TAGS.md)** (metric tagging)

### "I want to monitor system resources"
1. **[CPU Probe](probes/system/cpu/README.md)** (processor usage and load)
2. **[Memory Probe](probes/system/memory/README.md)** (RAM and swap)
3. **[Network Probe](probes/system/network/README.md)** (bandwidth and errors)
4. **[LogicalDisk Probe](probes/system/logicaldisk/README.md)** (disk space and IOPS)

### "I want to monitor network connectivity"
1. **[Gateway Ping](probes/network/README.md)** (local network latency and packet loss)
2. **[WiFi Signal](probes/network/wifi/README.md)** (wireless signal strength and quality)
3. **[WebApp Ping](probes/ping_webapp/README.md)** (internet connectivity)

### "I want to monitor web applications"
1. **[WebApp Ping](probes/ping_webapp/README.md)** (HTTP/HTTPS availability)
2. **[WebApp Load](probes/load_webapp/README.md)** (performance and response time)

### "I want to collect events and logs"
1. **[Syslog Probe](probes/syslog/README.md)** (syslog server for network devices)
2. **[Event Probe](probes/event/README.md)** (custom application events)

### "I want to monitor Citrix CVAD"
1. **[Citrix README](probes/citrix/README.md)** (overview and setup)
2. **[Citrix Metrics](probes/citrix/METRICS.md)** (complete metrics reference)
3. **[DEBUG-MODE.md](probes/citrix/DEBUG-MODE.md)** (debugging guide)

### "I want to customize monitoring"
1. **[PROBE-CONFIGURATION.md](user-guide/PROBE-CONFIGURATION.md)** (probe setup)
2. **[Developer Guide](developer-guide/README.md)** (development guide for custom probes)

## 🔍 Documentation by Topic

### Installation & Setup
- **[Quick Start](user-guide/QUICK-START-OFFLINE.md)** - 5-minute setup
- **[Complete Guide](user-guide/OFFLINE-MODE.md)** - Full installation documentation
- **[Probe Configuration](user-guide/PROBE-CONFIGURATION.md)** - Configure monitoring probes

### Security & HTTPS
- **[HTTPS Configuration](admin-guide/HTTPS-CONFIGURATION.md)** - Complete TLS setup guide
- **[Security Best Practices](admin-guide/HTTPS-CONFIGURATION.md#security-best-practices)** - Production security
- **[Certificate Management](admin-guide/HTTPS-CONFIGURATION.md#certificate-management)** - Certificate handling

### Monitoring & Probes
- **[System Probes](probes/system/)** - CPU, Memory, Network, Disk monitoring
  - **[CPU](probes/system/cpu/)** - Processor usage and load averages
  - **[Memory](probes/system/memory/)** - RAM and swap monitoring
  - **[Network](probes/system/network/)** - Network interface bandwidth and errors
  - **[LogicalDisk](probes/system/logicaldisk/)** - Disk space and I/O performance
- **[Network Probes](probes/network/)** - Network connectivity and quality
  - **[Gateway Ping](probes/network/)** - Default gateway latency and packet loss
  - **[WiFi Signal](probes/network/wifi/)** - WiFi signal strength and quality
- **WebApp Probes** - Web application monitoring
  - **[WebApp Ping](probes/ping_webapp/README.md)** - HTTP/HTTPS availability
  - **[WebApp Load](probes/load_webapp/README.md)** - Performance and response time
- **Event Probes** - Event collection and log aggregation
  - **[Syslog](probes/syslog/README.md)** - Syslog server (RFC 3164/5424)
  - **[Event](probes/event/README.md)** - Custom HTTP event endpoint
- **[Citrix CVAD](probes/citrix/)** - Citrix Virtual Apps and Desktops monitoring
- **[Redfish Hardware](probes/redfish/)** - Server hardware monitoring via Redfish
- **[OpenTelemetry](probes/otel/)** - OTEL integration
- **[All Probes](user-guide/PROBE-CONFIGURATION.md)** - Complete probe configuration guide

### Integration & APIs
- **[API Reference](user-guide/OFFLINE-MODE.md#api-endpoints)** - REST API documentation
- **[Universal Configuration](admin-guide/UNIVERSAL-CONFIGURATION.md)** - Probe configuration validation API
- **[HTTP Strategy](admin-guide/HTTP-STRATEGY.md)** - HTTP strategy configuration
- **[HTTP Bind Address](admin-guide/HTTP-BIND-ADDRESS.md)** - Network binding configuration

### Troubleshooting & Debugging
- **[Troubleshooting Guide](troubleshooting/TROUBLESHOOTING-OFFLINE.md)** - Common issues and solutions
- **[Logging System](admin-guide/LOGGING.md)** - Advanced logging and debugging
- **[Debug Mode](developer-guide/build-system.md#troubleshooting)** - Development debugging

### Development & Architecture
- **[Developer Guide](developer-guide/README.md)** - Architecture and development
- **[Architecture](developer-guide/architecture.md)** - System design and components
- **[Build Instructions](developer-guide/build-system.md)** - Building from source
- **[Code Style](developer-guide/architecture.md)** - Development standards
- **[Design Patterns](developer-guide/design-patterns.md)** - Code patterns and best practices
- **[Current Development](developer-guide/current-development.md)** - Active work and roadmap

## 📋 Quick Reference

### Essential Commands
```bash
# Install offline mode
./agent install --offline --enable-https

# Start/stop service
./agent start
./agent stop
./agent status

# Run interactively with debug
./agent run --offline --verbose

# Test health
curl http://localhost:8080/health
```

### Key Configuration Locations
- **Configuration File**: `./agent-config.yaml`
- **Certificates**: `./certs/agent-cert.pem`, `./certs/agent-key.pem`
- **Log Files**: `/var/log/senhub-agent/` (Linux), Event Viewer (Windows)

### Important URLs
- **Dashboard**: `http://localhost:8080/web/{agentkey}/dashboard`
- **HTTPS Dashboard**: `https://localhost:8443/web/{agentkey}/dashboard`
- **API Explorer**: `http://localhost:8080/web/{agentkey}/explorer`
- **Health Check**: `http://localhost:8080/health`

### Default Ports
- **HTTP**: 8080
- **HTTPS**: 8443
- **Syslog**: 514
- **Custom Events**: 5656

## 🆕 Recent Updates

### Version 0.8.0+ (Offline Mode)
- ✅ Complete offline mode implementation
- ✅ HTTPS/TLS support with auto-generated certificates
- ✅ Local web interface and API endpoints
- ✅ Comprehensive documentation suite
- ✅ Multiple monitoring tool integrations

### Documentation Updates
- 📚 Complete documentation rewrite for offline mode
- 🎯 User-focused quick start guides
- 🔒 Comprehensive security documentation
- 🛠️ Troubleshooting and debugging guides
- 📝 Configuration examples and templates

## 🤝 Contributing to Documentation

We welcome contributions to improve our documentation! Please:

1. **Check existing docs** before creating new ones
2. **Follow our style guide** (clear, concise, example-driven)
3. **Test all examples** before submitting
4. **Update this index** when adding new documentation

### Documentation Standards
- **Clear headings** and table of contents
- **Code examples** that actually work
- **Step-by-step instructions** for complex procedures
- **Cross-references** to related documentation
- **Troubleshooting sections** for common issues

## 📞 Getting Help

If you can't find what you're looking for:

1. **Check this index** for the most relevant documentation
2. **Search the documentation** using your browser (Ctrl/Cmd+F)
3. **Review examples** in the `examples/` directory
4. **Check troubleshooting** in `TROUBLESHOOTING-OFFLINE.md`
5. **Open an issue** on GitHub for missing or unclear documentation

---

**Last Updated**: January 2025  
**Documentation Version**: Complete  
**Total Documents**: 15+ files  
**Estimated Reading Time**: 2-3 hours for complete documentation