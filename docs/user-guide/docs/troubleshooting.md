# Troubleshooting

## Diagnosing in One Pass

Before reading logs, run `senhub-agent doctor`. It checks the install, the configuration, the outputs, the probes and the host, and prints the command that fixes each problem it finds. It needs no root and works with the service stopped. See [Doctor](cli.md#doctor).

## Checking Agent Status

Use the built-in status command to get a comprehensive overview of the agent:

**Windows (PowerShell as Administrator):**
```powershell
.\senhub-agent.exe status
```

**Linux:**
```bash
sudo /usr/local/bin/senhub-agent status
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
Get-Service senhub-agent
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
  "version": "0.6.0",
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
  "version": "0.6.0",
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
    "probe_count": 7,
    "ttl": "5m0s"
  },
  "resources": {
    "memory_usage_mb": 104.8,
    "heap_mb": 11.2,
    "cpu_percent": 0.4,
    "measured": true,
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

The log file is human-readable text: date, level, the sentence, then the
structured fields.

```
2026-08-13 11:12:58.102 INF Successfully sent datapoints module=data_store count=116 strategy=http
2026-08-13 11:13:04.551 ERR Connection refused module=probe.netscaler error="dial tcp 192.168.1.100:443: connect: connection refused"
```

Each line carries:

| Part | Description |
|-------|-------------|
| date and time | Local time with milliseconds |
| level | `TRC`, `DBG`, `INF`, `WRN` or `ERR` |
| message | Human-readable description |
| `module=` | Component that produced the line (for example `probe.citrix`, `strategy.http`, `cache`) |
| `error=` | Error details (only on error lines) |

Run the agent with `--log-format json` (or set `SENHUB_LOG_FORMAT=json`) to
write the same entries as one JSON object per line, for shipping the file to
an aggregator. The remote log shipper always sends JSON, whatever this
setting says.

### Filtering Logs

To find errors:

**Linux:**
```bash
grep ' ERR ' /var/log/senhub-agent/senhubagent.log | tail -20
```

**Windows (PowerShell):**
```powershell
Select-String -Path "C:\ProgramData\SenHub\logs\senhubagent.log" -Pattern ' ERR ' | Select-Object -Last 20
```

To find logs for a specific probe:

```bash
grep 'module=probe.citrix' /var/log/senhub-agent/senhubagent.log | tail -20
```

## Enabling Debug Logging

### Where Debug Lines Go

Debug lines go to the same places as every other line. Neither `--verbose`,
`--filter` nor the runtime per-module level opens a separate channel; they
only decide which lines are let through. What changes with how the agent is
started is where you read them.

| How the agent runs | Where the lines are written |
|---|---|
| Linux service (systemd) | The log file `/var/log/senhub-agent/senhubagent.log`, and the journal (`journalctl -u senhub-agent`) |
| Windows service | The log file `C:\ProgramData\SenHub\logs\senhubagent.log` only |
| Interactive `run` (Linux, Windows) | The console (standard error) and a log file of its own, `senhubagent-console.log`, in the same directory as the service file |
| Container (`run`, the image default) | The container's standard error (`docker logs`, `kubectl logs`), and the `-console` log file when the log directory is writable by the container user |

Details worth knowing:

- An interactive run never shares the service's file, so reading
  `senhubagent.log` while you run `senhub-agent run --filter ...` in a
  terminal shows nothing new. Read the console, or `senhubagent-console.log`.
- An agent started with another `--config-path` than the installed one
  writes to its own file, `senhubagent-<8 hex digits>.log`, in the same
  directory.
- If the log directory cannot be written, the file is created next to the
  agent binary instead (the agent says which path it uses when it starts).
- `--verbose` without `--filter` lets every module's debug lines through;
  `--filter probe.citrix` lets through only the modules whose name starts
  with that prefix. Both apply from the start of the process. The runtime
  per-module level (next section) needs no restart and applies to the
  running agent, whichever way it was started.
- To make a service write debug lines from its start, install it with the
  flag (`senhub-agent install --filter probe.citrix`); otherwise raise the level
  at runtime.
- The file is text by default, and debug lines read `DBG`. With
  `--log-format json` they carry `"level":"debug"` instead.

### Runtime Debug (No Restart Required)

You can enable debug logging for specific modules at runtime via the API. This is the recommended approach as it does not require restarting the service.

The `debug/logs` and `admin/cache/clear` routes belong to the administration surface: they answer only the administration key, written `{admin-key}` below, and refuse the agent key PRTG or Nagios read with. The administration key is the segment after `/web/` in the address `sudo /usr/local/bin/senhub-agent console --print` prints (on Windows, `senhub-agent.exe console --print` from an elevated prompt).

```bash
curl -X POST http://localhost:8080/api/{admin-key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [{"module": "probe.citrix", "level": "debug"}]}'
```

You can enable debug for multiple modules at once:

```bash
curl -X POST http://localhost:8080/api/{admin-key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [
    {"module": "probe.citrix", "level": "debug"},
    {"module": "probe.netscaler", "level": "debug"},
    {"module": "strategy.http", "level": "debug"}
  ]}'
