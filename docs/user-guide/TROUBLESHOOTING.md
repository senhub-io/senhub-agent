# Troubleshooting SenHub Agent

This guide provides systematic diagnostic procedures for resolving common issues with SenHub Agent. Whether you're troubleshooting installation failures, probe errors, or performance problems, this document walks through the diagnostic process from symptom identification through root cause analysis and resolution.

## Table of Contents

- [Understanding the Troubleshooting Process](#understanding-the-troubleshooting-process)
- [Logging and Diagnostic Tools](#logging-and-diagnostic-tools)
- [Installation and Startup Issues](#installation-and-startup-issues)
- [Configuration Issues](#configuration-issues)
- [License Issues](#license-issues)
- [Network Connectivity Issues](#network-connectivity-issues)
- [Probe Collection Issues](#probe-collection-issues)
- [Performance and Resource Issues](#performance-and-resource-issues)
- [Integration Issues with Monitoring Systems](#integration-issues-with-monitoring-systems)
- [Quick Reference: Common Issues](#quick-reference-common-issues)

---

## Understanding the Troubleshooting Process

Before diving into specific issues, understanding the systematic troubleshooting approach used in this guide will help you diagnose problems efficiently.

### Troubleshooting Methodology

```mermaid
graph TD
    SYMPTOM[1. Identify Symptom] --> GATHER[2. Gather Information]
    GATHER --> HYPOTHESIS[3. Form Hypothesis]
    HYPOTHESIS --> TEST[4. Test Hypothesis]
    TEST --> RESOLVED{Issue<br/>Resolved?}
    RESOLVED -->|No| HYPOTHESIS
    RESOLVED -->|Yes| VERIFY[5. Verify Solution]
    VERIFY --> DOCUMENT[6. Document Resolution]

    style SYMPTOM fill:#fff9c4
    style GATHER fill:#e1f5ff
    style RESOLVED fill:#c8e6c9
```

**Step 1: Identify Symptom**
- What specific behavior are you observing?
- When did the problem start?
- Has anything changed recently (configuration, infrastructure, software updates)?

**Step 2: Gather Information**
- Check agent logs for error messages
- Verify agent service status
- Test API endpoints for accessibility
- Review configuration files for correctness

**Step 3: Form Hypothesis**
- Based on symptoms and gathered information, what could cause this behavior?
- Prioritize hypotheses by likelihood (common issues first)

**Step 4: Test Hypothesis**
- Make one change at a time
- Document what you changed
- Observe the result

**Step 5: Verify Solution**
- Confirm issue is resolved
- Test related functionality to ensure no side effects
- Monitor for recurrence

**Step 6: Document Resolution**
- Record what caused the issue
- Document the solution applied
- Update runbooks or documentation for future reference

### SenHub Agent Architecture for Troubleshooting

Understanding the agent's architecture helps identify where problems originate:

```mermaid
graph TD
    CONFIG[Configuration Provider<br/>Local or Remote] --> AGENT[Agent Core<br/>Orchestration]
    LICENSE[License Validator] --> AGENT

    AGENT --> PROBE_REG[Probe Registry]
    PROBE_REG --> PROBES[Probes<br/>Data Collection]

    PROBES --> DATASTORE[Data Store<br/>In-Memory Cache]
    DATASTORE --> STRATEGIES[Strategies]

    STRATEGIES --> HTTP[HTTP Strategy<br/>API Endpoints]
    STRATEGIES --> SENHUB[SenHub Strategy]

    HTTP --> CLIENTS[Monitoring Clients<br/>PRTG, Nagios, Grafana]

    style AGENT fill:#81d4fa
    style PROBES fill:#fff9c4
    style DATASTORE fill:#c8e6c9
```

**Troubleshooting by layer:**
- **Configuration layer**: YAML syntax errors, missing required fields, incorrect values
- **License layer**: Invalid license, expired license, unauthorized probes
- **Agent core**: Service startup failures, permission issues, port conflicts
- **Probe layer**: Connection failures, authentication errors, data collection failures
- **Data store/cache**: Metrics not found, cache expiration issues
- **Strategy layer**: API endpoint errors, format conversion issues
- **Network layer**: Firewall blocking, bind address restrictions, TLS certificate errors

---

## Logging and Diagnostic Tools

SenHub Agent provides a modular logging system that allows enabling detailed debug output for specific components without restarting the agent. This is the primary diagnostic tool for troubleshooting.

### Modular Logging Architecture

```mermaid
graph LR
    LOGGER[Logging System] --> GLOBAL[Global Level<br/>info]
    LOGGER --> MODULES[Module-Specific Levels]

    MODULES --> M1[agent.core: info]
    MODULES --> M2[probe.redfish: debug]
    MODULES --> M3[strategy.http: info]
    MODULES --> M4[configuration.local: info]

    API[Runtime API<br/>/debug/logs] --> MODULES
    CLI[CLI Flags<br/>--debug-modules] --> MODULES

    style LOGGER fill:#81d4fa
    style API fill:#c8e6c9
```

**Design rationale**: Enabling debug logs for all modules generates excessive output and makes finding relevant information difficult. Module-specific logging allows targeting specific components for troubleshooting without noise from unrelated systems.

### Log Levels

```
disabled < trace < debug < info < warn < error < fatal < panic
           └─────────────────┬─────────────────┘
                  Increasing severity →
```

**When to use each level:**
- **debug**: Troubleshooting specific component behavior (verbose output with internal state)
- **info**: Normal operational messages (default level, shows key events without excessive detail)
- **warn**: Potential issues that don't prevent operation (e.g., retrying failed connection)
- **error**: Errors that affect specific probes or features (e.g., probe collection failed)

### Available Log Modules

| Module | Component | When to Enable |
|--------|-----------|----------------|
| `agent.core` | Main agent orchestration | Agent startup failures, general operational issues |
| `configuration.local` | Offline mode configuration | YAML parsing errors, probe configuration issues |
| `configuration.remote` | Online mode configuration | Remote configuration fetch failures |
| `probe.cpu` | CPU metrics collection | CPU metrics not appearing |
| `probe.memory` | Memory metrics collection | Memory metrics not appearing |
| `probe.logicaldisk` | Disk metrics collection | Disk metrics not appearing |
| `probe.network` | Network metrics collection | Network metrics not appearing |
| `probe.redfish` | Redfish hardware monitoring | Redfish connection issues, authentication failures |
| `probe.citrix` | Citrix VDI monitoring | Citrix API errors, session data issues |
| `probe.netscaler` | NetScaler load balancer monitoring | NetScaler NITRO API errors |
| `strategy.http` | HTTP API and cache | API endpoint errors, cache issues, format conversion errors |

### Enabling Debug Logs at Agent Startup

**Full verbose mode (all modules debug - use for general troubleshooting):**
```bash
# Linux/macOS
sudo senhub-agent run --verbose --offline

# Windows
.\senhub-agent.exe run --verbose --offline
```

**Impact**: Very verbose output. Use when you don't know which component is causing the issue.

**Selective debug mode (specific modules - recommended for targeted troubleshooting):**
```bash
# Debug only Redfish and HTTP strategy
senhub-agent run --debug-modules "probe.redfish,strategy.http" --offline

# Debug configuration parsing
senhub-agent run --debug-modules "configuration.local" --offline

# Debug all probes
senhub-agent run --debug-modules "probe.cpu,probe.memory,probe.logicaldisk,probe.network" --offline
```

**Impact**: Focused output only from specified modules. Use when you know which component to investigate.

### Runtime Log Level Changes (Without Restart)

**Check current log levels:**
```bash
curl http://localhost:8080/api/{key}/debug/logs
```

**Response:**
```json
{
  "global_level": "info",
  "modules": {
    "agent.core": "info",
    "probe.redfish": "info",
    "strategy.http": "info",
    "configuration.local": "info"
  }
}
```

**Enable debug for specific modules:**
```bash
curl -X POST http://localhost:8080/api/{key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{
    "module_levels": [
      {"module": "probe.redfish", "level": "debug"},
      {"module": "strategy.http", "level": "debug"}
    ]
  }'
```

**Response:**
```json
{
  "status": "success",
  "updated_modules": ["probe.redfish", "strategy.http"],
  "message": "Log levels updated successfully"
}
```

**Disable debug (revert to info):**
```bash
curl -X POST http://localhost:8080/api/{key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{
    "module_levels": [
      {"module": "probe.redfish", "level": "info"}
    ]
  }'
```

**When to use runtime activation:**
- Agent is already running in production
- You want to enable debug briefly without restarting
- You're troubleshooting intermittent issues and need logs when they occur

### Viewing and Analyzing Logs

**Log file locations:**

| Platform | Path |
|----------|------|
| **Linux** | `/var/log/senhub-agent/agent.log` |
| **macOS** | `/Library/Logs/SenHub/agent.log` |
| **Windows** | `C:\ProgramData\SenHub\Logs\agent.log` |

**Viewing logs in real-time:**

```bash
# Linux/macOS - Follow logs as they're written
sudo tail -f /var/log/senhub-agent/agent.log

# Filter by specific module
sudo tail -f /var/log/senhub-agent/agent.log | grep "probe.redfish"

# Filter by error level
sudo tail -f /var/log/senhub-agent/agent.log | grep "ERR"

# Windows PowerShell - Follow logs
Get-Content "C:\ProgramData\SenHub\Logs\agent.log" -Tail 50 -Wait

# Filter errors only
Get-Content "C:\ProgramData\SenHub\Logs\agent.log" -Tail 100 | Select-String "ERR"
```

**Common log patterns to look for:**

```bash
# Startup and initialization
[INFO] [agent.core] Starting SenHub Agent version=0.1.72
[INFO] [agent.core] Operating mode: offline

# Probe registration
[INFO] [agent.core] Probe registered name=cpu type=cpu
[INFO] [agent.core] Probe registered name=Production-iDRAC type=redfish

# Probe collection success
[DEBUG] [probe.cpu] Collection completed metrics=12 duration=23ms
[DEBUG] [probe.redfish] Redfish data fetched endpoint=https://idrac.local

# Probe collection errors
[ERR] [probe.redfish] Failed to connect endpoint=https://idrac.local error="dial tcp: connection refused"
[ERR] [probe.citrix] Authentication failed url=https://director.local error="401 Unauthorized"

# Cache operations
[DEBUG] [strategy.http] Metrics cached probe=cpu metrics=12 ttl=10m
[DEBUG] [strategy.http] Serving metrics from cache probe=cpu age=2m

# API requests
[DEBUG] [strategy.http] HTTP request method=GET path=/api/{key}/metrics client=192.168.1.50
[DEBUG] [strategy.http] HTTP response status=200 duration=15ms
```

### Diagnostic API Endpoints

Beyond logs, SenHub Agent exposes diagnostic endpoints for runtime inspection:

**System information:**
```bash
curl http://localhost:8080/api/{key}/info/system
```

**Response includes:**
```json
{
  "hostname": "prod-server-01",
  "os": "linux",
  "agent_version": "0.1.72",
  "mode": "offline",
  "uptime_seconds": 86400,
  "cache": {
    "retention_minutes": 10
  }
}
```

**Probe status:**
```bash
curl http://localhost:8080/api/{key}/info/probes
```

**Response includes:**
```json
{
  "probes": [
    {
      "name": "cpu",
      "type": "cpu",
      "status": "running",
      "last_collection": "2025-01-15T10:30:45Z",
      "metrics_count": 12,
      "error": null
    },
    {
      "name": "Production-iDRAC",
      "type": "redfish",
      "status": "error",
      "last_collection": "2025-01-15T10:25:12Z",
      "metrics_count": 0,
      "error": "connection refused"
    }
  ]
}
```

**Use this endpoint to:**
- Quickly identify which probes are failing
- See when probes last collected successfully
- Determine if probes are producing metrics

---

## Installation and Startup Issues

### Workflow: Diagnosing Service Startup Failures

This workflow addresses the most common issue after installation: the agent service won't start.

#### Symptom

After running `senhub-agent install`, the service fails to start:

```bash
# Linux
sudo systemctl status senhub-agent
● senhub-agent.service - SenHub Monitoring Agent
   Loaded: loaded
   Active: failed (Result: exit-code)

# Windows
Get-Service SenHubAgent
Status: Stopped
```

#### Diagnostic Procedure

**Step 1: Check Service Logs**

```bash
# Linux - systemd journal
sudo journalctl -u senhub-agent -n 100 --no-pager

# Linux - if using syslog
sudo grep senhub /var/log/syslog | tail -50

# Windows - Event Viewer
Get-EventLog -LogName Application -Source "SenHub Agent" -Newest 20
```

**Look for specific error patterns:**

| Error Message | Cause | Solution |
|---------------|-------|----------|
| `bind: address already in use` | Port 8080 or 8443 already occupied | [Change port](#port-already-in-use) |
| `permission denied` | Insufficient permissions to bind port | [Run as administrator/root](#insufficient-permissions) |
| `failed to parse configuration` | Invalid YAML syntax | [Validate configuration](#invalid-configuration-syntax) |
| `certificate verify failed` | Invalid TLS certificates | [Regenerate certificates](#invalid-tls-certificates) |

**Step 2: Test Manual Startup**

Running the agent manually (not as a service) provides immediate error output:

```bash
# Linux/macOS
sudo /usr/local/bin/senhub-agent run --offline --verbose

# Windows (as Administrator)
cd "C:\Program Files\SenHub"
.\senhub-agent.exe run --offline --verbose
```

**If agent starts successfully when run manually but fails as a service:**
- **Linux**: Service user lacks permissions (check `/etc/systemd/system/senhub-agent.service` User= field)
- **Windows**: Service account lacks permissions (use Local System account)

#### Common Root Causes and Resolutions

##### Port Already in Use

**Symptom:**
```
[ERR] [agent.core] Failed to start HTTP server error="listen tcp :8080: bind: address already in use"
```

**Diagnosis - Identify conflicting process:**
```bash
# Linux/macOS
sudo lsof -i :8080
# COMMAND   PID     USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
# nginx    1234    root    6u  IPv4  12345      0t0  TCP *:8080 (LISTEN)

# Windows
netstat -ano | findstr :8080
# TCP    0.0.0.0:8080    0.0.0.0:0    LISTENING    4567
#                                                  └─ PID
tasklist /FI "PID eq 4567"
```

**Resolution Option A: Change agent port**

Edit agent configuration:
```yaml
# /etc/senhub-agent/agent-config.yaml (Linux)
# C:\Program Files\SenHub\agent-config.yaml (Windows)

storage:
  - name: http
    params:
      port: 8081  # Changed from 8080
      bind_address: "0.0.0.0"
```

Restart agent:
```bash
# Linux
sudo systemctl restart senhub-agent

# Windows
Restart-Service SenHubAgent
```

**Resolution Option B: Stop conflicting service**

Only if the other service is not needed:
```bash
# Linux (example: stop nginx)
sudo systemctl stop nginx
sudo systemctl disable nginx

# Windows (example: stop IIS)
Stop-Service W3SVC
Set-Service W3SVC -StartupType Disabled
```

##### Insufficient Permissions

**Symptom:**
```
[ERR] [agent.core] Failed to bind to port error="listen tcp :8080: bind: permission denied"
```

**Cause**: On Linux/macOS, binding to ports < 1024 requires root privileges. Even for ports >= 1024, service users may lack permissions.

**Resolution - Linux (systemd service):**

Verify service runs as root:
```bash
# Check service configuration
sudo cat /etc/systemd/system/senhub-agent.service

# Should contain:
# [Service]
# User=root
# Group=root
```

If service runs as non-root user, either:
- Change User=root in service file, or
- Grant CAP_NET_BIND_SERVICE capability:
```bash
sudo setcap 'cap_net_bind_service=+ep' /usr/local/bin/senhub-agent
sudo systemctl daemon-reload
sudo systemctl restart senhub-agent
```

**Resolution - Windows:**

Run service as Local System:
```powershell
# Stop service
Stop-Service SenHubAgent

# Change service account to Local System
sc.exe config SenHubAgent obj= "LocalSystem"

# Start service
Start-Service SenHubAgent
```

##### Invalid Configuration Syntax

**Symptom:**
```
[ERR] [configuration.local] Failed to parse configuration file="/etc/senhub-agent/agent-config.yaml" error="yaml: line 15: did not find expected key"
```

**Diagnosis:**

Enable configuration debug logging:
```bash
senhub-agent run --debug-modules "configuration.local" --offline
```

**Common YAML syntax errors:**

❌ **Incorrect indentation** (most common):
```yaml
agent:
key: "test"  # Missing 2 spaces indentation
```

✅ **Correct**:
```yaml
agent:
  key: "test"
```

❌ **Missing quotes around strings with special characters**:
```yaml
agent:
  key: f47ac10b-58cc-4372-a567-0e02b2c3d479  # No quotes
```

✅ **Correct**:
```yaml
agent:
  key: "f47ac10b-58cc-4372-a567-0e02b2c3d479"
```

❌ **Mixing tabs and spaces** (invisible but breaks parsing):
```yaml
agent:
→ key: "test"  # Tab character used
```

✅ **Correct** (use spaces only):
```yaml
agent:
  key: "test"  # 2 spaces
```

**Resolution:**

Validate YAML syntax with online validator (https://yamlchecker.com) or yamllint:
```bash
# Install yamllint
sudo apt-get install yamllint  # Ubuntu/Debian
sudo yum install yamllint      # RHEL/CentOS

# Validate configuration
yamllint /etc/senhub-agent/agent-config.yaml
```

Fix syntax errors, then restart agent.

##### Invalid TLS Certificates

**Symptom:**
```
[ERR] [agent.core] Failed to start HTTPS server error="tls: failed to find any PEM data in certificate input"
```

**Cause**: Certificate files corrupted, wrong format, or paths incorrect.

**Resolution - Regenerate certificates:**

```bash
# Linux/macOS
sudo systemctl stop senhub-agent
sudo rm -rf /etc/senhub-agent/certs/
sudo senhub-agent install --offline --enable-https \
  --https-hosts "monitoring.company.com,192.168.1.100"
sudo systemctl start senhub-agent

# Windows
Stop-Service SenHubAgent
Remove-Item -Recurse -Force "C:\Program Files\SenHub\certs"
cd "C:\Program Files\SenHub"
.\senhub-agent.exe install --offline --enable-https --https-hosts "monitoring.company.com,192.168.1.100"
Start-Service SenHubAgent
```

**If using custom certificates**, verify format:
```bash
# Certificate should be PEM format (Base64 encoded)
head -1 /etc/senhub-agent/certs/server.crt
# Expected: -----BEGIN CERTIFICATE-----

# Private key should also be PEM format
head -1 /etc/senhub-agent/certs/server.key
# Expected: -----BEGIN RSA PRIVATE KEY-----
```

---

## Configuration Issues

### Workflow: Diagnosing Probe Configuration Problems

Probes fail to start or don't appear in `/api/{key}/info/probes` due to configuration errors.

#### Symptom

Probe configured in `agent-config.yaml` but not running:

```bash
curl http://localhost:8080/api/{key}/info/probes | jq '.probes[] | select(.name=="Production-iDRAC")'
# No output - probe not registered
```

#### Diagnostic Procedure

**Step 1: Check Probe Configuration Syntax**

Common configuration errors by probe type:

**Redfish Probe:**
```yaml
# ❌ Missing required fields
probes:
  - name: "Production iDRAC"
    type: redfish
    params:
      interval: 300
      # Missing: endpoint, username, password

# ✅ Complete configuration
probes:
  - name: "Production iDRAC"
    type: redfish
    params:
      endpoint: "https://idrac.company.com"
      username: "root"
      password: "SecurePassword"
      interval: 300
      verify_ssl: false
```

**Citrix Probe:**
```yaml
# ❌ Incorrect username format
probes:
  - name: "Citrix DDC"
    type: citrix
    params:
      base_url: "https://director.company.com"
      username: "user@domain.com"  # Wrong format
      password: "password"

# ✅ Correct domain format
probes:
  - name: "Citrix DDC"
    type: citrix
    params:
      base_url: "https://director.company.com"
      username: "DOMAIN\\user"  # Correct format
      password: "password"
```

**NetScaler Probe:**
```yaml
# ❌ Missing NITRO API credentials
probes:
  - name: "NetScaler LB"
    type: netscaler
    params:
      endpoint: "https://netscaler.company.com"
      # Missing: username, password

# ✅ Complete with NITRO credentials
probes:
  - name: "NetScaler LB"
    type: netscaler
    params:
      endpoint: "https://netscaler.company.com"
      username: "nsroot"
      password: "SecurePassword"
      interval: 120
      verify_ssl: false
```

**Step 2: Enable Debug Logging**

```bash
# Enable debug for specific probe
curl -X POST http://localhost:8080/api/{key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{
    "module_levels": [
      {"module": "probe.redfish", "level": "debug"},
      {"module": "configuration.local", "level": "debug"}
    ]
  }'

# Check logs for detailed error messages
sudo tail -f /var/log/senhub-agent/agent.log | grep -E "probe.redfish|configuration.local"
```

**Step 3: Identify Error Type**

**Configuration validation errors** (appear at startup):
```
[ERR] [configuration.local] Probe configuration invalid probe=Production-iDRAC error="endpoint is required"
[ERR] [configuration.local] Probe configuration invalid probe=Citrix-DDC error="base_url must start with http:// or https://"
```

**License authorization errors** (probe type requires paid license):
```
[WARN] [agent.core] Probe not authorized tier=free probe=redfish
[INFO] [agent.core] Upgrade to Pro or Enterprise license to enable redfish probe
```

**Resolution:**
- Configuration errors: Fix configuration syntax, restart agent
- License errors: Install appropriate license (see [License Issues](#license-issues))

#### Common Configuration Issues

##### Probe Name vs. Type Confusion

**Issue:** Probe `name` and `type` fields confused.

**Understanding the distinction:**
- `name`: **Display name** - free choice, used for identification in logs, metrics tags, and UI
- `type`: **Probe type** - must match registered probe type exactly (cpu, memory, redfish, citrix, netscaler, etc.)

```yaml
# ❌ Incorrect - reversed name and type
probes:
  - name: redfish  # This is treated as display name, not probe type
    type: "Production iDRAC"  # This must be a valid probe type

# ✅ Correct
probes:
  - name: "Production iDRAC"  # Display name (can be anything descriptive)
    type: redfish  # Probe type (must match exactly)
    params:
      endpoint: "https://idrac.company.com"
```

##### Interval Configuration

**Issue:** Interval value too low causes excessive CPU usage; interval too high causes stale metrics.

**Recommended intervals by probe type:**
```yaml
probes:
  # System probes - high volatility
  - name: cpu
    type: cpu
    params:
      interval: 30  # 30-60 seconds recommended

  - name: memory
    type: memory
    params:
      interval: 60  # 60 seconds recommended

  # Infrastructure probes - low volatility
  - name: "Hardware Monitoring"
    type: redfish
    params:
      interval: 300  # 5 minutes recommended (hardware temps change slowly)

  - name: "VDI Monitoring"
    type: citrix
    params:
      interval: 120  # 2 minutes recommended (session metrics moderate volatility)
```

**Impact of interval choices:**
- **Too low (<30s for system probes)**: High CPU usage, excessive log output, minimal benefit (metrics don't change that fast)
- **Too high (>5min for system probes)**: Metrics may appear stale in monitoring system, miss transient spikes
- **Mismatched with monitoring system**: PRTG scans every 60s, but probe interval is 300s → PRTG gets same data 5 times

---

## License Issues

### Workflow: Diagnosing License Problems

Paid probes (Redfish, Citrix, NetScaler, etc.) fail to start due to license issues.

#### Symptom

Probe configured but not appearing in probe list, with log message:

```
[WARN] [agent.core] Probe not authorized tier=free probe=redfish
[INFO] [agent.core] Upgrade to Pro or Enterprise license to enable redfish probe
```

#### Diagnostic Procedure

**Step 1: Check Current License Status**

```bash
curl http://localhost:8080/api/{key}/license/status
```

**Response indicates license state:**

**Case 1: No license installed (Free tier)**
```json
{
  "tier": "Free",
  "expires_at": null,
  "authorized_probes": ["cpu", "memory", "logicaldisk", "network"],
  "grace_period": false
}
```

**Case 2: License installed but expired**
```json
{
  "tier": "Pro",
  "expires_at": "2024-12-31T23:59:59Z",
  "expired": true,
  "grace_period": true,
  "grace_period_days_remaining": 4,
  "authorized_probes": ["cpu", "memory", "logicaldisk", "network", "redfish", "citrix"],
  "warning": "License expired - grace period active for 4 more days"
}
```

**Case 3: License installed and valid**
```json
{
  "tier": "Enterprise",
  "expires_at": "2025-12-31T23:59:59Z",
  "expired": false,
  "days_until_expiration": 345,
  "authorized_probes": ["*"],
  "status": "active"
}
```

**Step 2: Verify License Configuration**

Check that license JWT is correctly configured:

```bash
# Linux/macOS
sudo grep "license:" /etc/senhub-agent/agent-config.yaml

# Windows
Select-String -Path "C:\Program Files\SenHub\agent-config.yaml" -Pattern "license:"
```

**Expected format:**
```yaml
agent:
  authentication_key: "f47ac10b-58cc-4372-a567-0e02b2c3d479"
  license: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJ..."  # JWT token (very long string)
```

**Step 3: Validate License JWT Format**

**Common issues:**

❌ **Line breaks in JWT** (YAML multi-line format breaks token):
```yaml
agent:
  license: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.
    eyJhdXRob3JpemVkX3Byb2Jlcy..."  # WRONG - line break in middle of token
```

✅ **Correct** (single line):
```yaml
agent:
  license: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdXRob3JpemVkX3Byb2Jlcy..."
```

❌ **Missing quotes**:
```yaml
agent:
  license: eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...  # WRONG - no quotes
```

✅ **Correct**:
```yaml
agent:
  license: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9..."
```

**Validate JWT structure** (should have 3 parts separated by dots):
```bash
echo "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJ..." | awk -F'.' '{print NF-1}'
# Should output: 2 (indicating 3 parts: header.payload.signature)
```

#### Common License Issues and Resolutions

##### License Not Recognized After Installation

**Symptom:**
```bash
curl http://localhost:8080/api/{key}/license/status
# "tier": "Free"  (even though license was configured)
```

**Diagnosis:**

Check agent logs for license validation errors:
```bash
sudo grep -i license /var/log/senhub-agent/agent.log | tail -20
```

**Common error messages:**

```
[ERR] [license] Failed to parse license token error="invalid character '\n' in string literal"
→ Solution: License JWT contains line break - ensure single line in YAML

[ERR] [license] License signature validation failed
→ Solution: License token corrupted or tampered with - request new license

[ERR] [license] License expired at=2024-12-31T23:59:59Z
→ Solution: License expired - contact support@senhub.io for renewal
```

**Resolution:**

1. **Fix license format in configuration** (remove line breaks, add quotes)
2. **Restart agent** to reload license:
```bash
# Linux
sudo systemctl restart senhub-agent

# Windows
Restart-Service SenHubAgent

# macOS
sudo launchctl unload /Library/LaunchDaemons/io.senhub.agent.plist
sudo launchctl load /Library/LaunchDaemons/io.senhub.agent.plist
```

3. **Verify license loaded successfully**:
```bash
curl http://localhost:8080/api/{key}/license/status
# Should show correct tier: "Pro" or "Enterprise"
```

##### License Expired (Grace Period)

**Symptom:**

Web dashboard shows warning banner:
```
⚠ License Expired - Grace Period: 4 days remaining
Paid probes will be disabled after grace period expires.
Contact support@senhub.io for license renewal.
```

**Grace period behavior:**
- **Duration**: 7 days after license expiration date
- **Paid probes**: Continue working during grace period
- **Alerts**: Daily warning in logs + dashboard banner
- **After grace period**: Paid probes automatically disabled

**Resolution:**

**Immediate action required** - Contact support for license renewal:

**Email template:**
```
To: support@senhub.io
Subject: License Renewal Request - [Your Company Name]

License Information:
- Customer: [Company name]
- Current Tier: [Pro / Enterprise]
- Expiration Date: [Date from license status]
- Agent Count: [Number of agents deployed]

Renewal Request:
- Desired tier: [Same tier / Upgrade to Enterprise]
- Renewal period: [1 year / 2 years / 3 years]

Contact Information:
- Name: [Your name]
- Email: [your.email@company.com]
- Phone: [optional]
```

**After receiving new license:**

1. Update agent configuration with new license token
2. Restart agent (or wait for automatic reload - licenses checked every hour)
3. Verify renewal:
```bash
curl http://localhost:8080/api/{key}/license/status
# "expires_at": "2026-12-31T23:59:59Z"  (new expiration date)
# "days_until_expiration": 365
```

---

## Network Connectivity Issues

### Workflow: Diagnosing Web Interface Accessibility

The most common network issue: agent is running but web interface inaccessible.

#### Symptom

```bash
curl http://localhost:8080
# curl: (7) Failed to connect to localhost port 8080: Connection refused
```

Or from remote machine:
```bash
curl http://192.168.1.10:8080
# Timeout or connection refused
```

#### Diagnostic Decision Tree

```mermaid
graph TD
    START[Web Interface<br/>Inaccessible] --> Q1{Agent<br/>Running?}
    Q1 -->|No| FIX1[Start agent service]
    Q1 -->|Yes| Q2{Listening on<br/>correct port?}

    Q2 -->|No| FIX2[Check port configuration]
    Q2 -->|Yes| Q3{Listening on<br/>correct interface?}

    Q3 -->|127.0.0.1 only| FIX3[Change bind_address<br/>to 0.0.0.0]
    Q3 -->|0.0.0.0 or specific IP| Q4{Firewall<br/>blocking?}

    Q4 -->|Yes| FIX4[Configure firewall rules]
    Q4 -->|No| Q5{HTTPS certificate<br/>valid?}

    Q5 -->|No| FIX5[Regenerate or fix<br/>certificate]
    Q5 -->|Yes| RESOLVE[Check network routing,<br/>proxy, load balancer]

    style START fill:#ffccbc
    style RESOLVE fill:#c8e6c9
```

#### Step-by-Step Diagnostic Procedure

**Step 1: Verify Agent Running**

```bash
# Linux
sudo systemctl status senhub-agent
# Should show: Active: active (running)

# Windows
Get-Service SenHubAgent
# Should show: Status: Running

# macOS
sudo launchctl list | grep senhub
# Should show: PID and service name
```

**If not running:** See [Installation and Startup Issues](#installation-and-startup-issues)

**Step 2: Check Listening Ports**

```bash
# Linux/macOS
sudo lsof -i -P -n | grep senhub
# Expected output:
# senhub-ag 1234 root    7u  IPv4 12345      0t0  TCP *:8080 (LISTEN)

# Alternative with netstat
sudo netstat -tulpn | grep :8080

# Windows
netstat -ano | findstr :8080
# Expected output:
# TCP    0.0.0.0:8080    0.0.0.0:0    LISTENING    1234
```

**Interpretation:**

```
TCP    0.0.0.0:8080    LISTENING
       └──┬──┘
          └─ Listening on all interfaces (accessible from network)

TCP    127.0.0.1:8080    LISTENING
       └────┬────┘
            └─ Listening only on localhost (NOT accessible from network)
```

**If agent not listening on any port:**
- Configuration error (port directive missing or invalid)
- Agent started but HTTP strategy failed to initialize

Check agent logs:
```bash
sudo tail -100 /var/log/senhub-agent/agent.log | grep -E "HTTP|strategy.http"
```

**Step 3: Verify Bind Address Configuration**

**If agent listening on 127.0.0.1 but you need network access:**

Edit configuration:
```yaml
# /etc/senhub-agent/agent-config.yaml

storage:
  - name: http
    params:
      port: 8080
      bind_address: "0.0.0.0"  # Changed from "127.0.0.1"
```

**Security consideration**: Binding to 0.0.0.0 exposes agent to entire network. Combine with firewall rules to restrict access to monitoring servers only.

Restart agent:
```bash
sudo systemctl restart senhub-agent
```

Verify listening on all interfaces:
```bash
sudo lsof -i :8080
# Should show: TCP *:8080 (LISTEN)
```

**Step 4: Check Firewall Rules**

**Linux (UFW - Ubuntu/Debian):**
```bash
# Check UFW status
sudo ufw status verbose

# If port not allowed, add rule
sudo ufw allow 8080/tcp comment "SenHub Agent HTTP"
sudo ufw allow 8443/tcp comment "SenHub Agent HTTPS"
sudo ufw reload
```

**Linux (firewalld - RHEL/CentOS):**
```bash
# Check firewall status
sudo firewall-cmd --list-all

# If port not allowed, add rule
sudo firewall-cmd --permanent --add-port=8080/tcp
sudo firewall-cmd --permanent --add-port=8443/tcp
sudo firewall-cmd --reload
```

**Windows:**
```powershell
# Check existing rules
Get-NetFirewallRule | Where-Object {$_.LocalPort -eq 8080}

# If no rule exists, create one
New-NetFirewallRule -DisplayName "SenHub Agent HTTP" `
  -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow

New-NetFirewallRule -DisplayName "SenHub Agent HTTPS" `
  -Direction Inbound -Protocol TCP -LocalPort 8443 -Action Allow
```

**Step 5: Test Connectivity**

**From local machine:**
```bash
curl -v http://localhost:8080/api/{key}/info/system
```

**From remote machine:**
```bash
curl -v http://192.168.1.10:8080/api/{key}/info/system
```

**If local works but remote fails:**
- Firewall blocking (see Step 4)
- Network routing issue (check with `traceroute`)
- Intermediate proxy/load balancer blocking traffic

---

## Probe Collection Issues

### Workflow: Diagnosing Probe Collection Failures

Probes configured and registered but failing to collect metrics or producing errors.

#### Symptom

Probe appears in `/api/{key}/info/probes` but shows error status:

```json
{
  "name": "Production-iDRAC",
  "type": "redfish",
  "status": "error",
  "last_collection": "2025-01-15T10:25:12Z",
  "metrics_count": 0,
  "error": "dial tcp: connection refused"
}
```

Or metrics missing from `/api/{key}/metrics?probe=redfish`:
```json
{
  "metrics": []
}
```

#### Diagnostic Procedure by Probe Type

### Redfish Probe Collection Issues

**Common error patterns:**

**Error: Connection refused**
```
[ERR] [probe.redfish] Failed to connect endpoint="https://idrac.company.com" error="dial tcp: connection refused"
```

**Diagnosis:**
1. **Verify endpoint accessible** from agent server:
```bash
curl -k https://idrac.company.com/redfish/v1/
# Expected: JSON response with Redfish service root

# If connection refused:
ping idrac.company.com  # Check DNS resolution and reachability
telnet idrac.company.com 443  # Check TCP connectivity
```

2. **Check BMC/iDRAC network configuration**:
   - iDRAC IP address configured correctly
   - iDRAC network enabled (not disabled in BIOS)
   - VLAN configuration correct (if using VLANs)

**Error: Authentication failed (401 Unauthorized)**
```
[ERR] [probe.redfish] Authentication failed endpoint="https://idrac.company.com" error="401 Unauthorized"
```

**Diagnosis:**
1. **Verify credentials manually**:
```bash
curl -k -u "username:password" https://idrac.company.com/redfish/v1/Systems
# If 401: Credentials incorrect
# If 200: Credentials correct, configuration issue
```

2. **Check username format** - varies by vendor:
   - **Dell iDRAC**: `root` (default) or domain user `DOMAIN\user`
   - **HP iLO**: `Administrator` (default) or local user
   - **Supermicro**: `ADMIN` (default) or custom user

3. **Verify account permissions**:
   - Redfish account must have read permissions
   - Check BMC user settings for role assignments

**Error: TLS handshake failure**
```
[ERR] [probe.redfish] TLS handshake failed endpoint="https://idrac.company.com" error="x509: certificate signed by unknown authority"
```

**Resolution:**

Disable SSL verification for self-signed certificates:
```yaml
probes:
  - name: "Production iDRAC"
    type: redfish
    params:
      endpoint: "https://idrac.company.com"
      username: "root"
      password: "SecurePassword"
      verify_ssl: false  # Disable SSL verification
```

**Security note**: Only use `verify_ssl: false` with self-signed certificates on trusted internal networks. For production environments exposed to untrusted networks, use properly signed certificates.

### Citrix Probe Collection Issues

**Common error patterns:**

**Error: 401 Unauthorized**
```
[ERR] [probe.citrix] Authentication failed url="https://director.company.com" error="401 Unauthorized"
```

**Diagnosis:**

1. **Verify username format** - Citrix requires domain backslash format:
```yaml
# ❌ Incorrect formats
username: "user@domain.com"
username: "domain/user"

# ✅ Correct format
username: "DOMAIN\\user"  # Note: double backslash in YAML
```

2. **Test credentials manually** with Citrix Director web interface:
   - Navigate to: `https://director.company.com`
   - Login with same credentials
   - If web login fails: Credentials incorrect
   - If web login succeeds: Check API permissions

3. **Verify API access permissions**:
   - User must have "Read Only Administrator" or higher role in Citrix
   - Check Studio → Configuration → Administrators

**Error: No delivery controllers found**
```
[ERR] [probe.citrix] No delivery controllers discovered base_url="https://director.company.com"
```

**Diagnosis:**

1. **Verify base_url points to Director**:
```yaml
# ❌ Wrong - points to StoreFront
params:
  base_url: "https://storefront.company.com"

# ✅ Correct - points to Director
params:
  base_url: "https://director.company.com"
```

2. **Check Director is configured**:
   - Citrix Director must be installed and configured
   - Database connection must be functional
   - Access Director web interface to verify operational

### NetScaler Probe Collection Issues

**Common error patterns:**

**Error: NITRO API authentication failed**
```
[ERR] [probe.netscaler] NITRO API authentication failed endpoint="https://netscaler.company.com" error="Authentication failure"
```

**Diagnosis:**

1. **Verify NITRO API enabled** on NetScaler:
```bash
# SSH to NetScaler CLI
ssh nsroot@netscaler.company.com

# Check NITRO status
show httpProfile http
# Look for: HTTP: ENABLED
```

2. **Test NITRO API manually**:
```bash
# Get session cookie
curl -k -X POST https://netscaler.company.com/nitro/v1/config/login \
  -H "Content-Type: application/json" \
  -d '{"login":{"username":"nsroot","password":"password"}}'

# Expected: {"errorcode": 0, "message": "Done", "sessionid": "..."}
# If errorcode != 0: Credentials incorrect or API disabled
```

3. **Check NetScaler user permissions**:
   - User must have read permissions for statistics
   - Verify with: `show system user <username>`

**Error: SSL certificate verification failed**
```
[ERR] [probe.netscaler] TLS error endpoint="https://netscaler.company.com" error="x509: certificate signed by unknown authority"
```

**Resolution:**

Disable SSL verification:
```yaml
probes:
  - name: "NetScaler LB"
    type: netscaler
    params:
      endpoint: "https://netscaler.company.com"
      username: "nsroot"
      password: "SecurePassword"
      verify_ssl: false
```

---

## Performance and Resource Issues

### Workflow: Diagnosing High Memory Usage

#### Symptom

Agent consuming excessive memory (>500 MB for typical deployment):

```bash
# Linux
ps aux | grep senhub-agent
# USER       PID %CPU %MEM    VSZ   RSS
# root      1234  2.0 15.0 512000 768000

# Windows
Get-Process senhub-agent | Select-Object Name, CPU, WS
# WS (Working Set) > 500 MB
```

#### Diagnostic Procedure

**Step 1: Check Cache Configuration**

```bash
curl http://localhost:8080/api/{key}/info/system | jq '.cache'
```

**Response:**
```json
{
  "retention_minutes": 30
}
```

**Memory impact of cache retention:**
- **5 minutes**: ~50-100 MB (minimal memory usage)
- **10 minutes**: ~100-200 MB (recommended balance)
- **30 minutes**: ~300-600 MB (high memory usage)

**Resolution:**

Reduce cache retention if memory constrained:
```yaml
# /etc/senhub-agent/agent-config.yaml
cache:
  retention_minutes: 5  # Reduced from 30
```

**Trade-off:** Lower retention = metrics expire faster. If monitoring system queries at 5-minute intervals but cache retention is 5 minutes, queries may occasionally miss data during collection gaps.

**Recommended retention = max(probe_intervals) + 2 minutes:**
- Probes collect every 5 minutes → retention_minutes: 7
- Probes collect every 2 minutes → retention_minutes: 4

**Step 2: Check Active Probes and Metric Counts**

```bash
curl http://localhost:8080/api/{key}/info/probes | jq '.probes[] | {name, type, metrics_count}'
```

**Response:**
```json
[
  {"name": "cpu", "type": "cpu", "metrics_count": 12},
  {"name": "memory", "type": "memory", "metrics_count": 8},
  {"name": "Production-iDRAC", "type": "redfish", "metrics_count": 45},
  {"name": "NetScaler-PROD", "type": "netscaler", "metrics_count": 250}
]
```

**Memory calculation:**
```
Approximate memory = (total_metrics * retention_minutes * 8 bytes per sample)

Example:
- Total metrics: 315 (12 + 8 + 45 + 250)
- Retention: 30 minutes
- Samples: 30 minutes worth of historical data

Memory ≈ 315 metrics × 30 minutes × 8 bytes ≈ 75 KB (negligible)
```

**Actual memory usage higher due to:**
- Metric metadata (names, tags, units)
- Go runtime overhead
- HTTP server buffers

**If NetScaler probe has >500 metrics:** This is normal for large NetScaler deployments with many virtual servers. Memory usage is expected to be higher.

**Resolution if too many metrics:**

Use tag-based filtering to reduce metric count:
```yaml
probes:
  - name: "NetScaler - Load Balancing Only"
    type: netscaler
    params:
      endpoint: "https://netscaler.company.com"
      username: "nsroot"
      password: "password"
      filters:
        - tag: "metric_view"
          value: "load_balancing"  # Only collect LB metrics, not SSL or system
```

### Workflow: Diagnosing High CPU Usage

#### Symptom

Agent consuming excessive CPU (>20% sustained):

```bash
# Linux
top -p $(pgrep senhub-agent)
# %CPU column shows >20%

# Windows
Get-Process senhub-agent | Select-Object Name, CPU
```

#### Common Causes and Resolutions

**Cause 1: Collection intervals too short**

**Diagnosis:**
```bash
# Check probe intervals
curl http://localhost:8080/api/{key}/info/probes | jq '.probes[] | {name, interval_seconds}'
```

**If many probes have interval <30 seconds:** CPU overhead from continuous collection.

**Resolution:**

Increase intervals to recommended values:
```yaml
probes:
  - name: cpu
    type: cpu
    params:
      interval: 60  # Increased from 10 seconds

  - name: redfish
    type: redfish
    params:
      interval: 300  # 5 minutes for hardware monitoring
```

**Cause 2: Probe collection errors causing rapid retries**

**Diagnosis:**

Enable debug logging and look for repeated errors:
```bash
curl -X POST http://localhost:8080/api/{key}/debug/logs \
  -d '{"module_levels": [{"module": "probe.redfish", "level": "debug"}]}'

sudo tail -f /var/log/senhub-agent/agent.log | grep "probe.redfish"
```

**If you see rapid repeated errors** (multiple per second):
```
[ERR] [probe.redfish] Collection failed error="connection refused"
[ERR] [probe.redfish] Collection failed error="connection refused"
[ERR] [probe.redfish] Collection failed error="connection refused"
```

**Resolution:**

Fix the probe configuration causing errors (see [Probe Collection Issues](#probe-collection-issues)) or disable the failing probe temporarily:

```yaml
probes:
  # - name: "Broken Probe"  # Comment out failing probe
  #   type: redfish
  #   params:
  #     endpoint: "https://offline-idrac.local"
```

---

## Integration Issues with Monitoring Systems

See [Metrics Usage - Troubleshooting Integration Issues](./METRICS-USAGE.md#troubleshooting-integration-issues) for complete troubleshooting procedures for:
- PRTG sensor failures
- Nagios check timeouts
- Grafana "No Data" errors
- PRTG lookups not applied

---

## Quick Reference: Common Issues

### Quick Diagnostic Commands

**Check agent status:**
```bash
# Linux
sudo systemctl status senhub-agent

# Windows
Get-Service SenHubAgent

# macOS
sudo launchctl list | grep senhub
```

**Check agent is listening:**
```bash
# Linux/macOS
sudo lsof -i :8080

# Windows
netstat -ano | findstr :8080
```

**Test API accessibility:**
```bash
curl http://localhost:8080/api/{key}/info/system
```

**Check probe status:**
```bash
curl http://localhost:8080/api/{key}/info/probes
```

**Check license status:**
```bash
curl http://localhost:8080/api/{key}/license/status
```

**View recent logs:**
```bash
# Linux/macOS
sudo tail -100 /var/log/senhub-agent/agent.log

# Windows
Get-Content "C:\ProgramData\SenHub\Logs\agent.log" -Tail 100
```

### Common Issues Checklist

| Symptom | Quick Check | Most Likely Cause |
|---------|-------------|-------------------|
| Agent won't start | `sudo systemctl status senhub-agent` | Port conflict, invalid config, insufficient permissions |
| Web interface inaccessible | `sudo lsof -i :8080` | Agent not running, firewall blocking, bind address 127.0.0.1 |
| Probe not appearing | `curl .../info/probes` | License required, invalid configuration, probe type mismatch |
| Metrics missing | `curl .../metrics?probe=cpu` | Probe collection error, cache expired, probe interval too long |
| High memory usage | `curl .../info/system` | Cache retention too high (>30 min) |
| High CPU usage | Check intervals in config | Collection intervals too short (<30s) |
| License not recognized | `curl .../license/status` | JWT format error (line breaks), expired license |

### Error Message Quick Reference

| Error Message | File/Component | Resolution |
|---------------|----------------|------------|
| `bind: address already in use` | Agent startup | Change port or stop conflicting service |
| `permission denied` | Agent startup | Run as root/Administrator or grant CAP_NET_BIND_SERVICE |
| `failed to parse configuration` | Configuration | Fix YAML syntax (indentation, quotes) |
| `Probe not authorized tier=free` | License | Install Pro/Enterprise license |
| `connection refused` | Probe (Redfish/Citrix/NetScaler) | Verify endpoint accessible, check firewall |
| `401 Unauthorized` | Probe authentication | Verify credentials, check username format |
| `x509: certificate signed by unknown authority` | TLS certificate | Set `verify_ssl: false` or install valid certificate |
| `No metrics found in cache` | Cache | Wait for probe to collect (check interval), increase retention |

---

## Support and Escalation

When troubleshooting steps don't resolve your issue, gather diagnostic information before contacting support.

### Information to Provide to Support

**Email:** support@senhub.io

**Required information:**

1. **Agent version:**
```bash
senhub-agent version
```

2. **Operating system and version:**
```bash
# Linux
cat /etc/os-release

# Windows
systeminfo | findstr /B /C:"OS Name" /C:"OS Version"

# macOS
sw_vers
```

3. **Configuration file (anonymized):**

Remove sensitive information before sharing:
```yaml
# Anonymize before sharing:
agent:
  authentication_key: "REDACTED"
  license: "REDACTED"

probes:
  - name: "Production iDRAC"
    type: redfish
    params:
      endpoint: "https://REDACTED"
      username: "REDACTED"
      password: "REDACTED"
```

4. **Recent logs (last 100 lines):**
```bash
sudo tail -100 /var/log/senhub-agent/agent.log > agent-logs.txt
```

5. **System information API output:**
```bash
curl http://localhost:8080/api/{key}/info/system > agent-info.txt
curl http://localhost:8080/api/{key}/info/probes > agent-probes.txt
```

6. **Description of issue:**
- What specific behavior are you observing?
- When did the problem start?
- What troubleshooting steps have you already tried?
- Has anything changed recently (configuration, infrastructure, software updates)?

### Temporary Workarounds

While waiting for support response, consider temporary workarounds:

**Probe collection failing:**
- Disable failing probe temporarily:
```yaml
# probes:
#   - name: "Failing Probe"  # Commented out temporarily
#     type: redfish
```

**Performance issues:**
- Reduce collection frequency temporarily:
```yaml
probes:
  - name: cpu
    type: cpu
    params:
      interval: 120  # Temporarily increased from 60
```

**Network connectivity issues:**
- Use HTTP instead of HTTPS temporarily (development only):
```yaml
storage:
  - name: http
    params:
      port: 8080
      tls:
        enabled: false  # Temporarily disabled
```

---

## Next Steps

After resolving issues:

1. **[Agent Configuration](./AGENT-CONFIGURATION.md)** - Optimize cache, intervals, and license settings
2. **[HTTP/HTTPS Configuration](./HTTP-HTTPS-CONFIGURATION.md)** - Secure with proper TLS certificates
3. **[Probes Configuration](./PROBES-CONFIGURATION.md)** - Add additional monitoring capabilities
4. **[Metrics Usage](./METRICS-USAGE.md)** - Integrate with PRTG, Nagios, or Grafana

**For complex integration issues:** See [Metrics Usage - Troubleshooting](./METRICS-USAGE.md#troubleshooting-integration-issues)

**For security hardening:** See [HTTP/HTTPS Configuration - Security](./HTTP-HTTPS-CONFIGURATION.md#security-considerations)

**Documentation:**
- **Installation**: [INSTALLATION.md](./INSTALLATION.md)
- **Configuration**: [AGENT-CONFIGURATION.md](./AGENT-CONFIGURATION.md)
- **Web Interface**: [WEB-INTERFACE.md](./WEB-INTERFACE.md)
