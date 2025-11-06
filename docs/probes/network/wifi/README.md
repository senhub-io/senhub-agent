# WiFi Signal Strength Probe - README

## Overview

The WiFi Signal Strength probe monitors wireless network connectivity quality by measuring signal strength and link quality. It automatically detects WiFi connections and collects metrics about signal level, SSID, and access point information.

## Quick Start

### Basic Configuration
```yaml
probes:
  - name: wifi_signal_strength
    params:
      interval: 60
```

### Minimal Configuration
```yaml
probes:
  - name: wifi_signal_strength
```

## Key Metrics

| Metric | Description | Unit | Platform |
|--------|-------------|------|----------|
| `wifi_signal_strength` | Signal strength level | % (Windows), dBm (Linux) | Windows, Linux |
| `wifi_quality` | Link quality percentage | % | Linux only |

## Platform Support

- ✅ **Windows** - Uses `netsh wlan show interfaces` (Signal strength %)
- ✅ **Linux** - Uses `iwconfig` (Signal strength dBm, Link quality %)
- ❌ **macOS** - Not supported
- ❌ **BSD** - Not supported

**Auto-enable**: Probe only starts if WiFi is actively connected.

## Configuration Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `interval` | integer | 60 | Collection interval in seconds |

**Note**: No additional configuration required. Probe auto-detects WiFi connection.

## Signal Strength Interpretation

### Windows (Percentage)

| Range | Quality | Description |
|-------|---------|-------------|
| 90-100% | Excellent | Maximum performance |
| 75-89% | Good | Reliable connection |
| 60-74% | Fair | Minor connectivity issues |
| 40-59% | Poor | Frequent disconnections |
| <40% | Very Poor | Unusable connection |

### Linux (dBm)

| Range | Quality | Description |
|-------|---------|-------------|
| -30 to -50 dBm | Excellent | Maximum performance |
| -51 to -60 dBm | Good | Reliable connection |
| -61 to -70 dBm | Fair | Minor issues |
| -71 to -80 dBm | Poor | Slow speeds |
| -81 to -90 dBm | Very Poor | Barely connected |
| < -90 dBm | Unusable | No connection |

## Tags

Each metric includes these tags:

| Tag | Description | Example |
|-----|-------------|---------|
| `ssid` | WiFi network name | "CompanyWiFi" |
| `bssid` | Access point MAC address | "00:11:22:33:44:55" |

## Monitoring Integration

### PRTG

```yaml
storage:
  - name: http
    params:
      endpoints: ["prtg"]
```

Access: `http://localhost:8080/api/{key}/prtg/metrics`

**PRTG Channels**:
- WiFi Signal Strength (%, dBm)
- WiFi Quality (%) - Linux only

### Nagios

```yaml
storage:
  - name: http
    params:
      endpoints: ["nagios"]
```

Returns: `OK - WiFi connected | signal=85% quality=90%`

### Grafana

Query: `wifi_signal_strength{ssid="CompanyWiFi"}`

**Dashboard Panels**:
- Signal strength timeline
- Signal heatmap by SSID
- Connection quality gauge

## Use Cases

### 1. WiFi Performance Monitoring

**Objective**: Track WiFi connection quality over time

**Metrics**: `wifi_signal_strength`, `wifi_quality`

**Alert**: Signal <60% (Windows) or <-70dBm (Linux)

**Benefits**:
- Identify coverage dead zones
- Detect access point issues
- Monitor roaming behavior
- Track interference patterns

### 2. Remote Worker Connectivity

**Objective**: Monitor home office WiFi quality

**Configuration**:
```yaml
probes:
  - name: wifi_signal_strength
    params:
      interval: 30  # More frequent checks
```

**Alert Rules**:
- Signal <50% → Warning (ask user to move closer)
- Signal <30% → Critical (connectivity issues)

### 3. Mobile Device Monitoring

**Objective**: Track WiFi stability on laptops/tablets

**Metrics**: Signal strength + packet loss (Gateway probe)

**Combined Setup**:
```yaml
probes:
  - name: wifi_signal_strength
    params:
      interval: 60
  - name: ping_gateway
    params:
      interval: 30
```

### 4. Access Point Performance

**Objective**: Compare signal strength across multiple access points

**Metrics**: Signal strength by BSSID tag

**Analysis**:
- Group by BSSID to compare APs
- Identify weak access points
- Optimize AP placement

## Troubleshooting

### Probe Not Starting

**Symptom**: Probe doesn't collect metrics

**Cause**: No active WiFi connection detected

