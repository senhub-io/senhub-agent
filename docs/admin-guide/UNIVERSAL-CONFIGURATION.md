# Universal Configuration API

The SenHub Agent provides a powerful Universal Configuration API that allows validation and testing of probe configurations before deployment. This system supports all probe types and provides three levels of validation for maximum flexibility.

## Overview

The Universal Configuration API enables:
- **Schema validation** for configuration structure and data types
- **Connectivity testing** for network-based probes
- **Full metrics testing** with preview data collection
- **Support for all probe types** (Redfish, WebApp, System probes, etc.)
- **Integration with any monitoring system** (PRTG, Nagios, Zabbix, etc.)

## API Endpoints

All Universal Configuration endpoints require authentication via agent key in the URL path:

```
POST /api/{agentkey}/config/validate    # Configurable validation level
POST /api/{agentkey}/config/preview     # Fast schema-only validation
POST /api/{agentkey}/config/test        # Full validation with metrics preview
```

## Request Format

All endpoints accept the same JSON request structure:

```json
{
  "probe": "redfish",
  "target": "https://server.example.com",
  "config": {
    "endpoint": "https://server.example.com",
    "username": "admin",
    "password": "secret123",
    "verify_ssl": false,
    "collections": ["system", "thermal", "power"]
  },
  "validation": "connectivity",
  "timeout": 30
}
```

### Request Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `probe` | string | ✅ | Target probe name (redfish, cpu, memory, etc.) |
| `target` | string | ❌ | Target system URL or identifier |
| `config` | object | ✅ | Probe-specific configuration parameters |
| `validation` | string | ❌ | Validation level: `schema`, `connectivity`, `full` |
| `timeout` | integer | ❌ | Timeout for connectivity tests (seconds, default: 30) |

## Validation Levels

### 1. Schema Validation (`"validation": "schema"`)
- **Purpose**: Validates configuration structure and data types
- **Speed**: Very fast (~1-5ms)
- **Use case**: Quick validation during configuration creation

**What it validates**:
- Required fields are present
- Field data types are correct
- Value ranges and formats (URLs, ports, etc.)

### 2. Connectivity Validation (`"validation": "connectivity"`)
- **Purpose**: Schema validation + network connectivity testing
- **Speed**: Fast (~100-2000ms depending on network)
- **Use case**: Pre-deployment validation

**What it validates**:
- All schema validation checks
- Network connectivity to target systems
- Service availability (HTTP endpoints, ports)
- Authentication endpoint accessibility

### 3. Full Validation (`"validation": "full"`)
- **Purpose**: Complete validation including actual metrics collection
- **Speed**: Slower (~1-10 seconds depending on probe)
- **Use case**: Comprehensive testing and metrics preview

**What it validates**:
- All connectivity validation checks
- Actual metrics collection capability
- Returns preview metrics for verification

## Response Format

All endpoints return a structured JSON response:

```json
{
  "valid": true,
  "probe": "redfish",
  "target": "https://server.example.com",
  "validation_level": "connectivity",
  "tests": {
    "schema": {
      "passed": true,
      "duration_ms": 2,
      "details": "Redfish configuration schema is valid"
    },
    "connectivity": {
      "passed": true,
      "duration_ms": 1250,
      "details": "Redfish service accessible (HTTP 401)"
    }
  },
  "warnings": ["TLS certificate verification disabled"],
  "errors": [],
  "duration_ms": 1252
}
```

### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `valid` | boolean | Overall validation result |
| `probe` | string | Probe type that was validated |
| `target` | string | Target system that was tested |
| `validation_level` | string | Level of validation performed |
| `tests` | object | Individual test results by type |
| `warnings` | array | Non-fatal warnings |
| `errors` | array | Validation errors (if any) |
| `preview_metrics` | array | Sample metrics (full validation only) |
| `duration_ms` | integer | Total validation time |

## Probe-Specific Configuration

### Redfish Probe

**Required fields**:
- `endpoint` - Redfish API URL
- `username` - Authentication username  
- `password` - Authentication password

**Optional fields**:
- `verify_ssl` - TLS certificate verification (default: true)
- `collections` - Metrics to collect (default: all available)
- `interval` - Collection interval in seconds
- `cache_duration` - Cache duration in seconds

**Example**:
```json
{
  "probe": "redfish",
  "target": "https://ilo.server.example.com",
  "config": {
    "endpoint": "https://ilo.server.example.com",
    "username": "monitoring",
    "password": "secure123",
    "verify_ssl": true,
    "collections": ["system", "thermal", "power", "storage"]
  },
  "validation": "full"
}
```

### WebApp Probes (ping_webapp, load_webapp)

**Required fields**:
- `url` - Target web application URL

**Optional fields**:
- `timeout` - HTTP request timeout
- `interval` - Check interval in seconds

**Example**:
```json
{
  "probe": "ping_webapp",
  "target": "https://app.example.com",
  "config": {
    "url": "https://app.example.com/health",
    "timeout": "30s",
    "interval": 60
  },
  "validation": "connectivity"
}
```

### System Probes (cpu, memory, network, logicaldisk)

**Optional fields**:
- `interval` - Collection interval in seconds

**Example**:
```json
{
  "probe": "cpu",
  "config": {
    "interval": 30
  },
  "validation": "schema"
}
```

### Gateway Probe (ping_gateway)

**Optional fields**:
- `target` - Specific gateway IP (auto-discovered if not provided)
- `interval` - Ping interval in seconds

**Example**:
```json
{
  "probe": "ping_gateway",
  "config": {
    "target": "192.168.1.1",
    "interval": 60
  },
  "validation": "connectivity"
}
```

### Syslog Probe

