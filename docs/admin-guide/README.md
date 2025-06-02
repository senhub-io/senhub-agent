# Administrator Guide

This section contains documentation for system administrators and advanced users who need to configure and manage SenHub Agent in production environments.

## 📋 Contents

### HTTP Configuration
- **[HTTP Strategy](./HTTP-STRATEGY.md)** - Configure HTTP monitoring strategy and endpoints
- **[HTTPS Configuration](./HTTPS-CONFIGURATION.md)** - Set up SSL/TLS certificates and HTTPS
- **[HTTP Bind Address](./HTTP-BIND-ADDRESS.md)** - Configure network binding and interfaces

### System Management
- **[Logging](./LOGGING.md)** - Configure logging levels, outputs, and log management

## 🎯 Who This Is For

- **System Administrators** managing production SenHub Agent deployments
- **DevOps Engineers** configuring advanced networking and security features
- **Security Teams** implementing HTTPS and certificate management
- **Platform Engineers** integrating SenHub Agent with monitoring infrastructure

## 🔧 Prerequisites

Before using this guide, you should:
- Have SenHub Agent installed and running (see [User Guide](../user-guide/))
- Understand basic networking concepts
- Have administrative access to the target systems
- Be familiar with YAML configuration files

## 🚀 Common Admin Tasks

| Task | Documentation |
|------|---------------|
| Enable HTTPS monitoring | [HTTPS Configuration](./HTTPS-CONFIGURATION.md) |
| Configure network interfaces | [HTTP Bind Address](./HTTP-BIND-ADDRESS.md) |
| Set up monitoring endpoints | [HTTP Strategy](./HTTP-STRATEGY.md) |
| Manage log levels | [Logging](./LOGGING.md) |

## ⚠️ Security Considerations

When configuring SenHub Agent in production:
- Always use HTTPS in production environments
- Properly configure certificate validation
- Use appropriate network binding (avoid 0.0.0.0 in production unless necessary)
- Set appropriate log levels to avoid sensitive data exposure

For troubleshooting, see the [Troubleshooting Guide](../troubleshooting/).