# Modular Logging System

SenHub Agent includes a sophisticated modular logging system that allows fine-grained control over log levels for different components. This system helps reduce log noise and enables targeted debugging.

## Overview

The modular logging system provides:
- **Per-module log level configuration** - Set different log levels for different components
- **Runtime configuration** - Adjust log levels without restarting the agent
- **HTTP API** - Manage log levels via REST endpoints
- **Default configurations** - Sensible defaults for all modules

## Supported Modules

The system supports the following predefined modules:

### Data Routing Strategies
- `strategy.http` - HTTP strategy logs (endpoints, requests, responses)
- `strategy.prtg` - PRTG strategy logs (data synchronization, API calls)
- `strategy.senhub` - SenHub strategy logs (server communication)

### Data Collection Probes
- `probe.redfish` - Redfish hardware monitoring probe
- `probe.host` - Host system probes (CPU, memory, disk, network)
- `probe.network` - Network-specific probes
- `probe.webapp` - Web application monitoring probes
- `probe.otel` - OpenTelemetry data collection probe
- `probe.gateway` - Gateway connectivity probes
- `probe.syslog` - Syslog monitoring probe

### Core System Components
- `cache` - Caching operations and management
- `transformer` - Metric name transformations
- `scheduler` - Probe scheduling and execution
- `configuration` - Configuration loading and management

## Log Levels

Available log levels (from most to least verbose):

| Level | Description | Use Case |
|-------|-------------|----------|
| `debug` | Detailed debugging information | Development, troubleshooting |
| `info` | General informational messages | Normal operation monitoring |
| `warn` | Warning conditions that don't stop operation | Potential issues |
| `error` | Error conditions that may affect functionality | Error monitoring |
| `fatal` | Fatal errors that cause program termination | Critical failures |
| `panic` | Panic conditions (highest severity) | System crashes |
| `disabled` | No logging for this module | Complete silence |

## HTTP API

### View Current Log Levels

**Endpoint:** `GET /api/{agentkey}/debug/logs`

**Response Example:**
```json
{
  "module_levels": [
    {"module": "strategy.http", "level": "info"},
    {"module": "probe.redfish", "level": "warn"},
    {"module": "probe.host", "level": "info"},
    {"module": "cache", "level": "warn"}
  ]
}
```

### Set Log Levels

**Endpoint:** `POST /api/{agentkey}/debug/logs`

**Request Body:**
```json
{
  "module_levels": [
    {"module": "strategy.http", "level": "debug"},
    {"module": "probe.redfish", "level": "warn"}
  ]
}
```

**Response:**
```json
{
  "status": "success",
  "message": "Log levels updated"
}
```

## Usage Examples

### Common Troubleshooting Scenarios

#### 1. Debug HTTP Strategy Issues
When debugging HTTP endpoint problems:
```bash
curl -X POST http://localhost:8080/api/YOUR_AGENT_KEY/debug/logs \
  -H "Content-Type: application/json" \
  -d '{
    "module_levels": [
      {"module": "strategy.http", "level": "debug"},
      {"module": "cache", "level": "debug"}
    ]
  }'
```

#### 2. Debug Specific Probe Issues
When a particular probe (e.g., Redfish) isn't working:
```bash
curl -X POST http://localhost:8080/api/YOUR_AGENT_KEY/debug/logs \
  -H "Content-Type: application/json" \
  -d '{
    "module_levels": [
      {"module": "probe.redfish", "level": "debug"}
    ]
  }'
```

#### 3. Reduce Log Noise
When logs are too verbose:
```bash
curl -X POST http://localhost:8080/api/YOUR_AGENT_KEY/debug/logs \
  -H "Content-Type: application/json" \
  -d '{
    "module_levels": [
      {"module": "strategy.http", "level": "warn"},
      {"module": "probe.host", "level": "error"},
      {"module": "cache", "level": "error"}
    ]
  }'
```

#### 4. Silent Specific Components
To completely disable logs for noisy components:
```bash
curl -X POST http://localhost:8080/api/YOUR_AGENT_KEY/debug/logs \
  -H "Content-Type: application/json" \
  -d '{
    "module_levels": [
      {"module": "transformer", "level": "disabled"},
      {"module": "scheduler", "level": "disabled"}
    ]
  }'
```

### View Current Configuration
```bash
# Check current log levels
curl http://localhost:8080/api/YOUR_AGENT_KEY/debug/logs | jq

# View with formatting
curl -s http://localhost:8080/api/YOUR_AGENT_KEY/debug/logs | \
  jq '.module_levels[] | "\(.module): \(.level)"'
```

## Integration Examples

### Monitoring Script
Create a script to automatically adjust log levels based on system load:

```bash
#!/bin/bash
AGENT_KEY="your-agent-key"
AGENT_URL="http://localhost:8080"

# High verbosity for debugging
debug_mode() {
  curl -X POST "$AGENT_URL/api/$AGENT_KEY/debug/logs" \
    -H "Content-Type: application/json" \
    -d '{
      "module_levels": [
        {"module": "strategy.http", "level": "debug"},
        {"module": "probe.redfish", "level": "debug"},
        {"module": "cache", "level": "debug"}
      ]
    }'
}

# Production mode - minimal logging
production_mode() {
  curl -X POST "$AGENT_URL/api/$AGENT_KEY/debug/logs" \
    -H "Content-Type: application/json" \
    -d '{
      "module_levels": [
        {"module": "strategy.http", "level": "warn"},
        {"module": "probe.redfish", "level": "error"},
        {"module": "cache", "level": "error"}
      ]
    }'
}
```

## Best Practices

### Development
- Use `debug` level for components you're actively developing
- Use `info` level for general operation visibility
- Use `warn` level for components you're not currently debugging

### Production
- Use `warn` or `error` levels for most components
- Use `info` level only for critical components
- Use `disabled` for very noisy components that aren't essential

### Troubleshooting
1. **Start with current levels**: Always check current configuration first
2. **Enable debug selectively**: Only enable debug for components you're investigating
3. **Restore after debugging**: Return to previous levels when done
4. **Monitor log volume**: Debug logging can generate significant log volume

## Default Configuration

By default, all modules are set to `info` level, providing a balanced view of system operation without excessive verbosity.

To view the current defaults:
```bash
curl http://localhost:8080/api/YOUR_AGENT_KEY/debug/logs
```

## Notes

- Log level changes are **immediate** and don't require agent restart
- Settings are **not persistent** - they reset when the agent restarts
- Invalid module names are **ignored silently**
- Invalid log levels default to `info`
- The API requires **agent key authentication** for security