# WiFi Signal Strength Probe - Metrics Reference

## Introduction

The WiFi Signal Strength probe collects wireless network quality metrics using platform-native commands. It provides signal strength measurements and link quality data to monitor WiFi connectivity health and performance.

## Collected Metrics

### Overview

| Metric Name | Display Name | Type | Unit | Platform | Description |
|-------------|--------------|------|------|----------|-------------|
| `wifi_signal_strength` | WiFi Signal Strength | gauge | % / dBm | Windows / Linux | Signal strength level |
| `wifi_quality` | WiFi Link Quality | gauge | % | Linux only | Link quality percentage |

## Detailed Metric Descriptions

### 1. wifi_signal_strength

**Technical Name**: `wifi_signal_strength`  
**Display Name**: WiFi Signal Strength  
**Type**: Gauge  
**Unit**: Percentage (%) on Windows, dBm on Linux  
**Range**: 
- Windows: 0-100%
- Linux: -90 to -30 dBm

**Description**:
Wireless signal strength indicates the power level of the radio signal between the WiFi adapter and the access point. Higher values indicate better signal quality.

**Platform Implementation**:

**Windows** (Percentage):
```powershell
# Command executed
netsh wlan show interfaces

# Output parsing (example)
    Signal                 : 85%
```

**Regex**: `signal.*?(\d+)%`  
**Value**: Integer percentage (0-100)

**Linux** (dBm):
```bash
# Command executed
iwconfig

# Output parsing (example)
wlan0     IEEE 802.11  ESSID:"CompanyWiFi"
          Link Quality=60/70  Signal level=-50 dBm
```

**Regex**: `Signal level=(-?\d+) dBm`  
**Value**: Negative integer (-90 to -30)

**Signal Strength Ranges**:

**Windows (Percentage)**:
- **90-100%** (Excellent): Maximum performance, stable connection
- **75-89%** (Good): Reliable for all activities
- **60-74%** (Fair): Minor issues, acceptable for most use
- **40-59%** (Poor): Frequent buffering, slow speeds
- **0-39%** (Very Poor): Unusable, frequent disconnections

**Linux (dBm)**:
- **-30 to -50 dBm** (Excellent): Maximum throughput
- **-51 to -60 dBm** (Good): Very reliable, good speeds
- **-61 to -70 dBm** (Fair): Reliable, slower speeds
- **-71 to -80 dBm** (Poor): Minimum connection, slow
- **-81 to -90 dBm** (Very Poor): Barely usable
- **< -90 dBm** (Unusable): Cannot maintain connection

**Tags**:
- `probe`: "wifi_signal_strength"
- `ssid`: Network SSID (e.g., "CompanyWiFi")
- `bssid`: Access point MAC address (e.g., "00:11:22:33:44:55")
- `hostname`: System hostname
- `os`: Operating system

**Example (Windows)**:
```json
{
  "name": "wifi_signal_strength",
  "value": 85.0,
  "unit": "%",
  "tags": {
    "probe": "wifi_signal_strength",
    "hostname": "laptop-01",
    "ssid": "OfficeWiFi",
    "bssid": "00:11:22:33:44:55",
    "os": "windows"
  }
}
```

**Example (Linux)**:
```json
{
  "name": "wifi_signal_strength",
  "value": -55.0,
  "unit": "dBm",
  "tags": {
    "probe": "wifi_signal_strength",
    "hostname": "laptop-02",
    "ssid": "HomeWiFi",
    "bssid": "AA:BB:CC:DD:EE:FF",
    "os": "linux"
  }
}
```

**Use Cases**:
- Monitor WiFi coverage quality
- Identify weak signal areas (dead zones)
- Track roaming behavior between access points
- Detect access point hardware issues
- Optimize access point placement

**Troubleshooting**:
- **Windows: 0% or no data**: WiFi disconnected, check `netsh wlan show interfaces`
- **Linux: -90 dBm or lower**: Out of range, move closer to access point
- **Fluctuating values**: Interference (microwaves, Bluetooth), walls/obstacles
- **Sudden drops**: Access point overload, channel congestion

### 2. wifi_quality (Linux Only)

**Technical Name**: `wifi_quality`  
**Display Name**: WiFi Link Quality  
**Type**: Gauge  
**Unit**: Percentage (%)  
**Range**: 0-100%

**Description**:
Link quality represents the overall quality of the wireless link, combining signal strength, noise level, and error rates. This metric is unique to Linux and provides a more comprehensive view of connection health than signal strength alone.

**Platform Implementation**:

**Linux Only**:
```bash
# Command executed
iwconfig

# Output parsing (example)
wlan0     IEEE 802.11  ESSID:"CompanyWiFi"
          Link Quality=60/70  Signal level=-50 dBm
```

