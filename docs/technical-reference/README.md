# Technical Reference

This section contains detailed technical documentation for developers, integrators, and advanced users who need to understand the inner workings of SenHub Agent.

## 📋 Contents

### Redfish Integration
- **[Redfish Overview](./README-REDFISH.md)** - Overview of Redfish probe implementation
- **[Redfish Metrics](./REDFISH-METRICS.md)** - Complete list of available Redfish metrics
- **[Redfish Tags](./REDFISH-TAGS.md)** - Tagging system and metadata for Redfish metrics
- **[Redfish Tag Enhancement](./REDFISH-TAG-ENHANCEMENT.md)** - Advanced tag enhancement system for better organization

### OpenTelemetry Integration
- **[OpenTelemetry Metrics](./OTEL-METRICS.md)** - OTEL metrics collection and export
- **[OpenTelemetry Probe](./OTEL-PROBE.md)** - OTEL probe configuration and usage

## 🎯 Who This Is For

- **Developers** extending or integrating with SenHub Agent
- **Platform Engineers** building monitoring solutions
- **Integration Specialists** connecting SenHub Agent to other systems
- **Advanced Users** who need to understand metric structures and data formats

## 🔧 Prerequisites

- Familiarity with monitoring concepts and metrics
- Understanding of REST APIs and data formats (JSON, YAML)
- Knowledge of the systems you're monitoring (servers, network devices, etc.)
- For Redfish: Understanding of BMC and hardware management concepts
- For OTEL: Familiarity with OpenTelemetry standards

## 📊 Key Concepts

### Metrics Structure
SenHub Agent organizes metrics using:
- **Probes** - Data collection modules (Redfish, Host, Network, etc.)
- **Tags** - Key-value metadata for filtering and organization
- **Time Series** - Metric data points with timestamps
- **Transformers** - Data format conversion for different monitoring systems

### Integration Patterns
- **Pull-based** - External systems query SenHub Agent APIs
- **Push-based** - SenHub Agent sends data to external systems
- **Hybrid** - Combination of pull and push based on requirements

## 🚀 Quick Reference

| Topic | Key Information |
|-------|-----------------|
| Redfish Metrics | [Available metrics list](./REDFISH-METRICS.md) |
| Metric Tags | [Tagging conventions](./REDFISH-TAGS.md) |
| OTEL Integration | [Setup guide](./OTEL-PROBE.md) |
| Data Formats | Multiple output formats (JSON, Prometheus, PRTG, etc.) |

## 🔗 Related Documentation

- [User Guide](../user-guide/) - Basic configuration and setup
- [Admin Guide](../admin-guide/) - Advanced configuration
- [API Documentation](../../README.markdown#api-endpoints) - REST API reference

For implementation examples and code samples, see the project's examples directory.