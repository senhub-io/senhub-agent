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

Monolithic layout (`agent-config.yaml`):

```yaml
storage:
  - name: http
    params:
      port: 8080
      bind_address: "127.0.0.1"
      endpoints: ["prtg", "web", "nagios"]
```

Multi-file layout (`strategies.d/00-http.yaml`), one top-level key per file:

```yaml
http:
  port: 8080
  bind_address: "127.0.0.1"
  endpoints: ["prtg", "web", "nagios"]
```

### Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `port` | integer | 8080 | Listening port |
| `bind_address` | string | `127.0.0.1` | Interface to bind; `0.0.0.0` to accept remote pollers |
| `endpoints` | string list | none | Which endpoint families are served: `prtg`, `nagios`, `prometheus`, `web`. An unknown value is refused at startup |
| `admin_key` | string | none | Key opening the administration surface. Without it, none of that surface is served — see [Two keys, two surfaces](#two-keys-two-surfaces) |
| `max_cache_size` | integer | 50000 | Cap on cached series; `0` = unbounded |
| `tls.enabled` | boolean | `false` | Serve HTTPS — see [HTTPS Configuration](./HTTPS-CONFIGURATION.md) |
| `tls.min_tls_version` | string | `1.2` | `1.2` or `1.3` |
| `tls.cert_file` / `tls.key_file` | string | generated pair | Server certificate and key (PEM) |
| `prometheus.include_probe_tags` | boolean | `true` | Emit probe tags as Prometheus labels |
| `prometheus.expose_host_metrics` | boolean | `true` | Include the host probes in `/metrics` |

## Two keys, two surfaces

The agent key is what a monitoring tool is given to read this agent:
PRTG, Nagios, a Prometheus scrape. It travels in the URL path, so it
lands in the access log of every machine between the poller and the
agent.

That key used to open everything the output serves, including the parts
that **change** the agent — clearing the metric cache, injecting values
into it, changing log levels, reading the agent's own logs, editing
probes and outputs through the console. A read-only consumer therefore
held the means to falsify what it read.

The two are now separate.

```yaml
http:
  port: 8080
  endpoints: ["prtg", "web"]
  admin_key: "${secret:agent.admin_key}"
```

| Surface | Opened by | What it serves |
|---|---|---|
| Read | the agent key, or the administration key | `/metrics`, the PRTG, Nagios and Prometheus endpoints, `info/*`, the metric cache and its statistics |
| Administration | the administration key **only** | the web console, the configuration API, log levels, cache clearing, metric injection, the profiler |

The administration key carries the read privilege: the console reads as
much as it writes, and making an operator juggle two keys in one page
would only invite them to share the stronger one.

**Without `admin_key`, the administration surface is not served at all.**
Its routes are not registered, so they answer 404 rather than asking for
a key nobody has. An installation that exists to feed PRTG or Nagios
never needed that surface, and no longer carries it.

### What this changes for an existing installation

If you use the web console or the configuration API, set `admin_key` and
open the console with it — `/web/<admin_key>/dashboard`. Until you do,
the console answers 404 and your pollers keep working untouched.

Give the administration key to nobody who only needs to read. It is the
one to store in a secret backend and rotate; the key in your PRTG sensor
is not.

No endpoint is served unless it is listed in `endpoints`.

### Metric Naming

Channel names are not configured on the output. They come from the shipped
transformer definitions, which map an OTel metric name to a display name and a
unit per probe. There is no `naming` parameter.

## Endpoints

### PRTG Metrics Endpoint

**`GET /api/{agentkey}/prtg/metrics/{probe}`**

Returns what the named probe last collected, in PRTG-compatible JSON. This is
the form a PRTG sensor uses.

**`POST /api/{agentkey}/prtg/metrics`** is the legacy form, kept for older
sensors. It reads `probe` from the body and answers the same payload; the
`target` and `config` fields are accepted and **ignored**. The agent serves
what its own configured probes collected — an endpoint cannot ask it to go and
poll a device it is not configured for, and credentials do not belong in a
sensor definition.

#### Authentication
- Agent key must be provided in the URL path
- Invalid keys return `HTTP 401 Unauthorized`

#### Legacy request format
```json
{
  "probe": "redfish"
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

Returns server health status. It is the only route served without an agent
key — the container image's `HEALTHCHECK` uses it.

#### Response
```json
{
  "status": "ok"
}
```

### Caching

Every pull endpoint answers with `Cache-Control: no-store`. These routes carry
the current value of a measurement, with no validator and no expiry; without
the header a poller or an intermediary is free to reuse an earlier answer and
chart a value the agent never reported at that instant. The embedded console
assets set their own policy and are still revalidated normally.

## Metric Transformations

### Configuration Files

Transformations are defined in YAML files under
`internal/agent/services/data_store/transformers/` and **embedded in the
binary at build time**:

- `definitions/<probe>.yaml`: the current per-probe definitions (format v3)
- `cpu_friendly.yaml`, `memory_friendly.yaml`, `network_friendly.yaml`,
  `logicaldisk_friendly.yaml`, `system_friendly.yaml`, `redfish_friendly.yaml`:
  the older friendly-name maps

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
3. **Rebuild the agent** (`make build`) — the definitions are embedded, so a
   restart alone changes nothing

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

The authoritative list is what the agent serves at
`GET /api/{agentkey}/lookups`. At the time of writing the shipped set covers:

#### NetScaler ADC

- **`netscaler.lbvserver.state`**, **`netscaler.csvserver.state`**,
  **`netscaler.gslbvserver.state`** - virtual server state
- **`netscaler.service.state`**, **`netscaler.servicegroup.state`** - backend state
- **`netscaler.interface.state`** - Network Interface state (ENABLED/DISABLED)
- **`netscaler.ssl.certificate.status`** - SSL Certificate validity (VALID/INVALID)
- **`netscaler.ha.state`**, **`netscaler.ha.node.state`**,
  **`netscaler.ha.sync_status`** - HA role, node state, config sync

#### Veeam

- **`senhub.veeam.job_status`**, **`senhub.veeam.server_status`**,
  **`senhub.veeam.proxy_status`**, **`senhub.veeam.license_status`**,
  **`senhub.veeam.bottleneck`**

#### Databases and stores

- **`senhub.db.up`**, **`senhub.db.replication.role`**,
  **`senhub.db.replication.health`**, **`mssql.database.status`**
- **`redis.replication.role`**, **`redis.cluster.state`**
- **`elasticsearch.cluster.health`**, **`opensearch.cluster.health`**

#### Storage and hardware

- **`senhub.powerstore.up`**, **`senhub.powerstore.health`**,
  **`senhub.powerstore.cluster.state`**, **`senhub.powerstore.volume.state`**,
  **`senhub.powerstore.replication.state`**
- **`smart.disk.health`**

### Downloading Lookups

The agent provides **automatic lookup file generation** accessible via the Web UI:

1. **Open the console**: Navigate to `http://localhost:8080/web/{agentkey}/`
2. **Go to the Sensor URLs tab**: Outputs, then the HTTP output, tab "Sensor URLs"
3. **Download Lookups**: Click **"Download PRTG lookups"**
4. **Extract Files**: Unzip the downloaded archive to get `.ovl` files

**File Naming Convention**: `{lookup_id}.ovl` — the filename must match the
`id` attribute in the XML, which is the lookup key exactly as it appears in
`lookups.yaml` (no prefix is added).

Example files:
```
netscaler.lbvserver.state.ovl
netscaler.ha.state.ovl
netscaler.ha.node.state.ovl
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
curl -o prtg-lookups.zip http://localhost:8080/api/{agentkey}/lookups/prtg

# Extract to PRTG custom lookups directory
unzip prtg-lookups.zip -d "C:\Program Files (x86)\PRTG Network Monitor\lookups\custom\"
```

### Using Lookups in PRTG

Once installed, lookups are **automatically applied** to matching metrics:

**Automatic Application:**
- PRTG matches the lookup file name to the metric name
- Example: Metric `netscaler.lbvserver.state` uses `netscaler.lbvserver.state.ovl`
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
<ValueLookup id="netscaler.lbvserver.state" desiredValue="1">
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
2. Check file naming: must be the lookup id exactly, e.g.
   `netscaler.lbvserver.state.ovl` or `senhub.veeam.job_status.ovl`
3. Refresh PRTG lookups: **Setup → Administrative Tools → Load Lookups**
4. Check PRTG logs for lookup parsing errors
5. Verify metric name matches exactly (case-sensitive)

#### Download Button Not Visible

**Symptom**: the "Download PRTG lookups" link is missing from the HTTP
output's **Sensor URLs** tab

**Cause**: the `web` endpoint is not enabled, or the embedded assets were not
rebuilt

**Solutions:**
1. Check `web` is listed in the output's `endpoints`
2. Verify `lookups.yaml` contains lookup definitions
3. Rebuild the agent to embed updated assets
4. Check the browser console for JavaScript errors

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
GET /api/{agentkey}/lookups/prtg
```

**Download Single Lookup**:
```
GET /api/{agentkey}/lookups/prtg/{lookup_id}
```

Example:
```bash
curl -o netscaler.ha.state.ovl \
  http://localhost:8080/api/{agentkey}/lookups/prtg/netscaler_ha_state
```

## Cache Management

### TTL and Cleanup

- **Default TTL**: 5 minutes, from `cache.retention_minutes` in `agent.yaml`
- **Cleanup Interval**: half the TTL (2m30s at the default), as a background process
- **Cardinality cap**: `max_cache_size`, 50 000 series by default
- **Thread Safety**: All cache operations are protected by read-write mutexes

### Memory Management

The cache automatically removes expired metrics to prevent memory leaks. Cache size is bounded by:
- TTL-based expiration
- Number of active probes and their metrics
- Frequency of metric collection

## Integration Examples

### PRTG Network Monitor

1. Create an HTTP Advanced sensor in PRTG
2. Point it at the probe you want, one sensor per probe
3. Set up JSON parsing for PRTG channels
4. Schedule regular monitoring intervals

```bash
# Example PRTG sensor configuration
URL: http://agent-host:8080/api/your-agent-key/prtg/metrics/redfish
Method: GET
```

The agent's console generates these URLs for you: **Outputs**, the HTTP
output, tab **Sensor URLs**.

### Custom Monitoring Tools

The JSON response format can be easily parsed by custom scripts:

```python
import requests

response = requests.get(
    'http://agent-host:8080/api/your-agent-key/prtg/metrics/host'
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
./senhub-agent run --verbose
```

### Performance Monitoring

Monitor these metrics for optimal performance:
- Cache hit/miss ratios
- Request response times
- Memory usage of cached metrics
- Transformation processing time

## Shipped since this page was first written

- **Prometheus format**: add `prometheus` to `endpoints` and scrape
  `/metrics`, which authenticates with `Authorization: Bearer <agentkey>` or
  `?token=<agentkey>` — what vmagent, Prometheus and Grafana Alloy send
  natively. `/api/{agentkey}/prometheus/metrics` serves the same exposition
  with the key in the path.
- **Configuration from the console**: the `web` endpoint creates, edits and
  deletes probes and outputs, writing managed files under `probes.d/` and
  `strategies.d/`.
- **Metric filtering**: the PRTG GET route accepts tag filters as query
  parameters.

Still open: request rate limiting, and bulk export for analysis tools.

### Extension Points

The HTTP Strategy is designed for extensibility:
- Additional response formats can be added
- New transformation styles can be implemented
- Custom authentication methods can be integrated
- Additional endpoints can be registered