**Calculation**:
```
quality_percent = (current_quality / max_quality) * 100
                = (60 / 70) * 100
                = 85.71%
```

**Regex**: `Link Quality=(\d+)/(\d+)`  
**Processing**: Convert fraction to percentage

**Quality Ranges**:
- **90-100%** (Excellent): Optimal link quality
- **75-89%** (Good): Reliable connection
- **60-74%** (Fair): Minor issues
- **40-59%** (Poor): Significant problems
- **0-39%** (Very Poor): Barely functional

**Tags**:
- `probe`: "wifi_signal_strength"
- `ssid`: Network SSID
- `bssid`: Access point MAC address
- `hostname`: System hostname
- `os`: "linux"

**Example**:
```json
{
  "name": "wifi_quality",
  "value": 85.71,
  "unit": "%",
  "tags": {
    "probe": "wifi_signal_strength",
    "hostname": "server-linux",
    "ssid": "DataCenter-WiFi",
    "bssid": "11:22:33:44:55:66",
    "os": "linux"
  }
}
```

**Use Cases**:
- Detect RF interference
- Identify noisy WiFi environments
- Monitor connection stability
- Compare link quality across access points
- Troubleshoot performance issues beyond signal strength

**Troubleshooting**:
- **Low quality despite good signal**: RF interference, check for microwaves, Bluetooth devices
- **Quality <50%**: Channel congestion, consider changing WiFi channel
- **Quality drops periodically**: Intermittent interference source
- **Quality 0%**: WiFi disconnected or `iwconfig` parse error

## Metric Tags

### Standard Tags

All metrics include these tags by default:

| Tag | Example | Description |
|-----|---------|-------------|
| `probe` | `wifi_signal_strength` | Probe name identifier |
| `hostname` | `laptop-01` | System hostname |
| `os` | `windows` | Operating system |
| `ssid` | `CompanyWiFi` | WiFi network name |
| `bssid` | `00:11:22:33:44:55` | Access point MAC address |

### SSID Tag

**Purpose**: Identify the WiFi network being monitored

**Extraction**:
- **Windows**: From `netsh wlan show interfaces` → `SSID` line
- **Linux**: From `iwconfig` → `ESSID:"..."` field

**Use Cases**:
- Filter metrics by specific WiFi network
- Compare performance across networks (home, office, guest)
- Alert on specific SSID connections

**Example Queries**:
```
wifi_signal_strength{ssid="OfficeWiFi"}
wifi_quality{ssid="HomeWiFi"}
```

### BSSID Tag

**Purpose**: Identify the specific access point (AP)

**Extraction**:
- **Windows**: From `netsh wlan show interfaces` → `BSSID` line
- **Linux**: From `iwconfig` → `Access Point:` field

**Format**: MAC address (XX:XX:XX:XX:XX:XX)

**Use Cases**:
- Track roaming between access points
- Identify weak access points
- Compare signal strength per AP
- Detect AP hardware issues

**Example Queries**:
```
wifi_signal_strength{bssid="00:11:22:33:44:55"}
avg(wifi_signal_strength) by (bssid)
```

## Calculation Details

### Windows Signal Extraction

**Command Output Example**:
```
Name                   : WiFi
Description            : Intel(R) Wi-Fi 6 AX200 160MHz
GUID                   : {12345678-1234-1234-1234-123456789012}
Physical address       : aa:bb:cc:dd:ee:ff
State                  : connected
SSID                   : CompanyWiFi
BSSID                  : 00:11:22:33:44:55
Network type           : Infrastructure
Radio type             : 802.11ac
Authentication         : WPA2-Personal
Cipher                 : CCMP
Connection mode        : Auto Connect
Channel                : 36
Receive rate (Mbps)    : 866
Transmit rate (Mbps)   : 866
Signal                 : 85%
```

**Parsing Logic**:
```go
lines := strings.Split(string(output), "\n")
for _, line := range lines {
    line = strings.TrimSpace(line)
    if strings.Contains(strings.ToLower(line), "signal") {
        parts := strings.Fields(line)
        signalStrengthStr := strings.TrimSuffix(parts[len(parts)-1], "%")
        signalStrength, _ := strconv.Atoi(signalStrengthStr)
    }
}
```

**Edge Cases**:
- French Windows: "Signal" may be "Signal" (same)
- No WiFi: "State : disconnected" → probe doesn't start
- Multiple adapters: Uses first connected WiFi interface

### Linux Signal Extraction

