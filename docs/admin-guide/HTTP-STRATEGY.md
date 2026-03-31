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

## PRTG Value Lookups

### Overview

PRTG Value Lookups (`.ovl` files) provide **human-readable status values** for numeric metrics in PRTG Network Monitor. Instead of showing raw numbers like `0`, `1`, `2`, PRTG displays meaningful text like `"DOWN"`, `"UP"`, `"PRIMARY"`.

**Benefits:**
- **Improved readability**: See "UP" instead of "1"
- **Color coding**: PRTG applies colors based on severity (red for error, green for ok)
- **Automated alerting**: Trigger alerts based on status values
- **Dashboard clarity**: Status at a glance without memorizing numeric codes

### Lookup Definition Format

Lookups are defined in `/internal/agent/services/data_store/strategies/http/lookups/lookups.yaml`:

```yaml
netscaler.lbvserver.state:
  description: "NetScaler Load Balancer Virtual Server State"
  type: status
  source: "Citrix ADC NITRO API - lbvserver state field"
  mappings:
    0: {text: "DOWN", severity: "error"}
    1: {text: "UP", severity: "ok"}
    2: {text: "OUT OF SERVICE", severity: "warning"}
    3: {text: "BUSY", severity: "warning"}
    7: {text: "UNKNOWN", severity: "warning"}
  desired_value: 1  # "UP" is the desired state
```

**Field Descriptions:**

| Field | Required | Description |
|-------|----------|-------------|
| `description` | Yes | Human-readable description of what this lookup represents |
| `type` | No | Lookup type (`status`, `health`, `role`, etc.) - for documentation only |
| `source` | No | Where the values come from (API field, calculation, etc.) |
| `mappings` | Yes | Numeric value → text + severity mappings |
| `desired_value` | No | The "good" value that should not trigger alerts (usually 1) |

**Severity Levels:**
- `ok` - Normal operation (green in PRTG)
- `warning` - Degraded but operational (yellow in PRTG)
- `error` - Failed or critical state (red in PRTG)

### Available Lookups

#### NetScaler ADC Probes

- **`netscaler.lbvserver.state`** - Load Balancer Virtual Server state (UP/DOWN/OUT OF SERVICE/BUSY/UNKNOWN)
- **`netscaler.service.state`** - Backend Service state (UP/DOWN/OUT OF SERVICE/BUSY/UNKNOWN)
- **`netscaler.servicegroup.state`** - Service Group state (UP/DOWN/OUT OF SERVICE/BUSY/UNKNOWN)
- **`netscaler.interface.state`** - Network Interface state (ENABLED/DISABLED)
- **`netscaler.ssl.certificate.status`** - SSL Certificate validity (VALID/INVALID)
- **`netscaler.ha.state`** - HA Node Role (PRIMARY/SECONDARY/UNKNOWN)
- **`netscaler.ha.node.state`** - HA Node Operational State (UP/DOWN)
- **`netscaler.ha.sync_status`** - HA Configuration Sync Status (SUCCESS/FAILED)

#### Redfish Hardware Probes

- **`redfish.health`** - Component health status (OK/WARNING/CRITICAL)
- **`redfish.state`** - Component state (ENABLED/DISABLED/STANDBY/ABSENT)
- **`redfish.power_state`** - System power state (ON/OFF/POWERING_ON/POWERING_OFF)

### Downloading Lookups

The agent provides **automatic lookup file generation** accessible via the Web UI:

1. **Open Dashboard**: Navigate to `http://localhost:8080/web/{agentkey}/dashboard`
2. **Go to API Explorer**: Click on "API Explorer" in navigation
3. **Download Lookups**: Click the **"Download PRTG Lookups"** button
4. **Extract Files**: Unzip the downloaded archive to get `.ovl` files

**File Naming Convention**: `senhub.{lookup_name}.ovl`

Example files:
```
senhub.netscaler.lbvserver.state.ovl
senhub.netscaler.ha.state.ovl
senhub.redfish.health.ovl
```

### Installing Lookups in PRTG

**Method 1: Manual Installation (Recommended)**

1. Download lookups from agent (see above)
2. Copy `.ovl` files to PRTG Server:
   ```
   C:\Program Files (x86)\PRTG Network Monitor\lookups\custom\
   ```
3. Refresh PRTG lookup cache:
   - Go to: **Setup → System Administration → Administrative Tools**
   - Click: **Load Lookups and File Lists**
   - Wait for confirmation message

**Method 2: Direct API Download**

```bash
# Download all lookups as ZIP
curl -o prtg-lookups.zip http://localhost:8080/api/{agentkey}/prtg/lookups/download

# Extract to PRTG custom lookups directory
unzip prtg-lookups.zip -d "C:\Program Files (x86)\PRTG Network Monitor\lookups\custom\"
```