```

To check current log levels for all modules:

```bash
curl http://localhost:8080/api/{admin-key}/debug/logs
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
curl -X POST http://localhost:8080/api/{admin-key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [{"module": "probe.citrix", "level": "info"}]}'
```

### Raising One Module to Debug and Reading It

A module raised to `debug` writes its debug lines to the normal agent log
while every other module stays at `info`. Take IBM i as the example:

```bash
curl -X POST http://localhost:8080/api/{admin-key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [{"module": "probe.ibmi", "level": "debug"}]}'
```

Then read only that module's debug lines from the log file (see
[Viewing Logs](#viewing-logs) for its location):

```bash
grep ' DBG ' /var/log/senhub-agent/senhubagent.log | grep 'module=probe.ibmi'
```

Each line carries `module=probe.ibmi` (`"module":"probe.ibmi"` in JSON, where
the level is `"level":"debug"` instead of `DBG`). Where the lines land
depends on how the agent runs: see [Where Debug Lines Go](#where-debug-lines-go). The
debug lines appear from the next collection cycle; nothing needs restarting.

The setting lives in the agent's memory only:

- It survives a configuration reload and a probe restart.
- It does **not** survive a restart of the agent or of the service: the module
  is back at `info`. Raise it again afterwards.
- Set the module back to `info` when you are done, as shown above: debug
  output is verbose and grows the log quickly.

Before this was fixed (0.6.2), the call answered `200` and the level showed
as `debug` in the `GET` response, but the agent still wrote no debug line
for that module.

### Available Debug Modules

The agent prints its own list, one line per probe type it carries plus the
agent's own modules:

```bash
sudo /usr/local/bin/senhub-agent debug-modules-list
```

Use that rather than a list written here: the probe entries are read from
the registry, so they follow the build you are running, and a Pro binary
shows the types a Free one does not.

A filter matches by prefix, so `probe` selects every probe and
`probe.postgresql` selects one. The agent's own modules are `sensor`
(probe lifecycle), `configuration`, `strategy` and `strategy.http`,
`transformer`, `data_store` and `service.auto_update`.

Every log line names the module it came from in its `module=` field, so
the fastest way to find the filter for something you are reading is to
look at the line itself.

### Console Debug Mode

For troubleshooting startup issues or when the API is not available, run the agent interactively in verbose mode:

**All modules (verbose):**
```bash
sudo /usr/local/bin/senhub-agent run --verbose
```

**Specific modules only:**
```bash
sudo /usr/local/bin/senhub-agent run --filter probe.citrix,strategy.http
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
| Invalid config_version | Check logs for "config_version" errors | The agent manages `config_version` automatically. Leave it as written or let migration set it; do not hand-edit to a version newer than your agent supports (an agent refuses a config version above the one it ships). Run `agent config check` to confirm. |

### Configuration Changes Not Applied

**Symptom:** Changes to the configuration (`agent.yaml`, `probes.d/`, `strategies.d/`) are not reflected in agent behavior.

**Possible causes and solutions:**

- **YAML syntax error in the modified section**: The agent keeps the previous valid configuration when it encounters a syntax error. Check logs for configuration reload errors.
- **Config file path mismatch**: Verify the agent is monitoring the correct file. Check with `sudo /usr/local/bin/senhub-agent status` or logs.
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
| License restriction | Logs show "license" or "unauthorized probe" | Check license: `sudo /usr/local/bin/senhub-agent license show` |
| Missing required params | Logs show "missing parameter" | Check the probe guide for required parameters |
| DNS resolution | Logs show "no such host" | Verify DNS from the agent host: `nslookup target-server` |

Enable debug logging for the specific probe module to get detailed error information:

```bash
curl -X POST http://localhost:8080/api/{admin-key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [{"module": "probe.citrix", "level": "debug"}]}'
```

### API Not Accessible

**Symptom:** `curl http://localhost:8080/health` returns "Connection refused" or times out.

**Possible causes and solutions:**

- **Service not running**: Check with `sudo /usr/local/bin/senhub-agent status` and start if needed
- **Firewall blocking the port**: See Firewall Configuration in the [HTTP/HTTPS section](http-https.md)
- **Agent bound to a different interface**: Check `bind_address` in the `http` output (`strategies.d/`). If set to `127.0.0.1`, the API is only accessible from localhost
- **Different port configured**: Check the `port` of the `http` output in `strategies.d/`, or run `sudo /usr/local/bin/senhub-agent config show`
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

- **No license activated**: Check with `sudo /usr/local/bin/senhub-agent license show`. Without a license the agent runs every Free-tier probe — the whole universal collection tier (OS/host, logs, network checks, application, database and broker probes); only the Pro probes need a license. Each page of the [probe catalog](probes/index.md) shows the tier badge.
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
- **Missing lookups**: Install PRTG Lookups for status metrics (see [Web console](web-interface.md))

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
curl -X POST http://localhost:8080/api/{admin-key}/admin/cache/clear
```

This route answers only the administration key (see [Runtime Debug](#runtime-debug-no-restart-required)).

## Diagnostic Checklist

When troubleshooting, follow these steps in order:

1. **Service running?** `sudo /usr/local/bin/senhub-agent status`
2. **Health OK?** `curl http://localhost:8080/health`
3. **Probes active?** `curl http://localhost:8080/api/{key}/info/probes`
4. **Errors in logs?** Check the last 50 lines of the log file for errors
5. **Configuration valid?** `sudo /usr/local/bin/senhub-agent config check`
6. **Network reachable?** Test connectivity from the agent host to target systems
7. **License active?** `sudo /usr/local/bin/senhub-agent license show`
8. **Enable debug** for the relevant module and check detailed logs

## Getting Support

If you cannot resolve the issue:

- **Email:** support@senhub.io
- **Include in your support request:**
  - Agent version (`senhub-agent version`)
  - Operating system and version
  - Relevant log entries (last 50 lines with errors)
  - The output of `sudo /usr/local/bin/senhub-agent config show`, which masks secret values
  - The output of `sudo /usr/local/bin/senhub-agent status`
