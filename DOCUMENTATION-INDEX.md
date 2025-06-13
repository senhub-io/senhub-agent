# SenHub Agent - Documentation Index

> **📌 Documentation Reorganized!** 
> 
> For better navigation, documentation has been reorganized into the **[docs/](./docs/)** directory with dedicated sections for different user types. This page remains as a comprehensive index of all documentation.
> 
> **🚀 Start here: [docs/README.md](./docs/README.md)** for organized documentation by user role.

Welcome to the comprehensive documentation for SenHub Agent. This index will help you find the right documentation for your needs.

## 🚀 Getting Started

### For New Users
1. **[Quick Start Guide (5 min)](QUICK-START-OFFLINE.md)** - Get your agent running in 5 minutes
2. **[Offline Mode Guide](OFFLINE-MODE.md)** - Complete standalone deployment documentation
3. **[Installation Examples](examples/)** - Ready-to-use configuration examples

### For Production Deployment  
1. **[HTTPS Configuration](HTTPS-CONFIGURATION.md)** - TLS/SSL setup and security
2. **[Troubleshooting Guide](TROUBLESHOOTING-OFFLINE.md)** - Common issues and solutions
3. **[Security Best Practices](#security-best-practices)** - Production security guidelines

## 📚 User Documentation

### Core Guides
| Document | Description | Audience | Time to Read |
|----------|-------------|----------|--------------|
| **[QUICK-START-OFFLINE.md](QUICK-START-OFFLINE.md)** | 5-minute setup guide | All users | 5 min |
| **[OFFLINE-MODE.md](OFFLINE-MODE.md)** | Complete offline mode documentation | Users, Admins | 30 min |
| **[HTTPS-CONFIGURATION.md](HTTPS-CONFIGURATION.md)** | TLS/SSL configuration guide | Admins, DevOps | 20 min |
| **[TROUBLESHOOTING-OFFLINE.md](TROUBLESHOOTING-OFFLINE.md)** | Troubleshooting and debugging | All users | 15 min |

### Configuration References
| Document | Description | Use Case |
|----------|-------------|----------|
| **[examples/offline-config-example.yaml](examples/offline-config-example.yaml)** | Complete configuration reference | Configuration template |
| **[examples/https-config-example.yaml](examples/https-config-example.yaml)** | HTTPS configuration examples | TLS setup scenarios |
| **[PROBE-CONFIGURATION.md](PROBE-CONFIGURATION.md)** | Probe configuration guide | Custom monitoring |
| **[HTTP-STRATEGY.md](HTTP-STRATEGY.md)** | HTTP strategy configuration | API integration |

## 👨‍💻 Developer Documentation

### Core Development
| Document | Description | Audience |
|----------|-------------|----------|
| **[CLAUDE.md](CLAUDE.md)** | Development guide and architecture | Developers |
| **[LOGGING.md](LOGGING.md)** | Logging system and debugging | Developers, DevOps |
| **[HTTP-BIND-ADDRESS.md](HTTP-BIND-ADDRESS.md)** | Network binding configuration | Network admins |
| **[OTEL-METRICS.md](OTEL-METRICS.md)** | OpenTelemetry integration | DevOps, SRE |

### Specialized Features
| Document | Description | Use Case |
|----------|-------------|----------|
| **[REDFISH-METRICS.md](REDFISH-METRICS.md)** | Hardware monitoring via Redfish | Server monitoring |
| **[REDFISH-TAGS.md](REDFISH-TAGS.md)** | Redfish metric tagging | Hardware metrics |
| **[OTEL-PROBE.md](OTEL-PROBE.md)** | OpenTelemetry probe configuration | OTEL integration |

## 🎯 Quick Navigation by Use Case

### "I want to get started quickly"
1. **[QUICK-START-OFFLINE.md](QUICK-START-OFFLINE.md)** (5 minutes)
2. **[examples/offline-config-example.yaml](examples/offline-config-example.yaml)** (configuration reference)

### "I need to deploy in production"
1. **[OFFLINE-MODE.md](OFFLINE-MODE.md)** (comprehensive guide)
2. **[HTTPS-CONFIGURATION.md](HTTPS-CONFIGURATION.md)** (security setup)
3. **[examples/https-config-example.yaml](examples/https-config-example.yaml)** (HTTPS examples)

### "I have an issue to resolve"
1. **[TROUBLESHOOTING-OFFLINE.md](TROUBLESHOOTING-OFFLINE.md)** (troubleshooting guide)
2. **[LOGGING.md](LOGGING.md)** (debugging information)
3. **[CLAUDE.md](CLAUDE.md)** (development context)

### "I want to integrate with monitoring tools"
1. **[OFFLINE-MODE.md#api-endpoints](OFFLINE-MODE.md#api-endpoints)** (API reference)
2. **[HTTP-STRATEGY.md](HTTP-STRATEGY.md)** (HTTP strategy details)
3. **[examples/offline-config-example.yaml](examples/offline-config-example.yaml)** (configuration examples)

### "I need hardware monitoring"
1. **[REDFISH-METRICS.md](REDFISH-METRICS.md)** (Redfish documentation)
2. **[examples/offline-config-example.yaml](examples/offline-config-example.yaml)** (Redfish configuration)
3. **[REDFISH-TAGS.md](REDFISH-TAGS.md)** (metric tagging)

### "I want to customize monitoring"
1. **[PROBE-CONFIGURATION.md](PROBE-CONFIGURATION.md)** (probe setup)
2. **[examples/offline-config-example.yaml](examples/offline-config-example.yaml)** (probe examples)
3. **[CLAUDE.md](CLAUDE.md)** (development guide for custom probes)

## 🔍 Documentation by Topic

### Installation & Setup
- **[Quick Start](QUICK-START-OFFLINE.md)** - 5-minute setup
- **[Complete Guide](OFFLINE-MODE.md)** - Full installation documentation
- **[Configuration Examples](examples/)** - Ready-to-use configurations

### Security & HTTPS
- **[HTTPS Configuration](HTTPS-CONFIGURATION.md)** - Complete TLS setup guide
- **[Security Best Practices](HTTPS-CONFIGURATION.md#security-best-practices)** - Production security
- **[Certificate Management](HTTPS-CONFIGURATION.md#certificate-management)** - Certificate handling

### Monitoring & Probes
- **[Probe Configuration](PROBE-CONFIGURATION.md)** - Configure monitoring probes
- **[Hardware Monitoring](REDFISH-METRICS.md)** - Server hardware via Redfish
- **[OpenTelemetry](OTEL-METRICS.md)** - OTEL integration
- **[Event Collection](OFFLINE-MODE.md#available-monitoring-probes)** - Syslog and events

### Integration & APIs
- **[API Reference](OFFLINE-MODE.md#api-endpoints)** - REST API documentation
- **[Universal Configuration](docs/admin-guide/UNIVERSAL-CONFIGURATION.md)** - Probe configuration validation API
- **[HTTP Strategy](HTTP-STRATEGY.md)** - HTTP strategy configuration
- **[Integration Examples](OFFLINE-MODE.md#integration-examples)** - PRTG, Nagios, Grafana

### Troubleshooting & Debugging
- **[Troubleshooting Guide](TROUBLESHOOTING-OFFLINE.md)** - Common issues and solutions
- **[Logging System](LOGGING.md)** - Advanced logging and debugging
- **[Debug Mode](CLAUDE.md#debugging-guide)** - Development debugging

### Development & Architecture
- **[Development Guide](CLAUDE.md)** - Architecture and development
- **[Build Instructions](CLAUDE.md#build-commands)** - Building from source
- **[Code Style](CLAUDE.md#code-style-guidelines)** - Development standards

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