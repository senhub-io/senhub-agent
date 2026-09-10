<img src="https://api.iconify.design/mdi/wifi.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

**License**: Free

# WiFi Signal Strength Probe

## Overview

The WiFi Signal Strength probe monitors wireless network connectivity quality by measuring signal strength and link quality. It automatically detects WiFi connections and collects metrics about signal level, SSID, and access point information.

## Quick Start

```yaml
# probes.d/10-wifi_signal_strength.yaml — each file under probes.d/ is a YAML array of probes
- name: wifi_signal_strength
  type: wifi_signal_strength
```

That is the whole configuration. The probe reads no parameters and collects
every 60 seconds; the cadence is fixed in the code and a `params` block,
`interval` included, is ignored.

## Key Metrics

| Metric | Description | Unit | Platform |
|--------|-------------|------|----------|
| `wifi_signal_strength` | Signal level | dBm | Windows, Linux |
| `wifi_quality` | Link quality | % | Linux only |

On Linux the signal level is the one `iwconfig` reports. On Windows `netsh`
reports a percentage, which the probe converts to dBm with the usual
approximation (dBm = percentage / 2 - 100), so 100% reads as -50 dBm and 60%
as -70 dBm. `wifi_quality` is emitted only when `iwconfig` prints the link
quality on the same line as the signal level, which is its normal layout.

## Platform Support

- **Windows** - Uses `netsh wlan show interfaces` (signal percentage, converted to dBm)
- **Linux** - Uses `iwconfig` (signal level in dBm, link quality in %)
- **macOS** - Not supported
- **BSD** - Not supported

**Auto-enable**: Probe only starts if WiFi is actively connected.

## Configuration Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

This probe reads no parameters.

<!-- schema:params:end -->

The probe collects every 60 seconds. There is no way to change that cadence,
and no other setting: it detects the WiFi connection by itself and starts
only when one is up.

## Signal Strength Interpretation

The metric is in dBm on both platforms, so one scale applies everywhere.

| Range | Quality | Description |
|-------|---------|-------------|
| -30 to -50 dBm | Excellent | Maximum performance |
| -51 to -60 dBm | Good | Reliable connection |
| -61 to -70 dBm | Fair | Minor issues |
| -71 to -80 dBm | Poor | Slow speeds |
| -81 to -90 dBm | Very Poor | Barely connected |
| < -90 dBm | Unusable | No connection |

On Windows the value is derived from the `netsh` percentage, so it never goes
above -50 dBm: a full-strength connection reads -50 dBm, and the "Poor" band
starts where `netsh` shows about 60%.

## Tags

Each metric includes these tags:

| Tag | Description | Example |
|-----|-------------|---------|
| `ssid` | WiFi network name | "CompanyWiFi" |
| `bssid` | Access point MAC address | "00:11:22:33:44:55" |

## Monitoring Integration

### PRTG

```yaml
# strategies.d/00-http.yaml
http:
  endpoints: ["prtg"]
```

Access: `http://localhost:8080/api/{key}/prtg/metrics`

**PRTG Channels**:
- WiFi Signal Strength (dBm)
- WiFi Quality (%) - Linux only

### Nagios

```yaml
# strategies.d/00-http.yaml
http:
  endpoints: ["nagios"]
```

The signal level is reported in dBm, the link quality in percent (Linux only).


## Use Cases

### 1. WiFi Performance Monitoring

**Objective**: Track WiFi connection quality over time

**Metrics**: `wifi_signal_strength`, `wifi_quality`

**Alert**: Signal below -70 dBm

**Benefits**:
- Identify coverage dead zones
- Detect access point issues
- Monitor roaming behavior
- Track interference patterns

### 2. Remote Worker Connectivity

**Objective**: Monitor home office WiFi quality

**Configuration**: the quick start above; the 60-second cadence cannot be
tightened.

**Alert Rules**:
- Signal below -75 dBm: warning (ask the user to move closer)
- Signal below -85 dBm: critical (connectivity issues)

### 3. Mobile Device Monitoring

**Objective**: Track WiFi stability on laptops/tablets

**Metrics**: Signal strength + packet loss (Gateway probe)

**Combined Setup**:
```yaml
# probes.d/00-host.yaml
- name: wifi_signal_strength
  type: wifi_signal_strength
- name: ping_gateway
  type: ping_gateway
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

Without `iwconfig`, the probe can still detect that WiFi is up through
`nmcli`, but it cannot read the signal level: install `wireless-tools` for
the metrics.

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

| Metric | Warning | Critical | Action |
|--------|---------|----------|--------|
| Signal Strength | <-70 dBm | <-80 dBm | Move closer to AP |
| Link Quality (Linux) | <60% | <40% | Check interference |

## Performance

- **CPU**: <0.1% (command execution overhead)
- **Memory**: ~5MB
- **Network**: None (local command execution)
- **Collection Time**: ~100-500ms per collection, every 60 seconds

## Best Practices

### Combined Monitoring

For complete connectivity monitoring, combine with:

1. **Gateway Ping** - Network latency and packet loss
2. **Network Probe** - Interface traffic and errors
3. **WebApp Ping** - Internet connectivity

**Example Configuration**:
```yaml
# probes.d/00-host.yaml
- name: wifi_signal_strength
  type: wifi_signal_strength
- name: ping_gateway
  type: ping_gateway
- name: ping_webapp
  type: ping_webapp
  params:
    url: "https://www.google.com"
```

### Dashboard Design

**Essential Panels**:
1. **Signal Strength Gauge** - Current signal level
2. **Signal Timeline** - Trend over time
3. **SSID Table** - Connected networks
4. **Quality vs Strength** - Correlation chart (Linux)
