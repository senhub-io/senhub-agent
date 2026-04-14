---
title: "Web Interface"
weight: 4
---

# Web Interface

SenHub Agent includes a built-in web dashboard for monitoring agent status, exploring the API, and configuring your monitoring system integration.

## Accessing the Dashboard

Open a browser and navigate to:

```
http://agent-server:8080/web/{authentication-key}/
```

Replace `agent-server` with the hostname or IP address of the machine running the agent, and `{authentication-key}` with your agent's UUID key.

If HTTPS is enabled, use `https://` and the HTTPS port (default 8443):

```
https://agent-server:8443/web/{authentication-key}/
```

The web interface requires the `web` endpoint to be enabled in the storage configuration:

```yaml
storage:
  - name: http
    params:
      port: 8080
      endpoints: ["prtg", "web", "nagios"]
```

## Available Sections

### Dashboard

The main dashboard displays:

- Agent status and version
- Operating mode (online or offline)
- List of configured probes and their collection status
- Number of cached metrics per probe
- Memory and CPU usage
- Agent uptime

This page gives you a quick overview of the agent's health and activity.

### API Explorer

The API Explorer provides an interactive interface to:

- Browse all available API endpoints and their HTTP methods
- Test API calls directly from the browser with live responses
- View the raw JSON output from each endpoint
- Discover available probes and their metrics

This is especially useful when setting up PRTG, Nagios, or other monitoring tool integrations. You can see exactly what data is available and how it is formatted before configuring your monitoring system.

### Documentation

The Docs section provides embedded reference documentation accessible directly from the agent, without needing an internet connection. This is useful in air-gapped environments.

## Monitoring System Integration

### PRTG Network Monitor

#### Sensor Type

Use the **HTTP Data Advanced** sensor type in PRTG. This sensor sends an HTTP request to the agent and parses the JSON response.

#### Creating a PRTG Sensor

For each probe you want to monitor in PRTG:

1. In PRTG, right-click a device and select **Add Sensor**
2. Search for **HTTP Data Advanced** and select it
3. Configure the sensor:
   - **URL**: `http://agent-server:8080/api/{key}/prtg/metrics/{probe-name}`
   - **Request Method**: GET
   - **Content Type**: Leave default

The agent returns metrics in the PRTG JSON format:

```json
{
  "prtg": {
    "result": [
      {
        "channel": "CPU Usage",
        "value": 45.2,
        "float": 1,
        "unit": "Percent"
      },
      {
        "channel": "Memory Available",
        "value": 8192,
        "float": 1,
        "unit": "BytesMemory"
      }
    ]
  }
}
```

Each channel becomes a separate metric in PRTG with its own graph and alerting thresholds.

#### Finding Available Probe Names

To see which probe names are available for PRTG sensors:

```bash
curl http://agent-server:8080/api/{key}/prtg/probes
```

Or navigate to the API Explorer in the web dashboard.

#### Filtering Metrics by Tags

Some probes (Citrix, NetScaler) return metrics for multiple components. You can filter by tags:

```
http://agent-server:8080/api/{key}/prtg/metrics/{probe}?tags=vserver_name:vs_web
```

This returns only the metrics for the virtual server named `vs_web`.

#### Installing PRTG Value Lookups

SenHub Agent provides custom PRTG Lookup files that translate numeric status values into meaningful text (e.g., displaying "Up" instead of "1", "Down" instead of "0"):

1. Download the lookups from the agent API:
   ```bash
   curl -o senhub-prtg-lookups.zip http://agent-server:8080/api/{key}/lookups/prtg
   ```
   Or navigate to the API Explorer in the web dashboard and click the download button.

2. Extract the ZIP file to the PRTG custom lookups directory on your PRTG server:
   ```powershell
   Expand-Archive senhub-prtg-lookups.zip `
     -DestinationPath "C:\Program Files (x86)\PRTG Network Monitor\lookups\custom\"
   ```

3. In the PRTG web interface, go to **Administration > Administrative Tools** and click **Load Lookups and File Lists**

After loading the lookups, PRTG sensors will display human-readable status values instead of raw numbers.

The ZIP file contains lookup files in the PRTG `.ovl` format:
```
senhub-prtg-lookups.zip
  netscaler.lbvserver.state.ovl
  netscaler.service.state.ovl
  netscaler.ha.node.state.ovl
  ...
```

### Nagios

For Nagios, use the Nagios-formatted endpoints:

```
http://agent-server:8080/api/{key}/nagios/metrics/{probe-name}
```

The response follows the standard Nagios plugin output format:

```
OK - Probe has 12 metrics | cpu_usage=45.2% memory_available=8192MB
```

This can be used with `check_http` or a custom check command. Example Nagios command definition:

```
define command {
    command_name    check_senhub
    command_line    /usr/lib/nagios/plugins/check_http -H $HOSTADDRESS$ -p 8080 -u '/api/{key}/nagios/metrics/$ARG1$'
}
```

To list available checks:

```bash
curl http://agent-server:8080/api/{key}/nagios/checks
```


## Useful API Queries

### Check Agent Health

```bash
curl http://agent-server:8080/health
```

Response:
```json
{
  "status": "ok",
  "version": "0.1.80",
  "uptime": "2h30m",
  "probes_active": 4,
  "metrics_cached": 156
}
```

### List Configured Probes

```bash
curl http://agent-server:8080/api/{key}/info/probes
```

Response:
```json
{
  "probes": ["CPU", "Memory", "Citrix Production", "NetScaler LB"],
  "probe_metrics": {
    "CPU": 4,
    "Memory": 3,
    "Citrix Production": 85,
    "NetScaler LB": 64
  },
  "total_metrics": 156
}
```

### View System Information

```bash
curl http://agent-server:8080/api/{key}/info/system
```

Returns version, uptime, memory usage, CPU usage, cache statistics, and service health.

### Check License Status

```bash
curl http://agent-server:8080/api/{key}/license/status
```

Response:
```json
{
  "status": "active",
  "tier": "pro",
  "expires_at": "2026-06-30T23:59:59Z",
  "days_remaining": 120,
  "authorized_probes": ["cpu", "memory", "citrix", "netscaler", "redfish"],
  "free_tier_probes": ["cpu", "memory", "logicaldisk", "network"]
}
```

### View Active Configuration

```bash
curl http://agent-server:8080/api/{key}/config/probes
```

Returns the active probe configuration as read from `agent-config.yaml`.

### Cache Statistics

```bash
curl http://agent-server:8080/api/{key}/stats/cache
```

Returns the number of cached metrics, memory usage, and TTL (time-to-live) of the cache.

### Clear the Cache

If you need to force a fresh collection:

```bash
curl -X POST http://agent-server:8080/api/{key}/admin/cache/clear
```

This clears all cached metrics. The next collection cycle will repopulate the cache.
