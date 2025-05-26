# HTTP Strategy Documentation

## Overview

The HTTP Strategy provides a REST API interface for external monitoring tools to access SenHub Agent metrics. It's designed primarily for integration with tools like PRTG Network Monitor, but can be extended to support other monitoring platforms.

## Architecture

### Components

- **HTTP Server**: Built with `gorilla/mux` for robust routing
- **Metric Cache**: Thread-safe in-memory storage with configurable TTL
- **Transformer System**: Modular metric name transformation with YAML configurations
- **Authentication**: Agent key validation for secure access

### Data Flow

1. Probes collect metrics and send to DataStore
2. HTTP Strategy receives metrics via `AddDataPoints()`
3. Metrics are cached with timestamps and metadata
4. External tools make HTTP requests to retrieve metrics
5. Transformers convert technical names to user-friendly names
6. Responses are formatted according to the requesting tool's requirements

## Configuration

### Basic Configuration

```json
{
  "name": "http",
  "params": {
    "port": 8080,
    "naming": {
      "redfish": "friendly",
      "host": "friendly", 
      "otel": "technical"
    }
  }
}
```

### Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `port` | integer | 8080 | HTTP server listening port |
| `naming` | object | see below | Metric name transformation styles per probe |

### Naming Styles

- **`friendly`**: User-friendly names suitable for non-technical users
  - Example: `thermal.cpu.0.temperature` → `"CPU Temperature - Processor 0"`
- **`technical`**: Preserves technical metric names
  - Example: `thermal.cpu.0.temperature` → `"thermal.cpu.0.temperature"`
- **`prtg_standard`**: PRTG-optimized naming conventions (future)

## Endpoints

### PRTG Metrics Endpoint

**`POST /api/{agentkey}/prtg/metrics`**

Retrieves metrics in PRTG-compatible JSON format.

#### Authentication
- Agent key must be provided in the URL path
- Invalid keys return `HTTP 401 Unauthorized`

#### Request Format
```json
{
  "probe": "redfish",
  "target": "server1",
  "config": {
    "host": "192.168.1.100",
    "username": "admin",
    "password": "secret"
  }
}
```

#### Response Format
```json
{
  "prtg": {
    "result": [
      {
        "channel": "CPU Temperature - Processor 0",
        "value": 65.2,
        "unit": "°C",
        "limitmode": 1,
        "limitmaxwarning": 70,
        "limitmaxerror": 85
      },
      {
        "channel": "Memory Usage",
        "value": 75.5,
        "unit": "%"
      }
    ]
  }
}
```

#### Response Fields
- `channel`: Human-readable metric name
- `value`: Numeric metric value
- `unit`: Unit of measurement (optional)
- `limitmode`: PRTG limit mode (optional)
- `limitmaxwarning`: Warning threshold (optional)
- `limitmaxerror`: Error threshold (optional)

### Health Check Endpoint

**`GET /health`**

Returns server health status.

#### Response
```json
{
  "status": "ok"
}
```

## Metric Transformations

### Configuration Files

Transformations are defined in YAML files located in `internal/agent/services/data_store/transformers/`:

- `redfish_friendly.yaml`: Redfish hardware metrics
- `host_friendly.yaml`: System/host metrics
- `otel_technical.yaml`: OpenTelemetry metrics

### Transformation Patterns

#### Template Variables
- `{index}`: Numeric index (e.g., processor number, drive index)
- `{component}`: Component name from metric tags
- `{instance}`: Instance identifier from metric tags

#### Example Patterns

```yaml
patterns:
  # Input pattern: output template
  "thermal.cpu.{index}.temperature": "CPU Temperature - Processor {index}"
  "memory.used_percent": "Memory Usage"
  "power.psu.{index}.output_watts": "Power Supply - PSU{index} Output"

units:
  temperature: "°C"
  power: "W" 
  percentage: "%"
```

### Adding New Transformations

1. Edit the appropriate YAML file
2. Add pattern mappings using the template syntax
3. Restart the agent to load new configurations

### Fallback Behavior

If no transformation pattern matches:
1. Look for partial pattern matches with wildcards
2. Apply generic "make readable" transformation
3. Return original metric name as last resort

## Cache Management

### TTL and Cleanup

- **Default TTL**: 5 minutes
- **Cleanup Interval**: 1 minute (automatic background process)
- **Thread Safety**: All cache operations are protected by read-write mutexes

### Memory Management

The cache automatically removes expired metrics to prevent memory leaks. Cache size is bounded by:
- TTL-based expiration
- Number of active probes and their metrics
- Frequency of metric collection

## Integration Examples

### PRTG Network Monitor

1. Create an HTTP Advanced sensor in PRTG
2. Configure POST request to agent endpoint
3. Set up JSON parsing for PRTG channels
4. Schedule regular monitoring intervals

```bash
# Example PRTG sensor configuration
URL: http://agent-host:8080/api/your-agent-key/prtg/metrics
Method: POST
Content-Type: application/json
Post Data: {
  "probe": "redfish",
  "target": "server1", 
  "config": {
    "host": "192.168.1.100",
    "username": "admin",
    "password": "secret"
  }
}
```

### Custom Monitoring Tools

The JSON response format can be easily parsed by custom scripts:

```python
import requests
import json

response = requests.post(
    'http://agent-host:8080/api/your-agent-key/prtg/metrics',
    json={
        'probe': 'host',
        'target': 'localhost'
    }
)

metrics = response.json()
for channel in metrics['prtg']['result']:
    print(f"{channel['channel']}: {channel['value']} {channel.get('unit', '')}")
```

## Security Considerations

### Authentication
- Agent key provides basic authentication
- Keys should be kept confidential and rotated regularly
- Consider using HTTPS in production environments

### Network Security
- Bind to specific interfaces if needed
- Use firewall rules to restrict access
- Monitor for unauthorized access attempts

## Troubleshooting

### Common Issues

1. **HTTP 401 Unauthorized**
   - Verify agent key in URL path
   - Check key configuration in agent

2. **Empty Response**
   - Verify probe is collecting metrics
   - Check metric cache TTL
   - Ensure probe name matches request

3. **Connection Refused**
   - Verify HTTP strategy is enabled
   - Check port configuration
   - Ensure no port conflicts

### Debugging

Enable verbose logging to see detailed HTTP request/response information:

```bash
./senhub-agent run --verbose --authentication-key <key>
```

### Performance Monitoring

Monitor these metrics for optimal performance:
- Cache hit/miss ratios
- Request response times
- Memory usage of cached metrics
- Transformation processing time

## Future Enhancements

### Planned Features

1. **Prometheus Format Support**: Native Prometheus metrics endpoint
2. **Dynamic Configuration**: Real-time probe configuration updates
3. **Metric Filtering**: Advanced filtering and aggregation capabilities
4. **Rate Limiting**: Request throttling for resource protection
5. **Metrics Export**: Bulk export capabilities for analysis tools

### Extension Points

The HTTP Strategy is designed for extensibility:
- Additional response formats can be added
- New transformation styles can be implemented
- Custom authentication methods can be integrated
- Additional endpoints can be registered