# Configuration

SenHub Agent uses YAML configuration to define monitoring probes, data storage, and agent settings. Changes are detected automatically and applied without restarting the service.

Two layouts are supported and the agent auto-detects which one you use:

- **Single file** — one `agent.yaml` (or legacy `agent-config.yaml`) with everything, used by the installer.
- **Multi-file** — `agent.yaml` for global settings plus `probes.d/` and `strategies.d/` directories for fragments (introduced in 0.1.93). Cleaner once you start managing more than a handful of probes or sinks. See [Multi-File Configuration Layout](#multi-file-configuration-layout) below.

## Configuration File Location

The default configuration file is `agent.yaml` at the OS canonical path:

| OS | Default path |
|---|---|
| **Windows** | `%ProgramData%\SenHub\agent.yaml` |
| **Linux** | `/etc/senhub-agent/agent.yaml` |
| **macOS** | `/usr/local/etc/senhub-agent/agent.yaml` |

Legacy `agent-config.yaml` files continue to work unchanged. You can override the path at install time:

```bash
senhub-agent install --config-path /etc/senhub-agent/agent.yaml
```

## Configuration Structure Overview

The configuration file has four main sections:

```yaml
config_version: 2

agent:
  key: "550e8400-e29b-41d4-a716-446655440000"

storage:
  - name: http
    params:
      port: 8080
      endpoints: ["prtg", "web", "nagios"]

cache:
  retention_minutes: 5

probes:
  - name: "Server CPU"
    type: cpu
    params:
      interval: 60
```

## Config versions

`config_version` is the top-level schema version of the configuration. The
agent migrates an older configuration forward automatically when it loads it,
and stamps the new version on disk.

| Version | Introduced | Meaning |
|---|---|---|
| `1` | 0.1.x | Legacy baseline. |
| `2` | — | Current baseline: multi-file layout and `${env:}` / `${file:}` substitution. |
| `3` | 0.5.0 | Secret references: inline plaintext secrets are sealed into the [secret store](secret-store.md) and rewritten as `${secret:...}`. |

On first boot under version 3, the agent seals any inline plaintext secret
into the store, replaces it with a `${secret:...}` reference, and stamps the
file `config_version: 3`. The bump to 3 happens **only when a secret is
actually sealed** — a secret-free version 2 configuration stays at version 2
and is left untouched.

!!! warning "Do not downgrade under a sealed config"
    An older agent only supports up to the `config_version` it shipped with
    (0.4.x and earlier: version 2). Loading a configuration whose version is
    newer than the agent supports is **refused** with
    `configuration version N is too new for this agent`, rather than passing
    an unresolved `${secret:}` literal to a probe. Upgrade every agent before
    distributing a version 3 (sealed) configuration.

## Agent Section

The `agent` section defines the agent identity.

| Parameter | Required | Description |
|-----------|----------|-------------|
| `key` | Yes | Authentication key (UUID format), provided by SenHub support |
| `license` | No | License token for premium probes (see License section) |

## Probes Section

Each probe entry defines a monitoring target. The agent collects metrics at regular intervals from the configured probes.

### Probe Parameters

| Parameter | Required | Description |
|-----------|----------|-------------|
| `name` | Yes | Unique display name for this probe instance |
| `type` | Yes | Probe type (see Available Probe Types below) |
| `params` | Yes | Probe-specific parameters |
| `custom_tags` | No | Additional key-value tags attached to all metrics from this probe |

### Available Probe Types

| Type | License | Description |
|------|---------|-------------|
| `cpu` | Free | CPU utilization |
| `memory` | Free | Memory usage (physical and swap) |
| `logicaldisk` | Free | Disk space and I/O metrics |
| `network` | Free | Network interface metrics (bandwidth, errors, packets) |
| `veeam` | Pro | Veeam Backup & Replication v13 monitoring (via REST API) |
| `citrix` | Pro | Citrix Virtual Apps and Desktops monitoring (via Director API) |
| `netscaler` | Pro | Citrix ADC / NetScaler monitoring (via NITRO API) |
| `redfish` | Pro | Hardware monitoring via Redfish API (Dell iDRAC, HPE iLO, etc.) |
| `ping_webapp` | Pro | Web application availability check |
| `load_webapp` | Pro | Web page load time measurement |
| `ping_gateway` | Pro | Network gateway connectivity monitoring |
| `syslog` | Pro | Syslog message collection (UDP/TCP) |
| `event` | Pro | Custom event collection via HTTP |
| `wifi_signal_strength` | Pro | WiFi signal quality monitoring |
| `otel` | Enterprise | OpenTelemetry metrics collection |

### Common Probe Parameters

All probes support the following parameters in the `params` section:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `interval` | `60` | Collection interval in seconds |
| `timeout` | `30` | Request timeout in seconds |

### Custom Tags

You can attach custom tags to all metrics from a specific probe. Tags are included in the metric output for filtering and grouping in your monitoring system:

```yaml
probes:
  - name: "Production CPU"
    type: cpu
    params:
      interval: 60
    custom_tags:
      environment: "production"
      location: "datacenter-paris"
      team: "infrastructure"
```

Tags appear in PRTG, Nagios, and other monitoring tool outputs, allowing you to filter and organize your metrics.

## Storage Section

The `storage` section defines how the agent exposes collected metrics. The main storage type is `http`, which provides the REST API and web dashboard.

```yaml
storage:
  - name: http
    params:
      port: 8080
      bind_address: "0.0.0.0"
      endpoints: ["prtg", "web", "nagios"]
```

### HTTP Storage Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `port` | `8080` | TCP port for the HTTP API |
| `bind_address` | `127.0.0.1` | Network interface to bind to. Loopback by default — remote pollers (PRTG, Prometheus) require an explicit `"0.0.0.0"` or interface IP |
| `endpoints` | `["prtg", "web"]` | Enabled endpoint types |

### Available Endpoint Types

| Endpoint | Description |
|----------|-------------|
| `prtg` | PRTG-formatted JSON API for PRTG Network Monitor integration |
| `web` | Built-in web dashboard (Dashboard, Sensor Builder, Documentation) |
| `nagios` | Nagios-compatible check output |

### HTTPS Configuration

To enable HTTPS, add a `tls` section to the HTTP storage parameters:

```yaml
storage:
  - name: http
    params:
      port: 8443
      endpoints: ["prtg", "web", "nagios"]
      tls:
        enabled: true
        min_tls_version: "1.2"
        cert_file: "/opt/senhub/certs/agent-cert.pem"
        key_file: "/opt/senhub/certs/agent-key.pem"
```

If you installed with `--enable-https`, the agent generated self-signed certificates automatically in the `certs/` directory. You can replace them with your own certificates.

| TLS Parameter | Default | Description |
|---------------|---------|-------------|
| `enabled` | `false` | Enable HTTPS |
| `min_tls_version` | `1.2` | Minimum TLS version (1.2 or 1.3) |
| `cert_file` | - | Path to TLS certificate file (.pem or .crt) |
| `key_file` | - | Path to TLS private key file (.pem or .key) |

## Cache Section

The `cache` section controls how long collected metrics are kept in memory before being discarded.

```yaml
cache:
  retention_minutes: 5
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `retention_minutes` | `5` | Number of minutes to keep metrics in cache |

Monitoring systems (PRTG, Nagios, etc.) read metrics from the cache. Set the retention period longer than the longest polling interval of your monitoring system.

## Configuration Examples

### Minimal Configuration

The simplest configuration with system monitoring probes:

```yaml
config_version: 2

agent:
  key: "550e8400-e29b-41d4-a716-446655440000"

probes:
  - name: "CPU"
    type: cpu
    params:
      interval: 60

  - name: "Memory"
    type: memory
    params:
      interval: 60
```

### System Monitoring Configuration

A complete system monitoring setup with all free-tier probes:

```yaml
config_version: 2

agent:
  key: "550e8400-e29b-41d4-a716-446655440000"

storage:
  - name: http
    params:
      port: 8080
      endpoints: ["prtg", "web", "nagios"]

cache:
  retention_minutes: 5

probes:
  - name: "CPU"
    type: cpu
    params:
      interval: 60

  - name: "Memory"
    type: memory
    params:
      interval: 60

  - name: "Disk"
    type: logicaldisk
    params:
      interval: 60

  - name: "Network"
    type: network
    params:
      interval: 60
```

### Production Configuration with Premium Probes

A production setup monitoring Citrix and NetScaler infrastructure:

```yaml
config_version: 2

agent:
  key: "550e8400-e29b-41d4-a716-446655440000"
  # Paid tier? Place your license file as license.jwt next to this config
  # (see the License section) — no need to put the token inline.

storage:
  - name: http
    params:
      port: 8443
      endpoints: ["prtg", "web", "nagios"]
      tls:
        enabled: true
        min_tls_version: "1.2"

cache:
  retention_minutes: 5

probes:
  - name: "CPU"
    type: cpu
    params:
      interval: 60

  - name: "Memory"
    type: memory
    params:
      interval: 60

  - name: "Disk"
    type: logicaldisk
    params:
      interval: 60

  - name: "Network"
    type: network
    params:
      interval: 60

  - name: "Citrix Production"
    type: citrix
    params:
      base_url: "https://director.company.com"
      interval: 120
      auth:
        username: "DOMAIN\\svc-monitoring"
        password: "SecurePassword123"
      tls:
        verify_ssl: true
      timeout: 30
    custom_tags:
      environment: "production"
      site: "paris"

  - name: "NetScaler LB"
    type: netscaler
    params:
      base_url: "https://netscaler.company.com"
      username: "monitoring-user"
      password: "SecurePassword123"
      interval: 60
    custom_tags:
      environment: "production"

  - name: "Hardware iDRAC"
    type: redfish
    params:
      base_url: "https://idrac-server01.company.com"
      username: "monitoring"
      password: "SecurePassword123"
      interval: 300
      tls:
        verify_ssl: false
```

### Web Application Monitoring Configuration

Monitoring web application availability and load times:

```yaml
config_version: 2

agent:
  key: "550e8400-e29b-41d4-a716-446655440000"
  # Paid tier? Place your license file as license.jwt next to this config
  # (see the License section) — no need to put the token inline.

probes:
  - name: "Website Availability"
    type: ping_webapp
    params:
      url: "https://www.company.com"
      interval: 30
      timeout: 10

  - name: "Website Load Time"
    type: load_webapp
    params:
      url: "https://www.company.com"
      interval: 60
      timeout: 30

  - name: "Gateway Paris"
    type: ping_gateway
    params:
      target: "192.168.1.1"
      interval: 30
```

## Applying Configuration Changes

The agent watches the configuration file for changes. When you modify `agent.yaml` (or `agent-config.yaml`), the changes are detected and applied automatically within a few seconds. **No service restart is required.**

This applies to:

- Adding, removing, or modifying probes
- Changing probe parameters (intervals, credentials, URLs)
- Modifying storage configuration (port, endpoints, TLS)
- Adding or removing custom tags

## License

### Free Tier

Without a license, the agent runs with free-tier probes only: `cpu`, `memory`, `logicaldisk`, `network`. These probes are always available regardless of license status.

### Obtaining a License

Contact SenHub support (support@senhub.io) to request a license token. Specify the probe types you need:

- **Pro license**: adds Citrix, NetScaler, Redfish, Ping, SNMP, Syslog, Event
- **Enterprise license**: all current and future probe types

### Where the license is stored

The license is kept in a dedicated file, `license.jwt`, next to `agent.yaml`:

- Linux: `/etc/senhub/license.jwt`
- Windows: `%ProgramData%\SenHub\license.jwt`

Keeping it in its own file makes it easy to hand over and avoids pasting a long
token into your YAML. The token is stored in clear text there by design (it is
bound to your agent key and grants nothing on its own), so it is not sealed.

> An existing install that still has the token inline under `agent:` `license:`
> in `agent.yaml` keeps working, and is moved to `license.jwt` automatically on
> the next start.

### License Formats

SenHub supports two license formats, both auto-detected:

- **Compact key** (recommended): a short 40-character key bound to your agent
  key, e.g. `SH-040GMS-000100-02S3S2-HC3HMV-7RBZ4Y-PY`.
- **JWT token**: a longer token (~700 characters) starting with `eyJ`.

### Activating a License

**Option A — drop the file (simplest).** Save the license file you received
from support as `license.jwt` next to `agent.yaml` (see paths above), then
restart the agent:

```bash
# Linux
sudo cp license.jwt /etc/senhub/license.jwt
sudo systemctl restart senhub-agent
```

**Option B — CLI.** Activate with the token; this validates it, verifies the
agent-key binding, and writes `license.jwt` for you:

```bash
senhub-agent license activate SH-040GMS-000100-02S3S2-HC3HMV-7RBZ4Y-PY
```

Either way, **the license takes effect after restarting the agent** — a license
change is not picked up while the agent is running.

### Verifying License Status

You can check the license status at any time:

```bash
senhub-agent license show
```

Or via the API:
```bash
curl http://localhost:8080/api/{key}/license/status
```

Example API response:
```json
{
  "status": "active",
  "tier": "pro",
  "expires_at": "2026-06-30T23:59:59Z",
  "days_remaining": 120,
  "authorized_probes": ["cpu", "memory", "logicaldisk", "network", "citrix", "netscaler", "redfish", "ping_webapp", "syslog"],
  "free_tier_probes": ["cpu", "memory", "logicaldisk", "network"]
}
```

### License Tiers

| Tier | Available Probes |
|------|-----------------|
| **Free** | cpu, memory, logicaldisk, network |
| **Pro** | All free + veeam, citrix, netscaler, redfish, ping_webapp, load_webapp, ping_gateway, syslog, event, wifi_signal_strength |
| **Enterprise** | All probes (including future additions) |

### Grace Period

When a license expires, there is a 7-day grace period during which premium probes continue to work. After the grace period, the agent reverts to free-tier probes only.

### Other License Commands

```bash
senhub-agent license show                # Show current license details
senhub-agent license remove              # Remove license (reverts to free tier)
senhub-agent license remove --force      # Remove without confirmation prompt
```

## Auto-Update

The agent can check for new versions and optionally install them automatically.

```yaml
auto_update:
  enabled: false          # Automatic installation of new versions
  include_beta: false     # Include beta versions in update checks
  url: "https://eu-west-1.intake.senhub.io/releases"
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `enabled` | `false` | Automatically install new versions when available |
| `include_beta` | `false` | Include beta versions in update checks |
| `url` | SenHub releases | Update server URL |

Even with `enabled: false`, the agent checks for new versions at startup and logs a message if an update is available. Use `senhub-agent update --list` to see available versions and `senhub-agent update <version>` to install manually.

## Validating Configuration

Use the built-in configuration checker before deploying changes:

```bash
senhub-agent config check agent-config.yaml
```

This validates:
- YAML syntax (with line-level error context for syntax errors)
- Required fields and values
- License validity and agent key binding
- Probe types and required parameters
- Storage strategy names

Example output:
```
Checking configuration: C:\SenHub\agent-config.yaml

  [OK]   config_version: 2
  [OK]   agent.key: 550e8400-e29b-41d4-a716-446655440000
  [OK]   agent.license: tier=pro, expires=2031-04-14
  [OK]   License binding verified
  [OK]   1 probe(s) configured
  [OK]   Probe "veeam-prod" (type: veeam)
  [OK]   Storage: http

Configuration is valid.
```

## Multi-File Configuration Layout

Starting from version 0.1.93, the configuration can be split across multiple files. This is optional: an existing monolithic `agent-config.yaml` continues to work unchanged.

### Per-OS default paths

The agent looks for the multi-file layout in the **same directory as `agent.yaml`** (or `agent-config.yaml`). The defaults match how the installer lays things out per OS:

| OS | `agent.yaml` | `probes.d/` | `strategies.d/` |
|---|---|---|---|
| **Linux (systemd)** | `/etc/senhub-agent/agent.yaml` | `/etc/senhub-agent/probes.d/` | `/etc/senhub-agent/strategies.d/` |
| **Linux (tarball)** | `/opt/senhub/bin/agent.yaml` | `/opt/senhub/bin/probes.d/` | `/opt/senhub/bin/strategies.d/` |
| **Windows (MSI)** | `%ProgramData%\SenHub\agent.yaml` | `%ProgramData%\SenHub\probes.d\` | `%ProgramData%\SenHub\strategies.d\` |
| **macOS** | `/usr/local/etc/senhub-agent/agent.yaml` | `/usr/local/etc/senhub-agent/probes.d/` | `/usr/local/etc/senhub-agent/strategies.d/` |

Override any of these by passing `--config-path` to the agent — the directories `probes.d/` and `strategies.d/` are always resolved next to whichever `agent.yaml` is loaded.

### Layout

```
<config dir>/
├── agent.yaml                     # Global settings only (no probes/storage)
├── probes.d/
│   ├── 01-system.yaml             # YAML array of probe configs
│   ├── 10-citrix.yaml
│   └── 20-netscaler.yaml
└── strategies.d/
    ├── 01-http.yaml               # One strategy per file
    ├── 02-prometheus.yaml
    └── 10-otlp.yaml
```

- Files are loaded in **alphabetical order** within each directory. The two-digit prefix is a convention, not a requirement — use it to control merge order.
- Files matching `.*` (dotfiles) or `*.disabled` are **skipped**. Disable a fragment by renaming it: `mv 20-citrix.yaml 20-citrix.yaml.disabled`.
- An **empty** `probes.d/` or `strategies.d/` directory is valid (zero entries, no error).
- Each file in `strategies.d/` has **exactly one** top-level key, which is the strategy name. Duplicate strategy across files: later file wins, a WARN log surfaces the override.

### `agent.yaml` example (global only)

```yaml
config_version: 2
agent:
  key: "550e8400-e29b-41d4-a716-446655440000"
  license: "${file:/etc/senhub/license.jwt}"
cache:
  retention_minutes: 5
auto_update:
  enabled: false
```

### `probes.d/01-system.yaml` example

```yaml
- name: CPU
  type: cpu
  params:
    interval: 30
- name: Memory
  type: memory
  params:
    interval: 30
```

### `strategies.d/01-http.yaml` example

```yaml
http:
  bind_address: "127.0.0.1"
  port: 8080
  endpoints: [prtg, nagios, prometheus, web]
```

### Backward compatibility

If `agent.yaml` (or `agent-config.yaml`) contains a top-level `probes:` or `storage:` block, the agent uses the **legacy monolithic** path and **ignores** `probes.d/` and `strategies.d/`. A WARN log surfaces the situation so you can migrate at your own pace:

> Legacy monolithic config detected (top-level probes:/storage: present) — *.d/ directories are IGNORED. Migrate by trimming probes and storage out of the top file.

To migrate: remove the inline `probes:` and `storage:` blocks from `agent.yaml`, redistribute their entries across `probes.d/` and `strategies.d/`, restart the agent.

## Environment and File Substitution

String values in any configuration file can reference environment variables or file contents:

| Syntax | Resolves to |
|---|---|
| `${env:VAR}` | Value of `$VAR`, or empty string if unset |
| `${env:VAR:-fallback}` | Value of `$VAR`, or `fallback` if unset |
| `${file:/path/to/file}` | File contents, **trimmed of whitespace**. Error if the file is missing. |
| `${file:/path:-fallback}` | File contents, or `fallback` if the file is missing |
| `${secret:NAME}` | Value from the OS-native [secret store](secret-store.md). Error if the name is unknown or no backend is available. |
| `${secret:NAME:-fallback}` | Stored value, or `fallback` if the name is unknown |
| `$$` | Literal `$` character (escape) |

Substitution applies to **values** only — never to YAML keys. References inside `params:` blocks of probes and strategies are also resolved.

For `${secret:}` — storing values, the per-OS backends, and sealing inline secrets — see the [Secret Store](secret-store.md) page.

### Examples

```yaml
agent:
  license: "${file:/etc/senhub/license.jwt}"

probes:
  - name: db
    type: mysql
    params:
      host: "${env:DB_HOST:-127.0.0.1}"
      username: monitor
      password: "${file:/etc/senhub/secrets/db_password}"
```

A missing required reference (file not found, no default) **aborts agent boot** with the offending reference in the error message. An unset environment variable without a default substitutes to an empty string and does **not** abort — match POSIX shell behaviour.

## Inspecting the merged configuration

The `agent config show` command prints the final, merged configuration as YAML with map keys sorted alphabetically:

```bash
senhub-agent config show              # default: --resolved
senhub-agent config show --resolved   # references substituted
senhub-agent config show --raw        # references preserved
senhub-agent config show --redact     # secrets masked with ***
```

- `--resolved` (default): the same configuration the agent boots with — `${env:..}` / `${file:..}` resolved against the current environment and filesystem.
- `--raw`: the merged configuration BEFORE substitution. Useful for auditing the layout (which files contributed which entries) before comparing against the resolved output.
- `--redact`: resolved configuration but with values that came from `${file:..}` AND any value whose YAML key matches `(?i)(key|token|password|secret)` masked with `***`. Safe to copy into a support ticket or commit to source control.

Output is deterministic — two runs of the same input produce byte-identical output, suitable for `diff` and CI checks.

## Security Recommendations

- Protect configuration file permissions: Windows (Administrators only), Linux (`chmod 600`)
- Use service accounts with minimal permissions for probe credentials (Citrix, NetScaler, Redfish)
- Use HTTPS in production for the agent API
- Never commit configuration files containing passwords to version control
- Bind the agent to a specific network interface (`bind_address: "127.0.0.1"`) when remote access is not needed
- For secrets, prefer `${file:/path/to/secret}` references over inline values — file permissions limit blast radius and `agent config show --redact` masks them safely

## Troubleshooting Configuration

### Configuration Not Loading

Check agent logs for errors:

- Windows: `%ProgramData%\SenHub\logs\senhubagent.log`
- Linux: `/var/log/senhub-agent/senhubagent.log`

If there is a YAML syntax error, the agent keeps the previous valid configuration and logs the error.

### Probe Not Starting

Common causes:

- Invalid credentials (check logs for authentication errors)
- Network connectivity issues (verify the agent can reach the target system)
- License restrictions (verify the probe type is authorized in your license tier)
- Missing required parameters (check the probe-specific documentation)

Check probe-specific troubleshooting in the [Citrix Guide](probes/citrix.md) or [NetScaler Guide](probes/netscaler.md).
