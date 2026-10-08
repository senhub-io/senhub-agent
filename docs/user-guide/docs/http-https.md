# HTTP/HTTPS Configuration

SenHub Agent exposes a REST API that monitoring systems (PRTG and Nagios) and administrators use to collect metrics, check agent health, and manage configuration. By default, the agent listens on HTTP port 8080.

## HTTP Mode (Default)

When you install the agent, the HTTP API is available immediately on port 8080 with no additional configuration:

```
http://agent-server:8080/api/{authentication-key}/
```

The installer writes the HTTP output to `strategies.d/00-http.yaml`:

```yaml
http:
  port: 8080
  bind_address: "127.0.0.1"
  endpoints: ["prtg", "web", "nagios", "prometheus"]
  admin_key: "..."
```

Without `--enable-https`, the installer binds to loopback. With `--enable-https`, it writes `bind_address: "0.0.0.0"` and the HTTPS port. A remote poller on a plain HTTP installation needs an explicit `bind_address: "0.0.0.0"` or an interface IP.

An installation that still uses the legacy monolithic `agent-config.yaml` holds the same parameters under `storage:`, in the `params` of the entry named `http`.

### Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `port` | `8080` | TCP port for the HTTP API |
| `bind_address` | `127.0.0.1` | Network interface to bind to. Loopback by default — remote pollers (PRTG, Prometheus) require an explicit `"0.0.0.0"` or interface IP |
| `endpoints` | none | Enabled endpoint types (`prtg`, `nagios`, `prometheus`, `web`). There is no default: an endpoint answers only when it is listed. The installer writes `["prtg", "web", "nagios", "prometheus"]` |
| `admin_key` | generated | Administration key. It opens the web console, the configuration API, the log levels, the cache clear and the profiler; these routes are not served at all when it is absent. The installer writes one, and an older installation without it gets one generated on its first start. It is not the agent key: the agent key only reads metrics |

To change the port or other parameters, edit `strategies.d/00-http.yaml`. The change is applied automatically without restarting the service.

## HTTPS Mode

For production environments, enable HTTPS to encrypt API traffic between the agent and your monitoring system.

### Enabling HTTPS During Installation

The simplest way to enable HTTPS is during installation:

**Windows:**
```powershell
.\senhub-agent.exe install --enable-https
```

**Linux:**
```bash
sudo /opt/senhub/bin/senhub-agent install --enable-https
```

The agent automatically generates a self-signed certificate valid for 365 days and saves it in a `certs/` subdirectory:

- `certs/agent-cert.pem` (certificate)
- `certs/agent-key.pem` (private key)

The default HTTPS port is 8443.

### HTTPS Installation Options

| Flag | Default | Description |
|------|---------|-------------|
| `--enable-https` | - | Enable HTTPS for the agent API |
| `--https-port` | `8443` | HTTPS listening port |
| `--https-hosts` | `localhost,127.0.0.1` | Hostnames included in the certificate SAN (comma-separated) |
| `--cert-file` | auto-generated | Path to a custom TLS certificate (skips auto-generation) |
| `--key-file` | auto-generated | Path to a custom TLS private key (skips auto-generation) |
| `--min-tls-version` | `1.2` | Minimum TLS version (1.2 or 1.3) |

Example with custom hostnames for the certificate:

```bash
sudo /opt/senhub/bin/senhub-agent install \
  --enable-https \
  --https-hosts "agent.company.com,192.168.1.100" \
  --https-port 8443
```

The generated certificate will include `agent.company.com` and `192.168.1.100` as Subject Alternative Names, so your monitoring system can connect using either the hostname or the IP address without certificate warnings.

### Manual HTTPS Configuration

You can also configure HTTPS directly in `strategies.d/00-http.yaml`:

```yaml
http:
  port: 8443
  bind_address: "0.0.0.0"
  endpoints: ["prtg", "web", "nagios", "prometheus"]
  tls:
    enabled: true
    min_tls_version: "1.2"
    cert_file: "/opt/senhub/certs/agent-cert.pem"
    key_file: "/opt/senhub/certs/agent-key.pem"
```