**Command Output Example**:
```
wlan0     IEEE 802.11  ESSID:"CompanyWiFi"  
          Mode:Managed  Frequency:5.18 GHz  Access Point: 00:11:22:33:44:55   
          Bit Rate=866 Mb/s   Tx-Power=20 dBm   
          Retry short limit:7   RTS thr:off   Fragment thr:off
          Power Management:off
          Link Quality=60/70  Signal level=-50 dBm  
          Rx invalid nwid:0  Rx invalid crypt:0  Rx invalid frag:0
          Tx excessive retries:0  Invalid misc:0   Missed beacon:0
```

**Parsing Logic**:
```go
for _, line := range lines {
    if strings.Contains(line, "Signal level=") {
        signalMatch := strings.Split(strings.Split(line, "Signal level=")[1], " ")[0]
        signalStrength, _ := strconv.Atoi(strings.TrimSpace(signalMatch))
    }
    
    if strings.Contains(line, "Link Quality=") {
        qualityStr := strings.Split(strings.Split(line, "Link Quality=")[1], " ")[0]
        qualityParts := strings.Split(qualityStr, "/")
        quality := qualityParts[0]
        maxQuality := qualityParts[1]
        qualityPercent = (float32(quality) / float32(maxQuality)) * 100
    }
}
```

**Edge Cases**:
- No WiFi: "Access Point: Not-Associated" → probe doesn't start
- Multiple interfaces: Processes first WLAN interface (wlan0, wlp2s0)
- No `iwconfig`: Falls back to `nmcli radio wifi` check

### WiFi Connection Detection

**Windows**:
```go
cmd := exec.Command("netsh", "wlan", "show", "interfaces")
output, err := cmd.Output()
lines := strings.Split(string(output), "\n")
for _, line := range lines {
    if strings.Contains(strings.ToLower(line), "state") {
        if strings.Contains(line, "connect") && 
           !strings.Contains(line, "dis") {
            return true // WiFi connected
        }
    }
}
```

**Linux**:
```go
// Method 1: iwconfig
cmd := exec.Command("iwconfig")
output, err := cmd.Output()
if strings.Contains(string(output), "ESSID:") &&
   !strings.Contains(string(output), "ESSID:off/any") {
    return true
}

// Method 2: nmcli fallback
cmd = exec.Command("nmcli", "-t", "-f", "WIFI", "radio")
output, err = cmd.Output()
return strings.Contains(strings.ToLower(string(output)), "enabled")
```

## Use Cases by Metric

### Network Troubleshooting

**Metrics**: `wifi_signal_strength`, `wifi_quality`  
**Alert**: Signal <60% (Windows) or <-70dBm (Linux)

Diagnose poor network performance by correlating signal strength with application issues.

### Access Point Optimization

**Metrics**: `wifi_signal_strength` by `bssid`  
**Analysis**: Compare signal strength across access points

Identify weak access points and optimize placement:
```
avg(wifi_signal_strength) by (bssid, ssid)
```

### Remote Worker Support

**Metrics**: `wifi_signal_strength`, `wifi_quality`  
**Alert**: Sustained low signal (<50%) for >5 minutes

Proactively notify users to improve WiFi connectivity.

### Coverage Mapping

**Metrics**: `wifi_signal_strength` by location/hostname  
**Visualization**: Heatmap of signal strength

Identify WiFi dead zones and coverage gaps across office spaces.

### Roaming Analysis

**Metrics**: `wifi_signal_strength` by `bssid` over time  
**Visualization**: Timeline showing BSSID changes

Monitor device roaming between access points, detect sticky client issues.

## Monitoring Best Practices

### Alert Configuration

**Basic Alerts**:
```yaml
alerts:
  - name: Weak WiFi Signal (Windows)
    condition: wifi_signal_strength < 60
    duration: 5m
    severity: warning
    
  - name: Critical WiFi Signal (Windows)
    condition: wifi_signal_strength < 40
    duration: 2m
    severity: critical
    
  - name: Weak WiFi Signal (Linux)
    condition: wifi_signal_strength < -70
    duration: 5m
    severity: warning
    
  - name: Poor WiFi Quality (Linux)
    condition: wifi_quality < 50
    duration: 3m
    severity: warning
```

**Advanced Alerts**:
```yaml
alerts:
  - name: Frequent AP Roaming
    condition: count(bssid changes) > 5 in 10 minutes
    severity: info
    description: "Device roaming frequently between APs"
    
  - name: Degrading Signal Trend
    condition: derivative(wifi_signal_strength) < -5 per minute
    duration: 3m
    severity: warning
    description: "Signal strength degrading rapidly"
```

### Dashboard Panels