**Diagnosis**:
```bash
# Windows
netsh wlan show interfaces

# Linux
iwconfig
```

**Solution**: Connect to WiFi network first

### Windows: "Error checking WiFi connection"

**Symptom**: Probe fails on Windows

**Possible Causes**:
1. WiFi adapter disabled
2. No WiFi hardware
3. Permission issues

**Solutions**:
```powershell
# Enable WiFi adapter
netsh interface set interface "WiFi" enabled

# Check adapter status
netsh wlan show interfaces
```

### Linux: "Command not found: iwconfig"

**Symptom**: Probe fails on Linux

**Cause**: `wireless-tools` package not installed

**Solution**:
```bash
# Debian/Ubuntu
sudo apt-get install wireless-tools

# RHEL/CentOS
sudo yum install wireless-tools

# Arch Linux
sudo pacman -S wireless_tools
```

### Linux: Alternative with nmcli

If `iwconfig` is not available, probe falls back to `nmcli`:

```bash
# Check WiFi status
nmcli radio wifi

# Enable WiFi
nmcli radio wifi on
```

### No Signal Strength Data

**Symptom**: Probe runs but returns no metrics

**Windows**:
```powershell
# Check netsh output manually
netsh wlan show interfaces
# Look for "Signal" line
```

**Linux**:
```bash
# Check iwconfig output
iwconfig
# Look for "Signal level=" line
```

### Unstable Signal Readings

**Symptom**: Signal fluctuates rapidly

**Possible Causes**:
1. WiFi interference (microwaves, Bluetooth)
2. Moving laptop/device
3. Multiple access points with same SSID
4. Distance from access point

**Solutions**:
- Move closer to access point
- Switch to 5GHz band (less interference)
- Use wired connection for stability
- Check for interfering devices

## Alert Thresholds

### Windows (Percentage)

| Metric | Warning | Critical | Action |
|--------|---------|----------|--------|
| Signal Strength | <60% | <40% | Move closer to AP |

### Linux (dBm)

| Metric | Warning | Critical | Action |
|--------|---------|----------|--------|
| Signal Strength | <-70 dBm | <-80 dBm | Move closer to AP |
| Link Quality | <60% | <40% | Check interference |

## Performance

- **CPU**: <0.1% (command execution overhead)
- **Memory**: ~5MB
- **Network**: None (local command execution)
- **Collection Time**: ~100-500ms per interval

## Best Practices

### Collection Interval

| Interval | Use Case | Impact |
|----------|----------|--------|
| 30s | Real-time monitoring | Higher CPU usage |
| 60s | Standard monitoring (recommended) | Balanced |
| 300s | Periodic checks | Minimal impact |

### Combined Monitoring

For complete connectivity monitoring, combine with:

1. **Gateway Ping** - Network latency and packet loss
2. **Network Probe** - Interface traffic and errors
3. **WebApp Ping** - Internet connectivity

**Example Configuration**:
```yaml
probes:
  - name: wifi_signal_strength
    params:
      interval: 60
  - name: ping_gateway
    params:
      interval: 30
  - name: ping_webapp
    params:
      interval: 60
      url: "https://www.google.com"
```

### Dashboard Design

**Essential Panels**:
1. **Signal Strength Gauge** - Current signal level
2. **Signal Timeline** - Trend over time
3. **SSID Table** - Connected networks
4. **Quality vs Strength** - Correlation chart (Linux)

## Alert Examples

### Basic Signal Alert

```yaml
alerts:
  - name: Weak WiFi Signal
    condition: wifi_signal_strength < 60  # Windows
    duration: 5m
    severity: warning
    action: notify_user
```

### Linux Signal Alert

```yaml
alerts:
  - name: Weak WiFi Signal
    condition: wifi_signal_strength < -70  # Linux dBm
    duration: 5m
    severity: warning
    action: notify_user
```

### Quality Alert (Linux Only)

```yaml
alerts:
  - name: Poor WiFi Quality
    condition: wifi_quality < 50
    duration: 3m
    severity: warning
    action: check_interference
```

### SSID-Specific Alert

```yaml
alerts:
  - name: Office WiFi Weak
    condition: wifi_signal_strength < 60 AND ssid == "OfficeWiFi"
    severity: warning
    action: notify_admin
```

## Related Documentation

- [METRICS.md](./METRICS.md) - Complete metrics reference
- [Gateway Ping Probe](../README.md) - Network latency monitoring
- [Network Probe](../../system/network/) - Interface metrics
- [WebApp Ping](../../webapp/PING-README.md) - Internet connectivity