### TLS Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `tls.enabled` | `false` | Enable HTTPS |
| `tls.min_tls_version` | `1.2` | Minimum TLS version (1.2 or 1.3) |
| `tls.cert_file` | generated | Absolute path to the certificate file (.pem or .crt) |
| `tls.key_file` | generated | Absolute path to the private key file (.pem or .key) |

### If you set neither

Enabling `tls` without naming a certificate is a valid configuration. On
first start the agent generates a self-signed pair next to its
configuration, at `<config directory>/certs/agent-cert.pem` and
`agent-key.pem`, readable only by the service user, and says so in the
log:

```
INF HTTPS server listening ... self_signed=true
INF Using a self-signed certificate the agent manages; replace these
    files with your own to be trusted by a browser
```

The pair is generated once. Replacing those two files with your own
keeps them: the agent regenerates nothing that is already there, so an
upgrade or a restart will not undo your certificate.

Naming a `cert_file` that does not exist is a different case, and the
agent does not generate anything for it. It refuses to start the HTTPS
listener and names the file and the directory it looked in, because a
path you wrote and that is not there is more likely a typo than an
invitation:

```
ERR TLS is enabled but the certificate file is missing; the HTTPS
    listener is not started. Set tls.cert_file and tls.key_file to
    absolute paths, or disable tls.
```

### Using a CA-Signed Certificate

If you have a certificate issued by your internal Certificate Authority:

```yaml
http:
  port: 8443
  tls:
    enabled: true
    cert_file: "/etc/pki/tls/certs/senhub-agent.crt"
    key_file: "/etc/pki/tls/private/senhub-agent.key"
```

### Verifying HTTPS

```bash
curl -k https://agent-server:8443/health
```

The `-k` flag is required for self-signed certificates. For CA-signed certificates, omit this flag.

Expected response:
```json
{"status":"ok","timestamp":"2026-09-29T10:15:00+02:00","memory_mb":104.8,"version":"0.6.0"}
```

The **HTTPS** card of the console's [Settings](web-interface.md#settings) page shows the same state without a shell: whether TLS is on, the certificate and key files, the certificate's subject and expiry read from the file, and the minimum TLS version.

## API Endpoints Reference

All API endpoints require a key in the URL path, except `/health`. Two keys exist:

- The **agent key**, printed by `senhub-agent key show`, reads: metrics, discovery, cache statistics, licence status. It is the key given to PRTG, Nagios or a Prometheus scrape.
- The **administration key**, `admin_key` on the `http` output, changes the agent. It is the only key accepted on the routes marked `admin` below, and it also opens every route the agent key opens. The `admin` routes are not registered when `admin_key` is absent, and answer 404.

### Health and System Information

| Endpoint | Method | Key | Description |
|----------|--------|------|-------------|
| `/health` | GET | none | Agent health check with basic status |
| `/api/{key}/info/system` | GET | agent | Detailed system information (version, uptime, memory, CPU) |
| `/api/{key}/info/probes` | GET | agent | List of configured probes and metric counts |
| `/api/{key}/info/endpoints` | GET | agent | List of available API endpoints |
| `/api/{key}/info/tags/{probe}` | GET | agent | Available tag values for a probe (useful for discovery) |
| `/api/{key}/info/schema/{probe}` | GET | agent | Full metric schema for a probe with examples |

### Metrics Endpoints (Monitoring System Integration)

| Endpoint | Method | Key | Description |
|----------|--------|------|-------------|
| `/api/{key}/prtg/metrics/{probe}` | GET | agent | Metrics in PRTG JSON format |
| `/api/{key}/prtg/probes` | GET | agent | List of available PRTG probe names |
| `/api/{key}/nagios/metrics/{probe}` | GET | agent | Probe summary in Nagios plugin output format |
| `/api/{key}/nagios/check/{check}` | GET | agent | One configured check in Nagios plugin output format |
| `/api/{key}/nagios/metrics` | GET, POST | agent | Every configured check, or one (POST), as JSON |
| `/api/{key}/nagios/checks` | GET | agent | List of configured Nagios checks |

