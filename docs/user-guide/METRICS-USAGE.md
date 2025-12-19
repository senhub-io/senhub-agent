# SenHub Agent - Metrics Usage

## Table of Contents

- [Overview](#overview)
- [PRTG Integration](#prtg-integration)
- [Nagios Integration](#nagios-integration)
- [Grafana Integration](#grafana-integration)
- [Custom REST API](#custom-rest-api)
- [Examples by Probe](#examples-by-probe)
- [Best Practices](#best-practices)
- [Troubleshooting](#troubleshooting)

---

## Overview

SenHub Agent exposes collected metrics via multiple formats adapted to popular monitoring tools.

```mermaid
graph TD
    AGENT[SenHub Agent<br/>HTTP Strategy] --> PRTG[PRTG Network Monitor<br/>XML Format]
    AGENT --> NAGIOS[Nagios/Icinga<br/>Text Format]
    AGENT --> GRAFANA[Grafana<br/>JSON Format]
    AGENT --> CUSTOM[Custom Tools<br/>REST JSON API]

    PRTG --> P1[HTTP XML Sensors]
    PRTG --> P2[.ovl Lookups]
    NAGIOS --> N1[check_http plugin]
    NAGIOS --> N2[Custom NRPE]
    GRAFANA --> G1[JSON API datasource]
    CUSTOM --> C1[Python/PowerShell Scripts]

    style AGENT fill:#81d4fa
    style PRTG fill:#fff9c4
    style NAGIOS fill:#c8e6c9
    style GRAFANA fill:#f8bbd0
```

### Available Formats

| Format | Endpoint | Target Tool | Description |
|--------|----------|-------------|-------------|
| **PRTG XML** | `/api/{key}/prtg/metrics` | PRTG | Native PRTG XML format |
| **Nagios Text** | `/api/{key}/nagios/status` | Nagios/Icinga | Text performance data format |
| **JSON** | `/api/{key}/metrics` | Grafana/Custom | Structured JSON format |
| **Lookups** | `/api/{key}/prtg/lookups/download` | PRTG | .ovl files for labels |

---

## PRTG Integration

PRTG Network Monitor can consume SenHub Agent metrics via HTTP XML/REST sensors.

```mermaid
graph LR
    PRTG[PRTG Core] -->|HTTP GET| AGENT[SenHub Agent]
    AGENT -->|XML Response| PRTG
    PRTG -->|Parse| SENSORS[Sensors/Channels]
    LOOKUPS[Lookups .ovl] -->|Labels| SENSORS

    style PRTG fill:#fff9c4
    style AGENT fill:#81d4fa
```

### PRTG Sensor Configuration

#### Sensor Type: HTTP XML/REST Value

**Steps**:

1. **Add Device in PRTG**
   - IP/Hostname: `monitoring.company.com` (or IP)
   - Port: Leave empty (handled by URL)

2. **Add Sensor**
   - Type: **HTTP XML/REST Value Sensor**
   - Name: `SenHub Agent - CPU Metrics`

3. **Sensor Configuration**

**Basic Settings**:
```
Sensor Name: SenHub Agent - CPU Metrics
Tags: senhub, system, cpu
Priority: 3 stars
```

**HTTP Specific**:
```
REST Configuration:
  URL: https://monitoring.company.com:8443/api/f47ac10b-58cc-4372-a567-0e02b2c3d479/prtg/metrics/cpu

Port: [empty - included in URL]
Method: GET
Request Headers: [empty]
Authentication: None (key in URL)
Timeout (Sec): 60
```

**REST Configuration**:
```
REST Query: [empty]
Content Type: [empty - auto-detect XML]
```

**Scanning Interval**:
```
Scanning Interval: 60 seconds
```

**📸 SCREENSHOT TO INSERT**: PRTG sensor configuration showing URL and REST Configuration fields

---

#### URLs by Probe

**System Probes (Free)**:
```
CPU:
https://monitoring.company.com:8443/api/{key}/prtg/metrics/cpu

Memory:
https://monitoring.company.com:8443/api/{key}/prtg/metrics/memory

Logical Disk:
https://monitoring.company.com:8443/api/{key}/prtg/metrics/logicaldisk

Network:
https://monitoring.company.com:8443/api/{key}/prtg/metrics/network
```

**Infrastructure Probes (Pro/Enterprise)**:
```
Redfish:
https://monitoring.company.com:8443/api/{key}/prtg/metrics/redfish

Citrix:
https://monitoring.company.com:8443/api/{key}/prtg/metrics/citrix

NetScaler:
https://monitoring.company.com:8443/api/{key}/prtg/metrics/netscaler

Syslog:
https://monitoring.company.com:8443/api/{key}/prtg/metrics/syslog
```

---

### Example: Complete CPU Sensor

**PRTG result after configuration**:

```
Sensor: SenHub Agent - CPU Metrics
Status: Up (100%)
Last Scan: 30 seconds ago

Channels:
├─ CPU Usage Total: 45.2% [OK]
├─ CPU User: 32.1% [OK]
├─ CPU System: 13.1% [OK]
├─ CPU Load 1min: 1.23 [OK]
├─ CPU Load 5min: 1.45 [OK]
├─ CPU Load 15min: 1.67 [OK]
├─ CPU Core 0 Usage: 48.3% [OK]
├─ CPU Core 1 Usage: 42.1% [OK]
└─ ... (all cores)
```

**Configurable limits**:
- Warning: 80%
- Error: 95%

**📸 SCREENSHOT TO INSERT**: PRTG sensor showing all CPU channels with graphs

---

### Installing PRTG Lookups

Lookups allow displaying text labels instead of numeric codes for NetScaler.

**Step 1: Download lookups**

Via SenHub Agent web interface:
```
Dashboard → API Explorer → PRTG Lookups → Download
```

Or directly:
```
https://monitoring.company.com:8443/api/{key}/prtg/lookups/download
```

**Step 2: Extract ZIP**

Contents:
```
senhub-lookups.zip
├─ netscaler.metric_type.ovl
├─ netscaler.metric_view.ovl
└─ README.txt
```

**Step 3: Copy to PRTG**

```powershell
# Windows - PRTG Server
Copy-Item *.ovl "C:\Program Files (x86)\PRTG Network Monitor\lookups\custom\"
```

**Step 4: Reload lookups**

In PRTG:
```
Setup → Administrative Tools → Load Lookups and File Lists
```

**Verification**:
- NetScaler sensors now display "Rate" instead of "0"
- Labels "Load Balancing", "SSL", "System" instead of codes

**📸 SCREENSHOT TO INSERT**: Before/after lookups installation (codes vs text labels)

---

### NetScaler Sensor with Filtering

**Create multiple filtered sensors**:

**Sensor 1: Load Balancing only**
```
Name: NetScaler - Load Balancing
URL: https://monitoring.company.com:8443/api/{key}/prtg/metrics/netscaler?filter=metric_view:load_balancing
```

**Sensor 2: SSL Monitoring**
```
Name: NetScaler - SSL Metrics
URL: https://monitoring.company.com:8443/api/{key}/prtg/metrics/netscaler?filter=metric_view:ssl
```

**Sensor 3: Specific Virtual Server**
```
Name: NetScaler - Web vServer
URL: https://monitoring.company.com:8443/api/{key}/prtg/metrics/netscaler?filter=vserver_name:Web-vServer
```

**Benefits**:
- Lighter sensors (fewer channels)
- Clear organization by function
- Targeted alerts

---

### Multi-Instance with PRTG

**Scenario**: Multiple servers with SenHub Agent

```
Device: PROD-SERVER-01 (192.168.1.10:8443)
├─ Sensor: CPU Metrics
│  URL: https://192.168.1.10:8443/api/{key-01}/prtg/metrics/cpu
├─ Sensor: Memory Metrics
│  URL: https://192.168.1.10:8443/api/{key-01}/prtg/metrics/memory
└─ Sensor: Redfish Hardware
   URL: https://192.168.1.10:8443/api/{key-01}/prtg/metrics/redfish

Device: PROD-SERVER-02 (192.168.1.11:8443)
├─ Sensor: CPU Metrics
│  URL: https://192.168.1.11:8443/api/{key-02}/prtg/metrics/cpu
└─ ...
```

**📸 SCREENSHOT TO INSERT**: PRTG tree view with multiple SenHub Agent devices

---

## Nagios Integration

Nagios/Icinga can monitor SenHub Agent via the `check_http` plugin.

```mermaid
graph LR
    NAGIOS[Nagios Core] -->|check_http| AGENT[SenHub Agent]
    AGENT -->|Text Response| NAGIOS
    NAGIOS -->|Parse Perfdata| GRAPHS[PNP4Nagios/Grafana]

    style NAGIOS fill:#c8e6c9
    style AGENT fill:#81d4fa
```

### Nagios Check Configuration

#### Command Definition

**`/etc/nagios/objects/commands.cfg`**:

```cfg
define command {
    command_name    check_senhub_metrics
    command_line    $USER1$/check_http -H $ARG1$ -p $ARG2$ -S \
                    -u /api/$ARG3$/nagios/status \
                    -s "OK" \
                    -w 5 -c 10
}
```

**Parameters**:
- `-H $ARG1$`: Hostname/IP
- `-p $ARG2$`: Port (8443)
- `-S`: Use HTTPS
- `-u /api/$ARG3$/nagios/status`: URL endpoint
- `-s "OK"`: String to expect
- `-w 5 -c 10`: Timeout warning/critical (seconds)

---

#### Service Definition

**`/etc/nagios/objects/services.cfg`**:

```cfg
define service {
    use                     generic-service
    host_name               PROD-SERVER-01
    service_description     SenHub Agent - System Metrics
    check_command           check_senhub_metrics!monitoring.company.com!8443!f47ac10b-58cc-4372-a567-0e02b2c3d479
    check_interval          5
    retry_interval          1
}
```

**Nagios Result**:
```
OK - CPU: 45.2% | cpu_usage=45.2%;80;95;0;100 memory_usage=67.8%;80;95;0;100
```

**Performance Data**:
```
cpu_usage=45.2%;80;95;0;100
memory_usage=67.8%;80;95;0;100
disk_c_usage=35.4%;80;95;0;100
network_eth0_bytes_sent=1234567
```

**📸 SCREENSHOT TO INSERT**: Nagios service status showing SenHub checks with performance data

---

### Advanced Check per Probe

**Define separate checks**:

```cfg
# CPU Check
define service {
    host_name               PROD-SERVER-01
    service_description     SenHub - CPU
    check_command           check_senhub_metrics!monitoring.company.com!8443!{key}
    check_interval          1
}

# Memory Check
define service {
    host_name               PROD-SERVER-01
    service_description     SenHub - Memory
    check_command           check_senhub_metrics!monitoring.company.com!8443!{key}
    check_interval          1
}

# Redfish Check
define service {
    host_name               PROD-SERVER-01
    service_description     SenHub - Hardware (Redfish)
    check_command           check_senhub_metrics!monitoring.company.com!8443!{key}
    check_interval          5
}
```

**Note**: Currently the `/nagios/status` endpoint returns all metrics. Filtering by probe coming in future version.

---

### Custom NRPE Script

For more flexibility, create a custom NRPE script.

**`/usr/lib/nagios/plugins/check_senhub_custom.sh`**:

```bash
#!/bin/bash

HOST=$1
PORT=$2
KEY=$3
PROBE=$4

URL="https://${HOST}:${PORT}/api/${KEY}/metrics?probe=${PROBE}"

# Retrieve JSON metrics
RESPONSE=$(curl -s -k "$URL")

# Parse with jq
CPU_USAGE=$(echo "$RESPONSE" | jq -r '.metrics[] | select(.name=="cpu_usage_total") | .value')

# Apply thresholds
if (( $(echo "$CPU_USAGE > 95" | bc -l) )); then
    echo "CRITICAL - CPU: ${CPU_USAGE}% | cpu_usage=${CPU_USAGE};80;95;0;100"
    exit 2
elif (( $(echo "$CPU_USAGE > 80" | bc -l) )); then
    echo "WARNING - CPU: ${CPU_USAGE}% | cpu_usage=${CPU_USAGE};80;95;0;100"
    exit 1
else
    echo "OK - CPU: ${CPU_USAGE}% | cpu_usage=${CPU_USAGE};80;95;0;100"
    exit 0
fi
```

**Make executable**:
```bash
chmod +x /usr/lib/nagios/plugins/check_senhub_custom.sh
```

**Command definition**:
```cfg
define command {
    command_name    check_senhub_custom
    command_line    $USER1$/check_senhub_custom.sh $ARG1$ $ARG2$ $ARG3$ $ARG4$
}
```

**Service**:
```cfg
define service {
    host_name               PROD-SERVER-01
    service_description     SenHub - CPU Custom
    check_command           check_senhub_custom!monitoring.company.com!8443!{key}!cpu
}
```

---

## Grafana Integration

Grafana can consume SenHub Agent metrics via the JSON API datasource plugin.

```mermaid
graph LR
    GRAFANA[Grafana] -->|HTTP GET JSON| AGENT[SenHub Agent]
    AGENT -->|JSON Response| GRAFANA
    GRAFANA -->|Transform| PANELS[Panels/Dashboards]

    style GRAFANA fill:#f8bbd0
    style AGENT fill:#81d4fa
```

### Installing JSON API Plugin

```bash
grafana-cli plugins install simpod-json-datasource
systemctl restart grafana-server
```

### Datasource Configuration

**Grafana UI**:

1. **Configuration → Data Sources → Add data source**
2. **Type**: JSON API
3. **Settings**:

```
Name: SenHub Agent - PROD-SERVER-01
URL: https://monitoring.company.com:8443
```

4. **Custom HTTP Headers**:
```
Header: X-API-Key
Value: f47ac10b-58cc-4372-a567-0e02b2c3d479
```

5. **TLS Settings**:
```
Skip TLS Verify: ☑ (if self-signed certificate)
```

6. **Save & Test**

**📸 SCREENSHOT TO INSERT**: Grafana datasource configuration for SenHub Agent

---

### Creating a Dashboard

**Panel 1: CPU Usage**

```json
{
  "title": "CPU Usage",
  "type": "graph",
  "datasource": "SenHub Agent - PROD-SERVER-01",
  "targets": [
    {
      "target": "cpu_usage_total",
      "refId": "A",
      "type": "timeserie",
      "data": {
        "url": "/api/f47ac10b-58cc-4372-a567-0e02b2c3d479/metrics?probe=cpu"
      }
    }
  ]
}
```

**Panel 2: Memory Usage**

```json
{
  "title": "Memory Usage",
  "type": "gauge",
  "datasource": "SenHub Agent - PROD-SERVER-01",
  "targets": [
    {
      "target": "memory_usage_percent",
      "data": {
        "url": "/api/{key}/metrics?probe=memory"
      }
    }
  ],
  "options": {
    "min": 0,
    "max": 100,
    "thresholds": {
      "mode": "absolute",
      "steps": [
        { "value": 0, "color": "green" },
        { "value": 80, "color": "yellow" },
        { "value": 95, "color": "red" }
      ]
    }
  }
}
```

**📸 SCREENSHOT TO INSERT**: Grafana dashboard with CPU, Memory, Network panels

---

### Grafana Transformation

**Transform JSON → Time Series**:

SenHub metrics are exposed with timestamps. Grafana can transform them directly.

**JSON response example**:
```json
{
  "metrics": [
    {
      "name": "cpu_usage_total",
      "value": 45.2,
      "unit": "percent",
      "timestamp": "2025-01-15T10:30:45Z"
    }
  ]
}
```

**Grafana Query**:
```
Metric: cpu_usage_total
JSONPath: $.metrics[?(@.name=='cpu_usage_total')].value
```

---

### Multi-Server Dashboard

**Grafana Variables**:

```
Variable: server
Type: Custom
Values: server01, server02, server03

Variable: probe
Type: Custom
Values: cpu, memory, logicaldisk, redfish
```

**Dynamic query**:
```
URL: https://$server.company.com:8443/api/{key}/metrics?probe=$probe
```

**Result**:
- Dropdown to select server
- Dropdown to select probe
- Panels update automatically

**📸 SCREENSHOT TO INSERT**: Grafana dashboard with variables and multi-panels

---

## Custom REST API

For custom integrations (scripts, in-house tools), use the JSON REST API.

### Python Examples

**Script 1: Retrieve all metrics**

```python
#!/usr/bin/env python3
import requests
import json

AGENT_URL = "https://monitoring.company.com:8443"
API_KEY = "f47ac10b-58cc-4372-a567-0e02b2c3d479"

def get_all_metrics():
    url = f"{AGENT_URL}/api/{API_KEY}/metrics"
    response = requests.get(url, verify=False)  # verify=True in prod

    if response.status_code == 200:
        data = response.json()
        print(json.dumps(data, indent=2))
        return data
    else:
        print(f"Error: {response.status_code}")
        return None

if __name__ == "__main__":
    metrics = get_all_metrics()
```

---

**Script 2: Custom alerting**

```python
#!/usr/bin/env python3
import requests
import smtplib
from email.mime.text import MIMEText

AGENT_URL = "https://monitoring.company.com:8443"
API_KEY = "f47ac10b-58cc-4372-a567-0e02b2c3d479"
CPU_THRESHOLD = 80.0
MEMORY_THRESHOLD = 90.0

def check_metrics():
    url = f"{AGENT_URL}/api/{API_KEY}/metrics?probe=cpu,memory"
    response = requests.get(url, verify=False)

    if response.status_code != 200:
        send_alert("Agent unreachable", f"HTTP {response.status_code}")
        return

    data = response.json()

    for metric in data.get("metrics", []):
        name = metric.get("name")
        value = metric.get("value")

        if name == "cpu_usage_total" and value > CPU_THRESHOLD:
            send_alert(
                f"CPU High: {value}%",
                f"CPU usage exceeded threshold ({CPU_THRESHOLD}%)"
            )

        if name == "memory_usage_percent" and value > MEMORY_THRESHOLD:
            send_alert(
                f"Memory High: {value}%",
                f"Memory usage exceeded threshold ({MEMORY_THRESHOLD}%)"
            )

def send_alert(subject, body):
    msg = MIMEText(body)
    msg["Subject"] = f"[SenHub Alert] {subject}"
    msg["From"] = "monitoring@company.com"
    msg["To"] = "admin@company.com"

    smtp = smtplib.SMTP("localhost")
    smtp.send_message(msg)
    smtp.quit()

    print(f"Alert sent: {subject}")

if __name__ == "__main__":
    check_metrics()
```

**Cron**:
```bash
# Check every 5 minutes
*/5 * * * * /usr/local/bin/check_senhub_alerts.py
```

---

**Script 3: CSV Export**

```python
#!/usr/bin/env python3
import requests
import csv
from datetime import datetime

AGENT_URL = "https://monitoring.company.com:8443"
API_KEY = "f47ac10b-58cc-4372-a567-0e02b2c3d479"

def export_metrics_csv(probe, output_file):
    url = f"{AGENT_URL}/api/{API_KEY}/metrics?probe={probe}"
    response = requests.get(url, verify=False)

    if response.status_code != 200:
        print(f"Error: {response.status_code}")
        return

    data = response.json()

    with open(output_file, 'w', newline='') as csvfile:
        fieldnames = ['timestamp', 'metric_name', 'value', 'unit', 'tags']
        writer = csv.DictWriter(csvfile, fieldnames=fieldnames)

        writer.writeheader()

        for metric in data.get("metrics", []):
            writer.writerow({
                'timestamp': metric.get('timestamp', datetime.now().isoformat()),
                'metric_name': metric.get('name'),
                'value': metric.get('value'),
                'unit': metric.get('unit', ''),
                'tags': str(metric.get('tags', {}))
            })

    print(f"Exported {len(data.get('metrics', []))} metrics to {output_file}")

if __name__ == "__main__":
    export_metrics_csv("cpu", "cpu_metrics.csv")
    export_metrics_csv("redfish", "redfish_metrics.csv")
```

---

### PowerShell Examples

**Script 1: Retrieve metrics**

```powershell
$AgentUrl = "https://monitoring.company.com:8443"
$ApiKey = "f47ac10b-58cc-4372-a567-0e02b2c3d479"

function Get-SenHubMetrics {
    param(
        [string]$Probe = ""
    )

    $url = "$AgentUrl/api/$ApiKey/metrics"
    if ($Probe) {
        $url += "?probe=$Probe"
    }

    try {
        $response = Invoke-RestMethod -Uri $url -Method Get -SkipCertificateCheck
        return $response
    }
    catch {
        Write-Error "Failed to get metrics: $_"
        return $null
    }
}

# Usage
$metrics = Get-SenHubMetrics -Probe "cpu"
$metrics.metrics | Format-Table -Property name, value, unit
```

---

**Script 2: Windows Event Log monitoring**

```powershell
$AgentUrl = "https://monitoring.company.com:8443"
$ApiKey = "f47ac10b-58cc-4372-a567-0e02b2c3d479"

function Check-SenHubHealth {
    $url = "$AgentUrl/api/$ApiKey/info/system"

    try {
        $response = Invoke-RestMethod -Uri $url -Method Get -SkipCertificateCheck

        Write-EventLog -LogName Application -Source "SenHub Monitor" `
            -EntryType Information -EventId 1000 `
            -Message "SenHub Agent healthy: Version $($response.agent_version), Uptime $($response.uptime_seconds)s"

        return $true
    }
    catch {
        Write-EventLog -LogName Application -Source "SenHub Monitor" `
            -EntryType Error -EventId 1001 `
            -Message "SenHub Agent unreachable: $_"

        return $false
    }
}

# Create event source (once)
New-EventLog -LogName Application -Source "SenHub Monitor" -ErrorAction SilentlyContinue

# Check
Check-SenHubHealth
```

**Scheduled Task**:
```powershell
$action = New-ScheduledTaskAction -Execute "PowerShell.exe" `
    -Argument "-File C:\Scripts\Check-SenHubHealth.ps1"

$trigger = New-ScheduledTaskTrigger -Once -At (Get-Date) `
    -RepetitionInterval (New-TimeSpan -Minutes 5)

Register-ScheduledTask -TaskName "SenHub Health Check" `
    -Action $action -Trigger $trigger -User "SYSTEM"
```

---

## Examples by Probe

### CPU Metrics

**PRTG**:
```
URL: https://monitoring.company.com:8443/api/{key}/prtg/metrics/cpu
Interval: 60s
Channels: cpu_usage_total, cpu_load1, cpu_load5, cpu_core_*
```

**Nagios**:
```bash
check_http -H monitoring.company.com -p 8443 -S \
  -u /api/{key}/nagios/status \
  -s "OK - CPU"
```

**Grafana Panel**:
```json
{
  "title": "CPU Usage",
  "type": "timeseries",
  "targets": [
    {"metric": "cpu_usage_total"},
    {"metric": "cpu_user"},
    {"metric": "cpu_system"}
  ]
}
```

---

### Memory Metrics

**PRTG**:
```
URL: https://monitoring.company.com:8443/api/{key}/prtg/metrics/memory
Channels: memory_usage_percent, memory_available, swap_used
Limits: Warning 80%, Error 95%
```

**Grafana Gauge**:
```json
{
  "title": "Memory Usage",
  "type": "gauge",
  "options": {
    "thresholds": [
      {"value": 0, "color": "green"},
      {"value": 80, "color": "yellow"},
      {"value": 95, "color": "red"}
    ]
  }
}
```

---

### Redfish Hardware

**PRTG Multi-Sensor**:
```
Sensor 1: Temperatures
URL: .../prtg/metrics/redfish
Filter channels: *temperature*

Sensor 2: Fan Speeds
Filter channels: *fan_speed*

Sensor 3: Power
Filter channels: *power*
```

**Alerts**:
- Temperature > 75°C: Warning
- Temperature > 85°C: Critical
- Fan Speed < 30%: Warning
- Fan Speed = 0%: Critical

**📸 SCREENSHOT TO INSERT**: PRTG Redfish sensors with temperatures, fans and power

---

### Citrix VDI

**PRTG Sensors**:
```
Sensor 1: Session Metrics
URL: .../prtg/metrics/citrix
Channels: active_sessions, disconnected_sessions

Sensor 2: Logon Performance
Channels: logon_duration_seconds, logon_success_rate

Sensor 3: Server Load
Channels: server_load_percent, server_session_count
```

**Grafana Dashboard**:
```
Panel 1: Active Sessions (Time Series)
Panel 2: Logon Duration (Heatmap)
Panel 3: Server Load Distribution (Bar Gauge)
```

---

### NetScaler ADC

**PRTG with Lookups**:
```
1. Download lookups: .../prtg/lookups/download
2. Install .ovl in PRTG
3. Create filtered sensors:
   - Load Balancing: ?filter=metric_view:load_balancing
   - SSL: ?filter=metric_view:ssl
   - System: ?filter=metric_view:system
```

**Important channels**:
- `netscaler_vserver_state`: vServer state (UP/DOWN)
- `netscaler_vserver_hits`: Number of hits
- `netscaler_ssl_cert_days_to_expire`: Certificate expiration
- `netscaler_cpu_usage`: Appliance CPU

**Critical alerts**:
- vServer DOWN: Immediate
- SSL Certificate < 30 days: Warning
- SSL Certificate < 7 days: Critical

---

## Best Practices

### Performance

```mermaid
graph TD
    PERF[Performance Best Practices] --> P1[Adapted Intervals]
    PERF --> P2[Probe Filtering]
    PERF --> P3[Cache Retention]
    PERF --> P4[Dedicated Endpoints]

    P1 --> P1A[CPU/Memory: 30-60s]
    P1 --> P1B[Redfish: 300s]
    P1 --> P1C[Citrix: 120s]

    P2 --> P2A[PRTG: /metrics/probe]
    P2 --> P2B[NetScaler: ?filter=]

    style PERF fill:#81d4fa
```

**Recommendations**:

1. **Scanning Intervals**
   ```
   Real-time metrics (CPU, Memory): 30-60s
   Hardware (Redfish): 300s (5min)
   VDI/Apps (Citrix): 120s (2min)
   Network (NetScaler): 120s (2min)
   ```

2. **PRTG Filtering**
   - ✅ Use `/prtg/metrics/{probe}` instead of `/prtg/metrics`
   - ✅ Use tag filters for NetScaler
   - ❌ Avoid global "all-in-one" sensor (too many channels)

3. **Agent Cache**
   ```yaml
   cache:
     retention_minutes: 10  # Balance freshness/memory
   ```

4. **PRTG/Nagios Timeout**
   ```
   PRTG: 60 seconds
   Nagios: 10 seconds (warning: 5s, critical: 10s)
   ```

---

### Security

**✅ Secure Configuration**:

```yaml
# HTTPS mandatory in production
storage:
  - name: http
    params:
      port: 8443
      bind_address: "192.168.1.100"
      tls:
        enabled: true
        min_tls_version: "1.2"
```

**Firewall**:
```bash
# Allow only monitoring servers
sudo ufw allow from 192.168.1.50 to any port 8443 comment "PRTG Server"
sudo ufw allow from 192.168.1.51 to any port 8443 comment "Nagios Server"
```

**Authentication Key**:
- ✅ Complex UUID: `f47ac10b-58cc-4372-a567-0e02b2c3d479`
- ❌ Simple key: `test`, `admin`, `monitoring`

---

### Alerting

**Recommended thresholds**:

| Metric | Warning | Critical |
|--------|---------|----------|
| CPU Usage | 80% | 95% |
| Memory Usage | 80% | 95% |
| Disk Free | 20% | 10% |
| Temperature | 75°C | 85°C |
| Fan Speed | < 30% | 0% (stopped) |

**Escalation**:
```
1. Warning → Email ops team
2. Critical → Email + SMS on-call
3. Critical > 15min → Phone call
```

---

### Documentation

**Naming Conventions**:

```
PRTG Sensors:
- Format: "SenHub - {Probe Type} - {Server}"
- Examples:
  - "SenHub - CPU - PROD-SERVER-01"
  - "SenHub - Redfish Hardware - PROD-SERVER-01"
  - "SenHub - NetScaler LB - NETSCALER-PROD"

Nagios Services:
- Format: "SenHub {Probe Type}"
- Examples:
  - "SenHub CPU"
  - "SenHub Memory"
  - "SenHub Redfish"
```

**PRTG Tags**:
```
senhub, system, cpu
senhub, system, memory
senhub, hardware, redfish
senhub, vdi, citrix
senhub, network, netscaler
```

---

## Troubleshooting

### PRTG Sensor Status "Down"

**Symptoms**:
```
Sensor: Down (0%)
Last Message: No Response (HTTP 503)
```

**Diagnosis**:
```bash
# 1. Check agent accessible
curl -k https://monitoring.company.com:8443/api/{key}/info/system

# 2. Test exact endpoint
curl -k https://monitoring.company.com:8443/api/{key}/prtg/metrics/cpu

# 3. Check timeout
time curl -k https://monitoring.company.com:8443/api/{key}/prtg/metrics/cpu
# If > 60s → Increase PRTG timeout
```

**Solutions**:
1. Agent down → Restart agent
2. Timeout → Increase sensor timeout (60s → 120s)
3. Probe in error → See agent logs
4. Wrong URL → Check {key} and probe name

---

### Nagios Check CRITICAL

**Symptoms**:
```
CRITICAL - Socket timeout after 10 seconds
```

**Solutions**:
```bash
# 1. Increase check timeout
check_http ... -w 15 -c 30  # Instead of -w 5 -c 10

# 2. Check probe interval vs check interval
# If probe interval = 300s and check interval = 60s
# → Metrics not yet available at check time
```

---

### Grafana "No Data"

**Symptoms**:
```
Panel: No data
```

**Solutions**:
1. **Check datasource**
   ```
   Grafana → Data Sources → Test (should return 200 OK)
   ```

2. **Check query**
   ```json
   {
     "url": "/api/{key}/metrics?probe=cpu",
     "jsonPath": "$.metrics[?(@.name=='cpu_usage_total')].value"
   }
   ```

3. **Check timestamp format**
   - Agent returns ISO 8601: `2025-01-15T10:30:45Z`
   - Grafana expects timestamp in ms or ISO 8601

---

### PRTG Lookups Not Applied

**Symptoms**:
- NetScaler sensors display "0", "1", "2" instead of "Rate", "Counter", "Gauge"

**Solutions**:
```powershell
# 1. Check .ovl files present
dir "C:\Program Files (x86)\PRTG Network Monitor\lookups\custom\netscaler*.ovl"

# 2. Reload lookups
PRTG → Setup → Administrative Tools → Load Lookups and File Lists

# 3. Recreate sensor (not just refresh)
# Delete sensor → Recreate with same URL
```

---

**Support**:
- **Email**: support@senhub.io
- **Documentation**: [Installation](./INSTALLATION.md), [Configuration](./AGENT-CONFIGURATION.md), [Troubleshooting](./TROUBLESHOOTING.md)
