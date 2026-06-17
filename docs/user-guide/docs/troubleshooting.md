# Troubleshooting

## Checking Agent Status

Use the built-in status command to get a comprehensive overview of the agent:

**Windows (PowerShell as Administrator):**
```powershell
.\senhub-agent.exe status
```

**Linux:**
```bash
sudo /opt/senhub/bin/senhub-agent status
```

The status command displays:

- Service status (Running / Stopped)
- Agent version and build information
- Health status (healthy / degraded / unhealthy)
- Memory and CPU usage
- Number of active probes and cached metrics
- Uptime

You can also check the service directly using system commands:

**Windows:**
```powershell
Get-Service "SenHub Agent"
```

**Linux:**
```bash
sudo systemctl status senhub-agent
```

## Verifying Agent Health

### Health Endpoint

The health endpoint is the quickest way to verify the agent is running and responding:

```bash
curl http://localhost:8080/health
```

Expected response:
```json
{
  "status": "ok",
  "version": "0.1.80",
  "uptime": "2h30m15s",
  "probes_active": 4,
  "metrics_cached": 156
}
```

If the agent does not respond, check that the service is running and the port is not blocked.

### System Information

For detailed system information (requires authentication key):

```bash
curl http://localhost:8080/api/{key}/info/system
```

Response:
```json
{
  "status": "running",
  "version": "0.1.80",
  "os": "linux",
  "arch": "amd64",
  "port": 8080,
  "uptime": "2h30m15s",
  "health": {
    "status": "healthy",
    "services": {
      "http_server": "running",
      "cache": "running",
      "mode": "offline"
    }
  },
  "cache": {
    "total_metrics": 156,
    "ttl": "5m0s",
    "memory_usage": "2.45 MB"
  },
  "resources": {
    "memory_usage_mb": 45.67,
    "cpu_percent": 2.5,
    "goroutines": 42
  }
}
```

### Probes Status

To check which probes are active and how many metrics each is collecting:

```bash
curl http://localhost:8080/api/{key}/info/probes
```

Response:
```json
{
  "probes": ["CPU", "Memory", "Citrix Production"],
  "probe_metrics": {
    "CPU": 4,
    "Memory": 3,
    "Citrix Production": 85
  },
  "total_metrics": 92
}
```

If a probe shows 0 metrics, it may be encountering errors. Check the logs for details.

## Viewing Logs

Agent logs are stored in dedicated directories:

**Windows:**
```
%ProgramData%\SenHub\logs\senhubagent.log
```
Typically: `C:\ProgramData\SenHub\logs\senhubagent.log`

**Linux:**
```
/var/log/senhub-agent/senhubagent.log
```

If the Linux log directory is not writable, logs are written to the agent binary directory.

**Log rotation:** 10 MB maximum per file, 5 backup files, 30-day retention, compressed. Old log files are named `senhubagent-YYYY-MM-DD.log.gz`.

### Viewing Recent Logs

**Windows (PowerShell):**
```powershell
Get-Content "C:\ProgramData\SenHub\logs\senhubagent.log" -Tail 100
```

To follow logs in real-time:
```powershell
Get-Content "C:\ProgramData\SenHub\logs\senhubagent.log" -Tail 50 -Wait
```

**Linux:**
```bash
tail -100 /var/log/senhub-agent/senhubagent.log
```

To follow logs in real-time:
```bash
tail -f /var/log/senhub-agent/senhubagent.log
```

You can also view logs via systemd journal on Linux:
```bash
sudo journalctl -u senhub-agent -n 100
```

### Understanding Log Format

Log entries are structured in JSON format:

```json
{"level":"info","module":"probe.citrix","time":"2025-03-03T10:30:00Z","message":"Collection completed: 85 metrics"}
{"level":"error","module":"probe.netscaler","time":"2025-03-03T10:30:05Z","message":"Connection refused","error":"dial tcp 192.168.1.100:443: connect: connection refused"}
```

Key fields:

| Field | Description |
|-------|-------------|
| `level` | Log severity: `debug`, `info`, `warn`, `error` |
| `module` | Component that generated the log (e.g., `probe.citrix`, `strategy.http`, `cache`) |
| `time` | Timestamp in ISO 8601 format |
| `message` | Human-readable description |
| `error` | Error details (only present for error-level logs) |

### Filtering Logs

To find errors:

**Linux:**
```bash
grep '"level":"error"' /var/log/senhub-agent/senhubagent.log | tail -20
```

**Windows (PowerShell):**
```powershell
Select-String -Path "C:\ProgramData\SenHub\logs\senhubagent.log" -Pattern '"level":"error"' | Select-Object -Last 20
```