### Configuration and Administration

| Endpoint | Method | Key | Description |
|----------|--------|------|-------------|
| `/api/{key}/config/probes` | GET | admin | Active probe configuration |
| `/api/{key}/config/validate` | POST | admin | Validate a probe configuration (dry run) |
| `/api/{key}/config/test` | POST | admin | Test probe connectivity to a target system |
| `/api/{key}/license/status` | GET | agent | License details, tier, and authorized probes |
| `/api/{key}/admin/cache/clear` | POST | admin | Clear the metrics cache |
| `/api/{key}/stats/cache` | GET | agent | Cache statistics (size, memory usage) |

### Debug Endpoints

| Endpoint | Method | Key | Description |
|----------|--------|------|-------------|
| `/api/{key}/debug/logs` | GET | admin | Current log levels per module |
| `/api/{key}/debug/logs` | POST | admin | Change log levels at runtime (no restart) |
| `/api/{key}/debug/cache` | GET | agent | Current cache contents |
| `/api/{key}/debug/pprof/` | GET | admin | Go runtime profiler (index, `profile`, `trace`, `heap`, `goroutine` and the other named profiles) |

### Web Interface

The console answers the administration key only, and is served when both the `web` endpoint and `admin_key` are set.

| Endpoint | Description |
|----------|-------------|
| `/web/{key}/` | Overview |
| `/web/{key}/overview` | Overview (same as above; `/dashboard` still works) |
| `/web/{key}/probes` | Probes list and editor |
| `/web/{key}/outputs` | Outputs list; `/outputs/{name}` edits one, `/outputs/http` holds the Sensor URLs tab |
| `/web/{key}/settings` | Port, bind address, licence |
| `/web/{key}/docs` | Embedded API reference |
| `/web/{key}/explorer` | Former Sensor Builder; redirects to `/outputs/http#urls` |

