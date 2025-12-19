# SenHub Agent - Installation Guide

This guide walks you through installing SenHub Agent on your infrastructure. Whether you manage Windows, Linux, or macOS servers, you'll find all the steps to install the agent in minutes and start collecting metrics.

## Table of Contents

- [Overview](#overview)
- [System Requirements](#system-requirements)
- [Download](#download)
- [Windows Installation](#windows-installation)
- [Linux Installation](#linux-installation)
- [macOS Installation](#macos-installation)
- [Getting Started](#getting-started)
- [Uninstallation](#uninstallation)

---

## Overview

SenHub Agent is a lightweight and versatile monitoring agent that collects system and infrastructure metrics. Installation defaults to **offline mode**, meaning the agent runs autonomously without requiring external connections.

### What is Offline Mode?

In offline mode, the agent:
- Runs **autonomously** on your server
- Exposes a **local web interface** to view metrics
- Configures via a **local YAML file**
- Sends **no external data**
- Is perfect for air-gapped environments, edge computing, or development

```mermaid
graph LR
    A[SenHub Agent] -->|Collects| B[Metrics<br/>CPU, Memory, etc.]
    B -->|Storage| C[Local Cache]
    C -->|Exposition| D[Web Interface<br/>:8080/:8443]
    D -->|Access| E[Browser<br/>PRTG/Nagios]

    style A fill:#81d4fa
    style C fill:#fff9c4
    style D fill:#c8e6c9
```

> **💡 Note**: An **online mode** also exists (reserved for connection to the centralized SenHub platform), but this guide focuses on offline installation, the most common mode.

---

## System Requirements

Before starting, ensure your system meets these minimum requirements.

### Supported Operating Systems

SenHub Agent runs on all modern platforms:

| Platform | Supported Versions | Architecture |
|----------|-------------------|--------------|
| **Windows** | Windows Server 2012+ / Windows 10+ | x64 |
| **Linux** | Ubuntu 18.04+, RHEL 7+, CentOS 7+, Debian 10+ | x64, ARM64 |
| **macOS** | macOS 10.13+ (High Sierra and later) | x64, ARM64 (M1/M2) |

### Resource Requirements

Requirements are very modest:

| Resource | Minimum | Recommended |
|----------|---------|-------------|
| **CPU** | 1 core | 2 cores |
| **RAM** | 256 MB | 512 MB |
| **Disk** | 100 MB | 500 MB |

> **Note**: Actual consumption varies based on the number of active probes and collection frequency. In practice, the agent typically uses less than 100 MB of RAM in standard configuration.

### Network Ports

The agent exposes a local HTTP/HTTPS interface to access metrics:

| Port | Protocol | Usage | Required |
|------|----------|-------|----------|
| **8080** | HTTP | Web interface, API (default) | ✅ HTTP mode |
| **8443** | HTTPS | Web interface, secure API | ✅ HTTPS mode |
| **443** | HTTPS | SenHub platform (if online mode) | ❌ Offline mode |
| **514** | UDP/TCP | Syslog reception (if syslog probe enabled) | ⚠️ Optional |

**Outbound flows for online mode** (if used):
- `eu-west-1.intake.senhub.io:443` (HTTPS) - Communication with SenHub platform

> **💡 Tip**: For complete air-gap usage, only port 8080 or 8443 is needed (accessible only from your local network).

### Required Permissions

System service installation requires administrative rights:

- **Windows**: Administrator
- **Linux**: root or sudo
- **macOS**: root or sudo

> **💡 Alternative**: The agent can also be launched manually without elevated privileges if you don't need the system service (useful for console mode testing).

---

## Download

SenHub Agent binaries are available on the official releases server.

### Download URL

```
https://eu-west-1.intake.senhub.io/releases
```

Select the version and architecture matching your system:

**Windows**:
- `senhub-agent_windows_amd64.exe` (x64)

**Linux**:
- `senhub-agent_linux_amd64` (x64)
- `senhub-agent_linux_arm64` (ARM64 - Raspberry Pi, etc.)

**macOS**:
- `senhub-agent_darwin_amd64` (Intel x64)
- `senhub-agent_darwin_arm64` (Apple Silicon M1/M2/M3)

> **📸 SCREENSHOT TO INSERT**: Release page showing list of available versions and binaries

### Integrity Verification (Optional)

To verify the binary hasn't been tampered with:

```bash
# Download checksum
curl -O https://eu-west-1.intake.senhub.io/releases/{version}/checksums.txt

# Verify
sha256sum -c checksums.txt
```

---

## Windows Installation

This section guides you through installing SenHub Agent on Windows Server or Windows 10/11. Installation creates a Windows service that starts automatically with the system.

### Step 1: Download the Binary

Download the Windows binary from:
```
https://eu-west-1.intake.senhub.io/releases/senhub-agent_windows_amd64.exe
```

### Step 2: Prepare the Environment

Create a folder for the agent and move the binary:

```powershell
# Open PowerShell as Administrator
New-Item -ItemType Directory -Force -Path "C:\Program Files\SenHub"
cd "C:\Program Files\SenHub"

# Move the downloaded binary
Move-Item "C:\Users\YOUR_USER\Downloads\senhub-agent_windows_amd64.exe" .
```

### Step 3: Choose Installation Mode

You have a choice between two installation modes based on your security needs.

#### Option A: HTTP Installation (Development/Testing)

**When to use**: Development environment, localhost access only.

```powershell
.\senhub-agent_windows_amd64.exe install --offline
```

**What is configured**:
- Port: `8080`
- Bind: `127.0.0.1` (localhost only)
- Protocol: HTTP (unencrypted)
- Access: `http://localhost:8080/web/{key}/dashboard`

> **🔑 Important Note**: Carefully note the agent key (UUID) displayed during installation, you'll need it to access the web interface.

**📸 SCREENSHOT TO INSERT**: PowerShell showing installation output with agent key highlighted

#### Option B: HTTPS Installation (Production Recommended)

**When to use**: Production environment, access from other network machines.

```powershell
.\senhub-agent_windows_amd64.exe install --offline --enable-https
```

**What is configured**:
- Port: `8443`
- Bind: `0.0.0.0` (accessible from network)
- Protocol: HTTPS (encrypted TLS 1.2+)
- Certificates: Auto-generated (self-signed)
- Access: `https://monitoring.company.local:8443/web/{key}/dashboard`

**Generated certificates**:
```
C:\Program Files\SenHub\certs\
├── agent-cert.pem  (SSL Certificate)
└── agent-key.pem   (Private Key)
```

Installation automatically generates an SSL certificate with SANs for `localhost` and `127.0.0.1`. To add other hostnames:

```powershell
.\senhub-agent_windows_amd64.exe install --offline --enable-https `
  --https-hosts "monitoring.company.local,192.168.1.100"
```

**📸 SCREENSHOT TO INSERT**: Windows Explorer showing `C:\Program Files\SenHub\certs\` folder with certificate files

### Step 4: Start the Service

Once installed, start the service:

```powershell
.\senhub-agent_windows_amd64.exe start
```

Verify it's running:

```powershell
.\senhub-agent_windows_amd64.exe status
```

Or via Windows Services console:

```powershell
Get-Service "SenHub Agent"
```

**📸 SCREENSHOT TO INSERT**: Windows Services showing "SenHub Agent" with "Running" status

### Step 5: Configure Firewall

If using HTTPS and wanting to access the agent from other machines, open the port in the firewall:

```powershell
# Allow port 8443 (HTTPS)
New-NetFirewallRule -DisplayName "SenHub Agent HTTPS" `
  -Direction Inbound -Protocol TCP -LocalPort 8443 -Action Allow

# Or for HTTP (port 8080)
New-NetFirewallRule -DisplayName "SenHub Agent HTTP" `
  -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow
```

### Windows Files and Folders

After installation, the agent creates this structure:

```
C:\Program Files\SenHub\
├── senhub-agent_windows_amd64.exe    # Main binary
├── agent-config.yaml                 # Configuration (generated at install)
└── certs\                            # SSL certificates (if HTTPS)
    ├── agent-cert.pem
    └── agent-key.pem

C:\ProgramData\SenHub\Logs\
└── agent.log                         # Agent logs
```

---

## Linux Installation

Linux installation is straightforward using the standalone binary. This section covers Ubuntu, Debian, RHEL, CentOS, and other modern distributions.

### Step 1: Download the Binary

Download the binary matching your architecture:

```bash
# For x64 (most servers)
wget https://eu-west-1.intake.senhub.io/releases/senhub-agent_linux_amd64

# For ARM64 (Raspberry Pi, ARM servers)
wget https://eu-west-1.intake.senhub.io/releases/senhub-agent_linux_arm64
```

### Step 2: Install the Binary

Make it executable and move to `/usr/local/bin`:

```bash
chmod +x senhub-agent_linux_amd64
sudo mv senhub-agent_linux_amd64 /usr/local/bin/senhub-agent
```

Verify installation:

```bash
senhub-agent version
```

You should see the agent version displayed.

**📸 SCREENSHOT TO INSERT**: Terminal showing `senhub-agent version` output

### Step 3: Choose Installation Mode

Like Windows, you can choose between HTTP (development) and HTTPS (production).

#### Option A: HTTP Installation (Development)

```bash
sudo senhub-agent install --offline
```

Agent will be accessible at `http://localhost:8080`

#### Option B: HTTPS Installation (Production Recommended)

```bash
sudo senhub-agent install --offline --enable-https
```

Agent will be accessible at `https://localhost:8443` (or via server IP from network).

**To specify custom hostnames**:

```bash
sudo senhub-agent install --offline --enable-https \
  --https-hosts "monitoring.company.local,192.168.1.100"
```

This generates an SSL certificate with appropriate SANs to avoid browser security warnings.

### Step 4: Start the Service

Installation automatically creates a systemd service. Enable and start it:

```bash
sudo systemctl enable senhub-agent
sudo systemctl start senhub-agent
```

Check status:

```bash
sudo systemctl status senhub-agent
```

You should see:
```
● senhub-agent.service - SenHub Agent
   Loaded: loaded (/etc/systemd/system/senhub-agent.service; enabled)
   Active: active (running) since ...
```

**📸 SCREENSHOT TO INSERT**: Terminal with `systemctl status senhub-agent` output showing "active (running)" in green

### Step 5: Configure Firewall

Open the necessary port in your firewall.

**UFW (Ubuntu/Debian)**:

```bash
sudo ufw allow 8443/tcp comment 'SenHub Agent HTTPS'
sudo ufw reload
```

**firewalld (RHEL/CentOS/Rocky Linux)**:

```bash
sudo firewall-cmd --permanent --add-port=8443/tcp
sudo firewall-cmd --reload
```

### Systemd Service Configuration

The automatically created service file is in `/etc/systemd/system/senhub-agent.service`:

```ini
[Unit]
Description=SenHub Agent
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/senhub-agent run --offline
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=multi-user.target
```

This service starts automatically at boot and restarts on failure.

### Linux Files and Folders

```
/usr/local/bin/
└── senhub-agent                      # Main binary

/etc/senhub-agent/
└── agent-config.yaml                 # Configuration

/var/lib/senhub-agent/
└── certs/                            # SSL certificates (if HTTPS)
    ├── agent-cert.pem
    └── agent-key.pem

/var/log/senhub-agent/
└── agent.log                         # Logs
```

---

## macOS Installation

macOS installation works similarly to Linux, with creation of a LaunchDaemon to manage the service.

### Step 1: Download the Binary

Download the binary matching your Mac:

```bash
# For Intel Mac (x64)
curl -LO https://eu-west-1.intake.senhub.io/releases/senhub-agent_darwin_amd64

# For Apple Silicon Mac (M1/M2/M3)
curl -LO https://eu-west-1.intake.senhub.io/releases/senhub-agent_darwin_arm64
```

### Step 2: Install the Binary

```bash
# Make executable
chmod +x senhub-agent_darwin_amd64  # or arm64

# Move to /usr/local/bin
sudo mv senhub-agent_darwin_amd64 /usr/local/bin/senhub-agent
```

### Step 3: Allow Execution (macOS Security)

macOS blocks binaries downloaded from the Internet by default. Allow execution:

```bash
sudo xattr -d com.apple.quarantine /usr/local/bin/senhub-agent
```

**Alternative**: If a security popup appears at launch, go to **System Preferences → Security & Privacy** and click "Open Anyway".

**📸 SCREENSHOT TO INSERT**: macOS dialog "The application cannot be opened because it is from an unidentified developer"

### Step 4: Install the Service

```bash
# HTTPS installation (recommended)
sudo senhub-agent install --offline --enable-https
```

This creates a LaunchDaemon in `/Library/LaunchDaemons/io.senhub.agent.plist` that starts the agent automatically at boot.

### Step 5: Start the Service

```bash
# Load the LaunchDaemon
sudo launchctl load /Library/LaunchDaemons/io.senhub.agent.plist

# Verify it's running
sudo launchctl list | grep senhub
```

You should see a line with `io.senhub.agent`.

**📸 SCREENSHOT TO INSERT**: macOS terminal with `launchctl list | grep senhub` output

### LaunchDaemon Configuration

The automatically created plist file:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>io.senhub.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/senhub-agent</string>
        <string>run</string>
        <string>--offline</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
```

### macOS Files and Folders

```
/usr/local/bin/
└── senhub-agent                      # Main binary

/usr/local/etc/senhub-agent/
└── agent-config.yaml                 # Configuration

/usr/local/var/senhub-agent/
└── certs/                            # SSL certificates (if HTTPS)
    ├── agent-cert.pem
    └── agent-key.pem

/Library/Logs/SenHub/
└── agent.log                         # Logs
```

---

## Getting Started

Congratulations! Your SenHub agent is now installed and running. Here's how to verify everything works correctly and access your first metrics.

### 1. Retrieve the Agent Key

The agent key (authentication key) is a UUID automatically generated during installation. You need it to access the web interface and APIs.

**Method 1: Check configuration**

```bash
# Linux
cat /etc/senhub-agent/agent-config.yaml | grep "authentication_key:"

# macOS
cat /usr/local/etc/senhub-agent/agent-config.yaml | grep "authentication_key:"

# Windows
type "C:\Program Files\SenHub\agent-config.yaml" | findstr "authentication_key:"
```

**Method 2: Check installation logs**

The key is displayed during installation in the logs.

### 2. Verify the Service

Ensure the service is running properly.

**Windows**:
```powershell
Get-Service "SenHub Agent"
# Should display: Status = Running
```

**Linux**:
```bash
sudo systemctl status senhub-agent
# Should display: Active: active (running)
```

**macOS**:
```bash
sudo launchctl list | grep senhub
# Should return a line with the PID
```

### 3. Check Logs

Logs confirm the agent is collecting metrics.

**View last 20 lines**:

```bash
# Linux
sudo tail -20 /var/log/senhub-agent/agent.log

# macOS
sudo tail -20 /Library/Logs/SenHub/agent.log

# Windows
Get-Content "C:\ProgramData\SenHub\Logs\agent.log" -Tail 20
```

**Expected logs (successful startup)**:

```
2025-12-19T10:00:00Z INF Agent started version=0.1.80 mode=offline module=agent.core
2025-12-19T10:00:00Z INF HTTP server started port=8443 tls=true module=strategy.http
2025-12-19T10:00:01Z INF Probe started probe=cpu interval=30s module=probe.cpu
2025-12-19T10:00:01Z INF Probe started probe=memory interval=30s module=probe.memory
2025-12-19T10:00:01Z INF Probe started probe=logicaldisk interval=60s module=probe.logicaldisk
2025-12-19T10:00:01Z INF Probe started probe=network interval=60s module=probe.network
```

If you see these lines, everything is working correctly!

### 4. Test the REST API

Before opening the browser, quickly test the API:

```bash
# Replace {AGENT_KEY} with your actual key
curl -k https://localhost:8443/api/{AGENT_KEY}/info/system
```

**Expected response**:

```json
{
  "hostname": "PROD-SERVER-01",
  "os": "linux",
  "os_version": "Ubuntu 22.04.3 LTS",
  "agent_version": "0.1.80",
  "uptime_seconds": 135,
  "mode": "offline",
  "cache": {
    "retention_minutes": 10
  }
}
```

**📸 SCREENSHOT TO INSERT**: Terminal with curl command and formatted JSON response

### 5. Access the Web Interface

Open your browser and go to the dashboard:

**HTTP mode**:
```
http://localhost:8080/web/{AGENT_KEY}/dashboard
```

**HTTPS mode**:
```
https://localhost:8443/web/{AGENT_KEY}/dashboard
```

> **💡 HTTPS Note**: If using a self-signed certificate, your browser will show a security warning. Click "Advanced" then "Continue to site" (labels vary by browser).

**What you should see**:
- System overview (hostname, OS, uptime)
- License status (Free tier by default)
- List of active probes (cpu, memory, logicaldisk, network)
- Real-time metrics (graphs, values)

**📸 SCREENSHOT TO INSERT**: Complete dashboard showing CPU, Memory, Disk, Network metrics with graphs

### 6. Explore the API

The dashboard includes an interactive **API Explorer** to test all available endpoints.

**Navigate to**: `https://localhost:8443/web/{AGENT_KEY}/api-explorer`

**Try these endpoints**:

| Endpoint | Description | Format |
|----------|-------------|--------|
| `/api/{key}/info/probes` | List of active probes | JSON |
| `/api/{key}/metrics` | All metrics | JSON |
| `/api/{key}/prtg/metrics/cpu` | CPU metrics for PRTG | XML |
| `/api/{key}/license/status` | License status | JSON |

**📸 SCREENSHOT TO INSERT**: API Explorer showing call to `/info/probes` with JSON response

### Validation Checklist

Verify everything works:

- [ ] Service started and active
- [ ] Logs without critical errors (`ERR` or `FATAL`)
- [ ] Web interface accessible
- [ ] API responds with code 200
- [ ] Dashboard displays CPU/Memory metrics
- [ ] Probes collecting data (`/info/probes` returns counters > 0)

If all points are checked, your installation is successful! 🎉

### Next Steps

Now that the agent is installed, you can:

1. **Understand modes**: Read [OPERATING-MODES.md](./OPERATING-MODES.md) for online/offline differences
2. **Configure agent**: See [AGENT-CONFIGURATION.md](./AGENT-CONFIGURATION.md) to customize configuration
3. **Add probes**: Check [PROBES-CONFIGURATION.md](./PROBES-CONFIGURATION.md) to monitor Redfish, Citrix, NetScaler, etc.
4. **Integrate with PRTG/Nagios**: Read [METRICS-USAGE.md](./METRICS-USAGE.md)

---

## Uninstallation

If you need to uninstall the agent, follow these steps.

### Standard Uninstallation

This method removes the service but keeps configuration and logs.

**Windows**:

```powershell
# Stop the service
.\senhub-agent.exe stop

# Uninstall the service
.\senhub-agent.exe uninstall

# Manually remove files
Remove-Item -Recurse "C:\Program Files\SenHub"
```

**Linux**:

```bash
# Stop the service
sudo systemctl stop senhub-agent

# Uninstall
sudo senhub-agent uninstall

# Remove binary
sudo rm /usr/local/bin/senhub-agent
```

**macOS**:

```bash
# Stop the service
sudo launchctl unload /Library/LaunchDaemons/io.senhub.agent.plist

# Uninstall
sudo senhub-agent uninstall

# Remove binary
sudo rm /usr/local/bin/senhub-agent
```

### Complete Uninstallation (Purge)

This method removes **everything**: service, configuration, certificates, logs.

```bash
# All platforms
sudo senhub-agent uninstall --purge
```

**Files removed**:
- Configuration (`agent-config.yaml`)
- SSL certificates (`certs/`)
- Logs (`agent.log`)
- Local cache

---

## Installation Troubleshooting

Here are common issues and their solutions.

### Issue: Service won't start

**Symptoms**: `systemctl status senhub-agent` shows "failed" or service stops immediately.

**Solution**:

1. **Check detailed logs**:

```bash
# Linux
sudo journalctl -u senhub-agent -n 50

# Windows
Get-Content "C:\ProgramData\SenHub\Logs\agent.log" -Tail 50

# macOS
sudo tail -50 /Library/Logs/SenHub/agent.log
```

2. **Common errors**:

**"Port already in use"**:
```bash
# Identify which process is using the port
sudo lsof -i :8443  # Linux/macOS
netstat -ano | findstr :8443  # Windows

# Solution: Change port
senhub-agent install --offline --enable-https --https-port 9443
```

**"Permission denied"**:
- Verify you have admin/root rights
- On Linux: Check binary permissions (`chmod +x`)

**"Configuration file not found"**:
- Re-run `senhub-agent install --offline` to regenerate config

### Issue: Invalid HTTPS certificates

**Symptoms**: Browser refuses connection with SSL error.

**Solution**:

```bash
# Regenerate certificates
sudo senhub-agent stop
sudo rm -rf ./certs/  # or /var/lib/senhub-agent/certs/
sudo senhub-agent install --offline --enable-https \
  --https-hosts "monitoring.local,192.168.1.100"
sudo senhub-agent start
```

### Issue: Web interface inaccessible from network

**Symptoms**: `curl http://localhost:8080` works, but not from another machine.

**Solutions**:

1. **Check bind address**:

```bash
# Configuration must have bind_address: "0.0.0.0"
cat /etc/senhub-agent/agent-config.yaml | grep bind_address
```

If you see `127.0.0.1`, reinstall with HTTPS which uses `0.0.0.0` by default.

2. **Check firewall**:

```bash
# Test if port is open
sudo netstat -tlnp | grep 8443

# If port not listed, check firewall
sudo ufw status  # Ubuntu
sudo firewall-cmd --list-ports  # RHEL/CentOS
```

### Support

If you encounter other issues:

- **Complete documentation**: See [TROUBLESHOOTING.md](./TROUBLESHOOTING.md)
- **Email**: support@senhub.io
- **GitHub Issues**: https://github.com/senhub-io/senhub-agent/issues

---

**You're ready!** Installation is complete. Now check [AGENT-CONFIGURATION.md](./AGENT-CONFIGURATION.md) to customize your configuration and [PROBES-CONFIGURATION.md](./PROBES-CONFIGURATION.md) to add advanced monitoring probes.