### Using Lookups in PRTG

Once installed, lookups are **automatically applied** to matching metrics:

**Automatic Application:**
- PRTG matches the lookup file name to the metric name
- Example: Metric `netscaler.lbvserver.state` uses `senhub.netscaler.lbvserver.state.ovl`
- No manual configuration needed

**Verification:**
1. Create a sensor that collects the metric
2. View sensor channels
3. Status values should display as text instead of numbers
4. Channel background color should reflect severity (green/yellow/red)

**Example:**

Before lookup:
```
netscaler.lbvserver.state: 1
```

After lookup:
```
netscaler.lbvserver.state: UP (green background)
```

### Lookup File Format (.ovl)

Generated `.ovl` files use PRTG's XML format:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!--
  SenHub Agent - PRTG Value Lookup
  Lookup: netscaler.lbvserver.state
  Description: NetScaler Load Balancer Virtual Server State
  Source: Citrix ADC NITRO API - lbvserver state field
-->
<ValueLookup id="senhub.netscaler.lbvserver.state" desiredValue="1" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:noNamespaceSchemaLocation="PrtgLookup.xsd">
  <Lookups>
    <SingleInt state="Error" value="0">
      DOWN
    </SingleInt>
    <SingleInt state="Ok" value="1">
      UP
    </SingleInt>
    <SingleInt state="Warning" value="2">
      OUT OF SERVICE
    </SingleInt>
  </Lookups>
</ValueLookup>
```

**XML Elements:**
- `<ValueLookup id="...">` - Unique lookup identifier
- `desiredValue="1"` - Expected "good" value (optional)
- `<SingleInt state="Ok|Warning|Error" value="N">` - Value mapping
- Text content - Display text for the value

### Adding New Lookups

To add a new lookup:

1. **Edit lookups.yaml**:
   ```yaml
   my.metric.name:
     description: "My custom metric status"
     mappings:
       0: {text: "INACTIVE", severity: "warning"}
       1: {text: "ACTIVE", severity: "ok"}
     desired_value: 1
   ```

2. **Reference in transformer definition**:
   ```yaml
   # In netscaler.yaml, citrix.yaml, etc.
   - name: "my.metric.name"
     channel: "my_channel"
     display_name: "My Custom Metric"
     unit: "#"
     lookup: "my.metric.name"  # ← Add this line
   ```

3. **Rebuild agent**:
   ```bash
   make build
   ```

4. **Download updated lookups** from Web UI

### Troubleshooting

#### Lookup Not Applied in PRTG

**Symptom**: Metric shows numbers instead of text

**Solutions:**
1. Verify `.ovl` file exists in `C:\Program Files (x86)\PRTG Network Monitor\lookups\custom\`
2. Check file naming: Must be `senhub.{exact.metric.name}.ovl`
3. Refresh PRTG lookups: **Setup → Administrative Tools → Load Lookups**
4. Check PRTG logs for lookup parsing errors
5. Verify metric name matches exactly (case-sensitive)

#### Download Button Not Visible

**Symptom**: "Download PRTG Lookups" button missing in API Explorer

**Cause**: No lookups defined or embedded assets not loaded

**Solutions:**
1. Verify `lookups.yaml` exists and contains lookup definitions
2. Rebuild agent to embed updated assets
3. Check browser console for JavaScript errors

#### Incorrect Colors in PRTG

**Symptom**: Wrong severity colors displayed

**Cause**: Severity mapping doesn't match PRTG expectations

**Solution**: Update severity in `lookups.yaml`:
```yaml
mappings:
  0: {text: "DOWN", severity: "error"}    # Red
  1: {text: "UP", severity: "ok"}          # Green
  2: {text: "DEGRADED", severity: "warning"} # Yellow
```

### Best Practices

1. **Consistent Naming**: Use probe prefix (e.g., `netscaler.`, `redfish.`)
2. **Descriptive Text**: Use clear, standard terminology (UP/DOWN, not 1/0)
3. **Severity Accuracy**: Match severity to actual impact (error = outage)
4. **Document Source**: Include API field or calculation in `source` field
5. **Test Before Deploy**: Verify lookups work in dev PRTG before production
6. **Version Control**: Commit `lookups.yaml` changes with descriptive messages

### API Endpoints

**Download All Lookups (ZIP)**:
```
GET /api/{agentkey}/prtg/lookups/download
```

**Download Single Lookup**:
```
GET /api/{agentkey}/prtg/lookups/{lookup_name}.ovl
```

Example:
```bash
curl -o netscaler.ha.state.ovl \
  http://localhost:8080/api/{agentkey}/prtg/lookups/netscaler.ha.state.ovl
```

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