**Essential Panels**:
1. **Current Signal Gauge** - Real-time signal strength
2. **Signal Timeline** - Historical signal strength over time
3. **SSID Distribution** - Pie chart of connected networks
4. **Signal Heatmap** - Signal strength by time and location
5. **AP Comparison** - Table of signal strength by BSSID

**Linux-Specific Panels**:
6. **Quality vs Signal** - Scatter plot correlating quality and signal
7. **Quality Timeline** - Historical link quality

### Collection Intervals

| Interval | Use Case | Network Impact | CPU Impact |
|----------|----------|----------------|------------|
| 30s | Real-time monitoring | None (local) | <0.2% |
| 60s | Standard monitoring (recommended) | None (local) | <0.1% |
| 300s | Periodic health checks | None (local) | <0.05% |

**Recommendation**: 60 seconds balances responsiveness with resource usage.

## Advanced Analysis

### Signal Quality Score

Calculate overall WiFi quality score combining signal and quality:

**Windows**:
```
health_score = wifi_signal_strength
```

**Linux**:
```
health_score = (normalized_signal * 0.6) + (wifi_quality * 0.4)

Where:
normalized_signal = ((signal_dBm + 90) / 60) * 100
                  = ((-50 + 90) / 60) * 100
                  = 66.67%

health_score = (66.67 * 0.6) + (85.71 * 0.4)
             = 40.0 + 34.3
             = 74.3 (Good)
```

**Scoring**:
- **90-100**: Excellent WiFi
- **75-89**: Good WiFi
- **60-74**: Fair WiFi
- **40-59**: Poor WiFi
- **<40**: Very Poor WiFi

### Roaming Detection

Detect access point changes by monitoring BSSID tag changes:

```
changes(wifi_signal_strength{bssid}) over 10 minutes
```

**Healthy Roaming**: 0-2 changes per 10 minutes  
**Sticky Client**: 0 changes despite moving  
**Unstable Roaming**: >5 changes per 10 minutes

## Troubleshooting

### Windows: Probe Not Starting

**Symptom**: No metrics collected on Windows system

**Diagnosis**:
```powershell
# Check WiFi status
netsh wlan show interfaces

# Expected output if connected
State                  : connected
```

**Possible Causes**:
1. WiFi not connected
2. WiFi adapter disabled
3. No WiFi hardware

**Solutions**:
```powershell
# Enable WiFi adapter
netsh interface set interface "WiFi" admin=enabled

# Connect to network
netsh wlan connect name="NetworkName"
```

### Linux: Command Not Found

**Symptom**: `iwconfig: command not found`

**Solution**:
```bash
# Debian/Ubuntu
sudo apt-get install wireless-tools

# RHEL/CentOS
sudo yum install wireless-tools

# Arch Linux
sudo pacman -S wireless_tools
```

### No SSID/BSSID Tags

**Symptom**: Metrics collected but SSID/BSSID tags missing

**Windows Cause**: Encoding issues in `netsh` output

**Linux Cause**: Unexpected `iwconfig` output format

**Diagnosis**:
```bash
# Windows
netsh wlan show interfaces | findstr /i "ssid bssid"

# Linux
iwconfig | grep -i "essid\|access point"
```

**Solution**: Check output format, may need custom parsing

### Unstable Signal Readings

**Symptom**: Signal fluctuates dramatically (±20% / ±10dBm)

**Possible Causes**:
1. **Physical Movement**: Laptop/device moving
2. **Interference**: Microwave ovens, Bluetooth devices
3. **Channel Congestion**: Too many devices on same channel
4. **Weak Hardware**: Failing WiFi adapter or access point

**Diagnosis**:
```bash
# Check for interference (Linux)
sudo iwlist wlan0 scan | grep -i "frequency\|quality"

# Check connected devices (access AP admin panel)
```

**Solutions**:
- Move to stationary location
- Switch to 5GHz band (less interference)
- Change WiFi channel on access point
- Replace WiFi adapter

### Permission Errors

**Symptom**: "Permission denied" running commands

**Linux Solution**:
```bash
# Add user to netdev group
sudo usermod -aG netdev $USER

# Or run agent with capabilities
sudo setcap cap_net_admin+ep /path/to/agent
```

## Performance Characteristics

- **CPU Usage**: <0.1% per collection
- **Memory**: ~5MB
- **Network**: None (local command execution)
- **Collection Time**: 100-500ms
- **Latency**: Negligible

## Related Documentation

- [WiFi Signal README](./README.md) - Overview and configuration
- [Gateway Ping Probe](../README.md) - Network latency monitoring
- [Network Probe](../../system/network/) - Interface traffic metrics
- [WebApp Ping](../../webapp/PING-README.md) - Internet connectivity testing
