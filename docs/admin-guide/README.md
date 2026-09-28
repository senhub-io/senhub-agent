# Administrator Guide

This section contains documentation for system administrators and advanced users who need to configure and manage SenHub Agent in production environments.

## 📋 Contents

### HTTP Configuration
- **[HTTP Strategy](./HTTP-STRATEGY.md)** - Configure HTTP monitoring strategy and endpoints
- **[HTTPS Configuration](./HTTPS-CONFIGURATION.md)** - Set up SSL/TLS certificates and HTTPS
- **[HTTP Bind Address](./HTTP-BIND-ADDRESS.md)** - Configure network binding and interfaces
- **[Universal Configuration](./UNIVERSAL-CONFIGURATION.md)** - Validate probe configurations before deployment

### Observability
- **[OTLP Observability](./OTLP-OBSERVABILITY.md)** - Field reference for the OTLP self-metric surfaces (HTTP endpoint `/info/otlp`, CLI `status --otlp`, dashboard card) and starter alert recipes

### System Management
- **[Logging](./LOGGING.md)** - Configure logging levels, outputs, and log management
- **[Least-Privilege](./LEAST-PRIVILEGE.md)** - Run the daemon as a non-root user with per-probe capabilities
- **[Global and custom tags](./TAGS.md)** - Attach site / tenant / region labels to every datapoint
- **[Configuration Version Changelog](./CONFIG-VERSION-CHANGELOG.md)** - Config format versions and migration
- **[Update Signing](./UPDATE-SIGNING.md)** - How a self-update artifact is verified before it is applied

### OTLP Pipeline
- **[Backpressure & Resilience](./BACKPRESSURE.md)** - Cardinality caps, memory limiter, persistent checkpoint

### Build Notes
- **[Backup and restore](./BACKUP-RESTORE.md)** - What to copy to rebuild an agent, the secret store and console traps
- **[IBM i Native Runner](./IBMI-RUNTIME-BUILD.md)** - Getting and installing the `jt400runner` binary

## 🎯 Who This Is For

- **System Administrators** managing production SenHub Agent deployments
- **DevOps Engineers** configuring advanced networking and security features
- **Security Teams** implementing HTTPS and certificate management
- **Platform Engineers** integrating SenHub Agent with monitoring infrastructure

## 🔧 Prerequisites

Before using this guide, you should:
- Have SenHub Agent installed and running (see [User Guide](../user-guide/docs/index.md))
- Understand basic networking concepts
- Have administrative access to the target systems
- Be familiar with YAML configuration files

## 🚀 Common Admin Tasks

| Task | Documentation |
|------|---------------|
| Enable HTTPS monitoring | [HTTPS Configuration](./HTTPS-CONFIGURATION.md) |
| Configure network interfaces | [HTTP Bind Address](./HTTP-BIND-ADDRESS.md) |
| Set up monitoring endpoints | [HTTP Strategy](./HTTP-STRATEGY.md) |
| Validate probe configurations | [Universal Configuration](./UNIVERSAL-CONFIGURATION.md) |
| Watch the OTLP push pipeline | [OTLP Observability](./OTLP-OBSERVABILITY.md) |
| Manage log levels | [Logging](./LOGGING.md) |

## ⚠️ Security Considerations

When configuring SenHub Agent in production:
- Always use HTTPS in production environments
- Properly configure certificate validation
- Use appropriate network binding (avoid 0.0.0.0 in production unless necessary)
- Set appropriate log levels to avoid sensitive data exposure

For troubleshooting, see the [Troubleshooting Guide](../user-guide/docs/troubleshooting.md).