### PRTG Lookups

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/{key}/lookups` | GET | List all available lookup definitions |
| `/api/{key}/lookups/prtg` | GET | Download all PRTG lookups as a ZIP file |
| `/api/{key}/lookups/prtg/{lookup_id}` | GET | Download a specific lookup as XML |

## API Response Examples

### Health Response

```bash
curl http://localhost:8080/health
```

```json
{
  "status": "ok",
  "timestamp": "2026-09-29T10:15:00+02:00",
  "memory_mb": 104.8,
  "version": "0.6.0"
}
```

`/health` takes no key. `memory_mb` is the agent's resident memory, as the operating system counts it; `version` is the agent release.

### Probes Information

```bash
curl http://localhost:8080/api/{key}/info/probes
```

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

### System Information

```bash
curl http://localhost:8080/api/{key}/info/system
```

```json
{
  "status": "running",
  "version": "x.y.z",
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

`resources.memory_usage_mb` is the resident set the operating system charges to the agent (the working set on Windows) and `heap_mb` the part the Go heap holds; `cpu_percent` is the agent's CPU time over the last interval as a share of the whole machine. `measured` is false when the operating system could not be asked: memory is then the Go heap and CPU is not known.

### PRTG Metrics Response

```bash
curl http://localhost:8080/api/{key}/prtg/metrics/CPU
```

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
        "channel": "CPU Queue Length",
        "value": 2,
        "unit": "Count"
      }
    ]
  }
}
```

For status-type metrics, PRTG lookups are used instead of numeric values:

```json
{
  "prtg": {
    "result": [
      {
        "channel": "Service State",
        "value": 1,
        "valuelookup": "senhub.netscaler.lbvserver.state"
      }
    ]
  }
}
```

### PRTG Metrics with Tag Filtering

You can filter PRTG metrics by tag values using query parameters:

```bash
curl "http://localhost:8080/api/{key}/prtg/metrics/NetScaler%20LB?tags=vserver_name:vs_web,vs_api"
```

This returns metrics only for the specified virtual servers.

### PRTG Speed Units

PRTG fixes a channel's unit when the sensor creates the channel and does not change it afterwards. The HTTP Data Advanced sensor also ignores `speedsize` and `speedtime`: it reads every `SpeedNet` and `SpeedDisk` value as bytes per second. The agent therefore never sends those two fields, and offers two modes for rates expressed in bits.

| Mode | Request | Bit-based rate channel | Value |
|---|---|---|---|
| Default | `.../prtg/metrics/{probe}` | `"unit": "Custom"`, `"customunit": "Mbit/s"` (or `kbit/s`, `bit/s`, `Gbit/s`, matching the value's scale) | unchanged |
| Native | `.../prtg/metrics/{probe}?speed=native` | `"unit": "SpeedNet"` | converted to bytes per second (Mbit/s x 125000, kbit/s x 125, bit/s / 8), so PRTG scales it (kbit/s, Mbit/s, Gbit/s) on its own |

Byte-based rates (`SpeedNet`, or `SpeedDisk` for disks) carry the byte value in both modes. Any other `speed` value is ignored and gives the default mode. The parameter is accepted on the GET and POST PRTG metrics routes.

Default mode keeps the raw value, which is what channels created by agents 0.1.x expect. Native mode suits a new sensor where you want PRTG's automatic scaling; choose it before the sensor is created, since changing the URL of an existing sensor does not change its channels' units.

Example, a NetScaler interface receiving 1588 Mbit/s:

```json
{"channel": "Interface RX Rate (1/1)", "value": 1588, "float": 1, "unit": "Custom", "customunit": "Mbit/s"}
```

and with `?speed=native`:

```json
{"channel": "Interface RX Rate (1/1)", "value": 198500000, "float": 1, "unit": "SpeedNet"}
```

!!! warning "Upgrading from 0.4.0 to 0.6.1"
    Agents 0.4.0 to 0.6.1 sent bit rates as `SpeedNet`, so PRTG created those channels with a wrong scale (for example 1588 Mbit/s shown as 0.01 Mbit/s). After upgrading, delete and recreate the affected channels, or the whole sensor, in either mode: the unit of an existing channel is not updated.

### Nagios Response

```bash
curl http://localhost:8080/api/{key}/nagios/check/cpu_detailed
```

```
OK - cpu_usage_total: OK 12.00%, cpu_system: OK 5.00%, cpu_user: OK 10.00% | cpu_usage_total=12.00%;80;90;0;100 cpu_system=5.00%;30;50;0;100 cpu_user=10.00%;70;85;0;100
```

The response follows the Nagios plugin output format: `STATUS - message | performance_data`.
The [Nagios page](nagios.md) covers the probe summary, the shipped checks,
writing your own and the command to declare in Nagios.

## Firewall Configuration

The agent port must be accessible from your monitoring system.

**Windows:**
```powershell
netsh advfirewall firewall add rule name="SenHub Agent" dir=in action=allow protocol=TCP localport=8080
```

**Linux (Ubuntu/Debian):**
```bash
sudo ufw allow 8080/tcp
```

**Linux (RHEL/CentOS):**
```bash
sudo firewall-cmd --permanent --add-port=8080/tcp
sudo firewall-cmd --reload
```

For HTTPS, replace `8080` with your configured HTTPS port (default: `8443`).

## Security Recommendations

- Use HTTPS in production environments
- Bind the agent to a specific interface (`bind_address: "127.0.0.1"`) when remote access is not needed
- Restrict firewall rules to allow only your monitoring system's IP address
- Keep both keys confidential. The agent key only reads metrics; the administration key (`admin_key`) changes the agent, so give it to nobody who only needs to read
- Use TLS 1.2 or higher (the agent defaults to TLS 1.2 minimum)
- For self-signed certificates, distribute the CA certificate to your monitoring system rather than disabling certificate verification