To find logs for a specific probe:

```bash
grep '"module":"probe.citrix"' /var/log/senhub-agent/senhubagent.log | tail -20
```

## Enabling Debug Logging

### Runtime Debug (No Restart Required)

You can enable debug logging for specific modules at runtime via the API. This is the recommended approach as it does not require restarting the service:

```bash
curl -X POST http://localhost:8080/api/{key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [{"module": "probe.citrix", "level": "debug"}]}'
```

You can enable debug for multiple modules at once:

```bash
curl -X POST http://localhost:8080/api/{key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [
    {"module": "probe.citrix", "level": "debug"},
    {"module": "probe.netscaler", "level": "debug"},
    {"module": "strategy.http", "level": "debug"}
  ]}'
```

To check current log levels for all modules:

```bash
curl http://localhost:8080/api/{key}/debug/logs
```

Response:
```json
{
  "module_levels": [
    {"module": "strategy.http", "level": "info"},
    {"module": "probe.citrix", "level": "debug"},
    {"module": "probe.netscaler", "level": "debug"},
    {"module": "cache", "level": "info"}
  ]
}
```

To revert a module to normal logging:

```bash
curl -X POST http://localhost:8080/api/{key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [{"module": "probe.citrix", "level": "info"}]}'
```

### Available Debug Modules

| Module | Description |
|--------|-------------|
| `probe.cpu` | CPU probe collection |
| `probe.memory` | Memory probe collection |
| `probe.network` | Network probe collection |
| `probe.logicaldisk` | Disk probe collection |
| `probe.citrix` | Citrix probe |
| `probe.netscaler` | NetScaler ADC probe |
| `probe.redfish` | Redfish hardware probe |
| `probe.syslog` | Syslog collection |
| `strategy.http` | HTTP API and web interface |
| `strategy.prtg` | PRTG data formatting |
| `strategy.senhub` | SenHub platform sync |
| `cache` | Metrics cache operations |
| `config` | Configuration loading and reload |

### Console Debug Mode

For troubleshooting startup issues or when the API is not available, run the agent interactively in verbose mode:

**All modules (verbose):**
```bash
senhub-agent run --verbose
```

**Specific modules only:**
```bash
senhub-agent run --verbose --debug-modules probe.citrix,strategy.http
```

This runs the agent in the foreground (not as a service) and outputs detailed logs to the console. Press Ctrl+C to stop. This is useful for:

- Diagnosing startup failures
- Verifying credentials and connectivity to target systems
- Debugging probe collection in detail
- Identifying configuration issues

## Common Problems

### Agent Does Not Start

**Symptom:** The service fails to start or exits immediately after starting.

**Possible causes and solutions:**

| Cause | How to Diagnose | Solution |
|-------|-----------------|----------|
| Invalid YAML syntax | Check logs for "yaml" errors | Validate: `python3 -c "import yaml; yaml.safe_load(open('agent.yaml'))"` |
| Port already in use | Check logs for "bind" or "address already in use" | Linux: `ss -tlnp \| grep 8080` / Windows: `netstat -an \| findstr 8080` |
| Insufficient permissions | Check logs for "permission denied" | Run as Administrator (Windows) or root (Linux) |
| Invalid config_version | Check logs for "config_version" errors | Ensure `config_version: 2` is at the top of the config file |

### Configuration Changes Not Applied

**Symptom:** Changes to `agent-config.yaml` are not reflected in agent behavior.

**Possible causes and solutions:**

- **YAML syntax error in the modified section**: The agent keeps the previous valid configuration when it encounters a syntax error. Check logs for configuration reload errors.
- **Config file path mismatch**: Verify the agent is monitoring the correct file. Check with `senhub-agent status` or logs.
- **File not saved**: Ensure the editor saved the file (some editors use temporary files).

The agent detects file changes automatically within a few seconds. No restart is required.

### Probe Not Collecting Metrics

**Symptom:** A probe is configured but returns 0 metrics or does not appear in the probes list.

**Possible causes and solutions:**

| Cause | How to Diagnose | Solution |
|-------|-----------------|----------|
| Invalid credentials | Logs show "401" or "authentication failed" | Verify username/password in the probe config |
| Network connectivity | Logs show "connection refused" or "timeout" | Verify: `curl -k https://target-server/` from the agent host |
| SSL/TLS certificate | Logs show "certificate" errors | Set `tls.verify_ssl: false` in the probe config for testing |
| License restriction | Logs show "license" or "unauthorized probe" | Check license: `senhub-agent license show` |
| Missing required params | Logs show "missing parameter" | Check the probe guide for required parameters |
| DNS resolution | Logs show "no such host" | Verify DNS from the agent host: `nslookup target-server` |