**Optional fields**:
- `port` - UDP/TCP port number (default: 514)
- `protocol` - Transport protocol: "udp" or "tcp" (default: "udp")

**Example**:
```json
{
  "probe": "syslog",
  "config": {
    "port": 1514,
    "protocol": "udp"
  },
  "validation": "schema"
}
```

## Usage Examples

### 1. Quick Schema Validation

Validate configuration structure without network testing:

```bash
curl -X POST http://localhost:8080/api/YOUR_AGENT_KEY/config/preview \
  -H "Content-Type: application/json" \
  -d '{
    "probe": "redfish",
    "config": {
      "endpoint": "https://server.example.com",
      "username": "admin",
      "password": "secret"
    }
  }'
```

### 2. Pre-Deployment Connectivity Test

Test configuration and network connectivity:

```bash
curl -X POST http://localhost:8080/api/YOUR_AGENT_KEY/config/validate \
  -H "Content-Type: application/json" \
  -d '{
    "probe": "redfish",
    "target": "https://server.example.com",
    "config": {
      "endpoint": "https://server.example.com",
      "username": "admin",
      "password": "secret123",
      "verify_ssl": false
    },
    "validation": "connectivity",
    "timeout": 30
  }'
```

### 3. Full Testing with Metrics Preview

Complete validation including actual metrics collection:

```bash
curl -X POST http://localhost:8080/api/YOUR_AGENT_KEY/config/test \
  -H "Content-Type: application/json" \
  -d '{
    "probe": "redfish",
    "target": "https://server.example.com",
    "config": {
      "endpoint": "https://server.example.com", 
      "username": "admin",
      "password": "secret123",
      "verify_ssl": false,
      "collections": ["system", "thermal", "power"]
    }
  }'
```

**Response with preview metrics**:
```json
{
  "valid": true,
  "probe": "redfish",
  "validation_level": "full",
  "tests": {
    "schema": {"passed": true, "duration_ms": 2},
    "connectivity": {"passed": true, "duration_ms": 1200},
    "metrics": {"passed": true, "duration_ms": 3500}
  },
  "preview_metrics": [
    {
      "name": "system.health",
      "value": 1,
      "tags": {"system_id": "1"},
      "timestamp": 1640995200
    },
    {
      "name": "thermal.cpu.0.temperature",
      "value": 45.2,
      "tags": {"cpu_id": "0", "system_id": "1"},
      "timestamp": 1640995200
    }
  ],
  "duration_ms": 4702
}
```

## Integration with Monitoring Systems

The Universal Configuration API can be used with any monitoring system to validate configurations before applying them:

### PRTG Integration

```bash
# 1. Validate configuration
curl -X POST /api/key/config/validate -d '{"probe":"redfish","config":{...}}'

# 2. Use validated configuration with PRTG
curl -X POST /api/key/prtg/metrics -d '{"probe":"redfish","config":{...}}'
```

### Nagios Integration

```bash
# 1. Test configuration with connectivity check
curl -X POST /api/key/config/test -d '{"probe":"ping_webapp","config":{...}}'

# 2. Deploy to Nagios monitoring
curl -X POST /api/key/nagios/metrics -d '{"probe":"ping_webapp","config":{...}}'
```

### Custom Monitoring Integration

The Universal Configuration API provides a standardized way to validate any probe configuration regardless of the target monitoring system.

## Error Handling

### Validation Errors

When validation fails, the response includes detailed error information:

```json
{
  "valid": false,
  "probe": "redfish",
  "validation_level": "connectivity",
  "tests": {
    "schema": {"passed": true, "duration_ms": 2},
    "connectivity": {
      "passed": false,
      "error": "connectivity test failed: dial tcp 192.168.1.100:443: connection refused",
      "duration_ms": 5000
    }
  },
  "errors": ["connectivity test failed: dial tcp 192.168.1.100:443: connection refused"],
  "duration_ms": 5002
}
```

### Common Error Types

| Error Type | Description | Resolution |
|------------|-------------|------------|
| Schema errors | Missing required fields, invalid data types | Fix configuration structure |
| Connectivity errors | Network timeouts, DNS failures, connection refused | Check network connectivity and target availability |
| Authentication errors | Invalid credentials, permission denied | Verify username/password and access permissions |
| Metrics errors | Unable to collect data, API incompatibility | Check target system compatibility and API access |

## Best Practices

### 1. Validation Workflow

**Recommended validation sequence**:
1. **Schema validation** during configuration creation/editing
2. **Connectivity validation** before deployment
3. **Full validation** for comprehensive testing (optional)

### 2. Performance Considerations

- Use **schema validation** for real-time configuration validation in UIs
- Use **connectivity validation** for pre-deployment checks
- Use **full validation** sparingly due to higher resource usage

### 3. Security

- Always validate configurations before deployment to production systems
- Use appropriate timeout values to prevent resource exhaustion
- Consider credential security when testing authentication

### 4. Monitoring Integration

- Validate configurations using the Universal API before applying to monitoring systems
- Use preview metrics to verify expected data collection
- Implement automated validation in CI/CD pipelines for configuration management

## Troubleshooting

### Common Issues

**1. Schema validation fails with "missing required field"**
- Check that all required fields are present in the configuration
- Verify field names match expected values exactly

**2. Connectivity validation fails with timeout**
- Increase timeout value in request
- Verify network connectivity to target system
- Check firewall rules and network accessibility

**3. Full validation passes but no preview metrics**
- Check if target system has data available for collection
- Verify authentication and permissions for data access
- Review probe-specific requirements and limitations

### Debug Mode

Enable debug logging for detailed validation information:

```bash
# Start agent with debug logging for HTTP strategy
./agent run --verbose --debug-modules strategy.http
```

This will provide detailed logs for all Universal Configuration API operations, including validation steps and error details.