Enable debug logging for the specific probe module to get detailed error information:

```bash
curl -X POST http://localhost:8080/api/{key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [{"module": "probe.citrix", "level": "debug"}]}'
```

### API Not Accessible

**Symptom:** `curl http://localhost:8080/health` returns "Connection refused" or times out.

**Possible causes and solutions:**

- **Service not running**: Check with `senhub-agent status` and start if needed
- **Firewall blocking the port**: See Firewall Configuration in the [HTTP/HTTPS section](http-https.md)
- **Agent bound to a different interface**: Check `bind_address` in the storage config. If set to `127.0.0.1`, the API is only accessible from localhost
- **Different port configured**: Check the `port` in the storage config section of `agent-config.yaml`
- **HTTPS enabled**: If HTTPS is enabled, use `https://` instead of `http://`

### TLS / HTTPS Errors

**Symptom:** HTTPS connections fail with certificate errors or TLS handshake failures.

**Possible causes and solutions:**

| Cause | Solution |
|-------|----------|
| Self-signed certificate not trusted | Use `-k` flag (curl) or disable verification in your monitoring tool |
| Certificate expired (older than 365 days) | Regenerate: reinstall with `--enable-https` |
| Hostname mismatch | Regenerate with correct hostnames: `--https-hosts "correct-hostname"` |
| TLS version mismatch | Check `tls.min_tls_version` in config (default: 1.2) |

### License Errors

**Symptom:** A probe type is rejected with a license error, or premium probes are not available.

**Possible causes and solutions:**

- **No license activated**: Check with `senhub-agent license show`. Free tier only allows cpu, memory, logicaldisk, network.
- **License expired**: Check the expiration date. There is a 7-day grace period after expiration. Contact support for renewal.
- **Probe not in license tier**: Verify the probe type is included in your tier. See the License Tiers table in the [Configuration section](configuration.md).

Check the full license status via the API:

```bash
curl http://localhost:8080/api/{key}/license/status
```

Response for an expired license:
```json
{
  "status": "grace_period",
  "tier": "pro",
  "expires_at": "2026-01-01T00:00:00Z",
  "days_remaining": 5,
  "message": "License expired but in grace period (5 days remaining)"
}
```

### PRTG Sensor Shows No Data

**Symptom:** PRTG sensor is configured but shows "No data" or errors.

**Possible causes and solutions:**

- **Wrong sensor type**: Use **HTTP Data Advanced** (not HTTP XML/REST Value or other types)
- **Wrong URL**: Verify the URL format: `/api/{key}/prtg/metrics/{probe-name}`
- **Probe name mismatch**: Check available probe names with `curl http://localhost:8080/api/{key}/prtg/probes`
- **Authentication key invalid**: Verify the key in the URL matches the agent's key
- **Agent not reachable from PRTG server**: Test with `curl` from the PRTG server itself
- **Missing lookups**: Install PRTG Lookups for status metrics (see [Web Interface section](web-interface.md))

### High Memory Usage

**Symptom:** The agent uses more memory than expected.

**Possible causes and solutions:**

- **Too many probes**: Each probe maintains its own cache. Reduce the number of probes or increase the collection interval.
- **Cache retention too long**: Reduce `cache.retention_minutes` in the configuration.
- **Debug logging enabled**: Debug logging generates more data. Disable debug logging when not needed.

Check current memory usage:
```bash
curl http://localhost:8080/api/{key}/info/system
```

Clear the cache if needed:
```bash
curl -X POST http://localhost:8080/api/{key}/admin/cache/clear
```

## Diagnostic Checklist

When troubleshooting, follow these steps in order:

1. **Service running?** `senhub-agent status`
2. **Health OK?** `curl http://localhost:8080/health`
3. **Probes active?** `curl http://localhost:8080/api/{key}/info/probes`
4. **Errors in logs?** Check the last 50 lines of the log file for errors
5. **Configuration valid?** Validate YAML syntax
6. **Network reachable?** Test connectivity from the agent host to target systems
7. **License active?** `senhub-agent license show`
8. **Enable debug** for the relevant module and check detailed logs

## Getting Support

If you cannot resolve the issue:

- **Email:** support@senhub.io
- **Include in your support request:**
  - Agent version (`senhub-agent version`)
  - Operating system and version
  - Relevant log entries (last 50 lines with errors)
  - Your `agent-config.yaml` (with passwords removed)
  - The output of `senhub-agent status`
