# Configuration

SenHub Agent uses YAML configuration to define monitoring probes, data storage, and agent settings. Changes are detected automatically and applied without restarting the service.

Two layouts are supported and the agent auto-detects which one you use:

- **Multi-file**: `agent.yaml` for global settings plus `probes.d/` and `strategies.d/` directories for fragments. This is the layout `install`, `config init` and the Windows MSI generate. See [Multi-File Configuration Layout](#multi-file-configuration-layout) below.
- **Single file** (legacy): one `agent.yaml` or `agent-config.yaml` carrying top-level `probes:` and `storage:` blocks. Existing installs keep working; `senhub-agent config migrate` converts them. The single-file examples on this page show the same settings in one block for readability; in the multi-file layout, probes go under `probes.d/` and outputs under `strategies.d/`.

## Configuration File Location

The default configuration file is `agent.yaml` at the OS canonical path:

| OS | Default path |
|---|---|
| **Windows** | `%ProgramData%\SenHub\agent.yaml` |
| **Linux** | `/etc/senhub-agent/agent.yaml` |
| **macOS** | `/usr/local/etc/senhub-agent/agent.yaml` |

Legacy `agent-config.yaml` files continue to work unchanged. You can override the path at install time; an absolute path is used as given, and a relative path resolves next to the binary:

```bash
sudo ./senhub-agent install --config-path /etc/senhub-agent/agent.yaml
```

## Configuration Structure Overview

The configuration has four main sections, shown here in the legacy single-file form:

```yaml
config_version: 3

agent:
  key: "550e8400-e29b-41d4-a716-446655440000"

storage:
  - name: http
    params:
      port: 8080
      endpoints: ["prtg", "web", "nagios", "prometheus"]

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
| `1` | 0.1.x | Legacy. |
| `2` | 0.2.x | Legacy: multi-file layout and `${env:}` / `${file:}` substitution. |
| `3` | 0.5.0 | Current. Secret references: inline plaintext secrets are sealed into the [secret store](secret-store.md) and rewritten as `${secret:...}`. A new configuration is written at version 3. |

Versions 1 and 2 are legacy numbers an older configuration may still carry; the agent loads them and migrates them forward. On first boot under version 3, the agent seals any inline plaintext secret
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
| `key` | Yes | Agent key (UUID format), generated on the machine by `install`, `config init` or the MSI. It identifies the agent and is the key PRTG, Nagios and a Prometheus scrape read with. It must not be empty. It does not open the web console, which answers only the administration key (`admin_key` of the `http` output) |
| `license` | No | License token for premium probes (see License section) |
| `global_tags` | No | Key-value tags applied to every datapoint of every probe. A probe's own `custom_tags` win on a key present in both. Keep the set small — every key multiplies the series a backend stores |

## Entities Section

The `entities` section turns on **entity detection**: the agent
describes what this host is, what runs on it and what it talks to, and
publishes that as a stream any output may consume.

```yaml
entities:
  enabled: true
  interval: 5m
  depends_on:
    enabled: false
    debounce: 3
    exclude_cidrs: ["10.50.0.0/16"]
```

| Parameter | Default | Description |
|---|---|---|
| `enabled` | see below | Runs the detector. Off means nothing is produced and nothing is polled |
| `interval` | `5m` | Heartbeat: everything is re-described each interval, and the interval travels with each event as the consumer's staleness hint |
| `depends_on.enabled` | `false` | Also map this host's outbound dependencies. Off by default because which peers a host talks to can be sensitive |
| `depends_on.debounce` | `3` | How many consecutive scrapes a peer must persist before it counts as a dependency rather than a passing connection. The delay before one appears is `debounce × interval` |
| `depends_on.exclude_cidrs` | none | Peer ranges to leave out entirely |

**Why this is not under an output.** What a host *is* does not depend on
where the description is shipped. The detector feeds a channel that
several outputs can read at once, so the decision to describe the host
is made once, here, rather than inherited from one output's settings.

**The default of `enabled`.** Absent this section, the agent falls back
to whatever an OTLP output declares under `signals.entities`, which is
where this setting used to live — so an existing install keeps behaving
exactly as it did. An agent with neither produces nothing.

**It has a cost**, which is why it is not on for everyone: every source
is polled each interval, and the dependency scanner reads the host's
sockets. An agent that does not want it pays none of it.

## Probes Section

Each probe entry defines a monitoring target. The agent collects metrics at regular intervals from the configured probes.

### Probe Parameters

| Parameter | Required | Description |
|-----------|----------|-------------|
| `name` | Yes | Unique display name for this probe instance |
| `type` | Yes | Probe type (see Available Probe Types below) |
| `params` | Yes | Probe-specific parameters |
| `custom_tags` | No | Additional key-value tags attached to all metrics from this probe |
| `governance` | No | Who owns what this probe observes, how critical it is, where it is, and which application chain it belongs to. Stamped on this probe's entities, metrics and logs. See [Governance per probe](#governance-per-probe). |
| `enabled` | No | Set to `false` to stop the probe without deleting its configuration. Absent means enabled, so existing files are unaffected. A disabled probe collects nothing and reports no topology. |

### Turning a probe off

Set `enabled: false` rather than deleting the entry:

```yaml
probes:
  - name: mysql-prod
    type: mysql
    enabled: false
    params:
      host: 127.0.0.1
      username: monitor
```

Deleting the entry works too, but it takes the credentials, intervals and tags
with it — which have to be retyped to turn the probe back on. `agent config
check` lists disabled probes explicitly, so "why is this collecting nothing"
has an answer that does not require reading the file.

### Available Probe Types

The complete list of probe types lives in the [probe catalog](probes/index.md). Each catalog page documents the probe's `type` value, its parameters and metrics, and carries a Free/Pro tier badge.

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

### Governance per probe

The agent-level `governance` block (see [OpenTelemetry output](otlp.md))
describes the host the agent runs on. It says nothing about what a probe
observes: the database on that host may belong to one application and the
one next to it to another, and a probe that reads a remote system observes
something with an owner of its own. A `governance` block on the probe entry
states that, with the same vocabulary:

```yaml
probes:
  - name: erp-db
    type: mysql
    params:
      host: 127.0.0.1
      username: monitor
      password: "${secret:erp-db.password}"
    governance:
      criticality: high
      owner:
        team: dba
        contact: dba@example.com
      lifecycle: active
      labels:
        application: erp

  - name: crm-db
    type: mysql
    params:
      host: 127.0.0.1
      port: 3307
      username: monitor
      password: "${secret:crm-db.password}"
    governance:
      criticality: medium
      labels:
        application: crm
```

The block is stamped on the entities the probe reports, and on every metric
and log record it produces, as the same attributes a topology consumer
already reads: `entity.owner.team`, `entity.owner.contact`,
`service.criticality`, `entity.location.*`, `entity.lifecycle.status` and
`entity.label.<key>`. The host entity is never touched by a probe's
governance. From the agent-level block, only the location (`site`,
`datacenter`, `rack`, `room`) descends, and only to the entities the agent
places on its own host: a database reached on the loopback address is where
the host is, a database reached across the network is not. Owner,
criticality, lifecycle and labels never descend; a database on a machine is
often another team's, and a host runs more than one application.

An application is not an entity. It is the label `application`, put on
every instance that takes part in an application chain: with it, every
entity, metric and log of that chain can be found by one filter, across
hosts and probe types. Two instances on the same host can belong to two
different chains.

Precedence, key by key, is the most specific statement: a governance rule
matched by `snmp_poll` discovery, then the probe's `governance` block, then
the host's location for what runs on it. On a key present in both, a probe's
`custom_tags` win over its `governance`.

`criticality` takes `critical` / `high` / `medium` / `low`; `lifecycle`
takes `active` / `maintenance` / `decommissioning` / `retired`. An unknown
key or a value outside those sets is an error in `agent config check` and
is refused by the web console.

## Storage Section

The `storage` section defines how the agent exposes collected metrics. The main storage type is `http`, which provides the REST API and the [web console](web-interface.md).

```yaml
storage:
  - name: http
    params:
      port: 8080
      bind_address: "0.0.0.0"
      endpoints: ["prtg", "web", "nagios", "prometheus"]
```

### HTTP Storage Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `port` | `8080` | TCP port for the HTTP API |
| `bind_address` | `127.0.0.1` | Network interface to bind to. Loopback by default — remote pollers (PRTG, Prometheus) require an explicit `"0.0.0.0"` or interface IP |
| `endpoints` | none | Enabled endpoint types. There is no default: an endpoint answers only when it is listed. The installer writes `["prtg", "web", "nagios", "prometheus"]` |
| `max_cache_size` | `50000` | Maximum number of distinct series the shared metric cache holds. Past it new series are refused and counted, rather than growing memory without bound — the cache is also fed by `otlp_receiver` and `prometheus_scrape`, whose series sets come from senders you do not control. `0` means unbounded |

### Available Endpoint Types

| Endpoint | Description |
|----------|-------------|
| `prtg` | PRTG-formatted JSON API for PRTG Network Monitor integration |
| `web` | Built-in web console (Overview, Probes, Outputs, Settings) |
| `nagios` | Nagios-compatible check output |
| `prometheus` | Prometheus text exposition on `/metrics` (see [Prometheus](prometheus/index.md)) |

### HTTPS Configuration

To enable HTTPS, add a `tls` section to the HTTP storage parameters:

```yaml
storage:
  - name: http
    params:
      port: 8443
      endpoints: ["prtg", "web", "nagios", "prometheus"]
      tls:
        enabled: true
        min_tls_version: "1.2"
        cert_file: "/etc/senhub-agent/certs/agent-cert.pem"
        key_file: "/etc/senhub-agent/certs/agent-key.pem"
```

If you installed with `--enable-https`, the agent generated self-signed certificates automatically in the `certs/` directory next to `agent.yaml` (`/etc/senhub-agent/certs/` on Linux, `%ProgramData%\SenHub\certs\` on Windows). You can replace them with your own certificates.

| TLS Parameter | Default | Description |
|---------------|---------|-------------|
| `enabled` | `false` | Enable HTTPS |
| `min_tls_version` | `1.2` | Minimum TLS version (1.2 or 1.3) |
| `cert_file` | - | Path to TLS certificate file (.pem or .crt) |
| `key_file` | - | Path to TLS private key file (.pem or .key) |

### Push storages

Two storages push rather than being polled, and both take their own
parameters. Neither is required: an agent polled over HTTP needs no
storage but `http`.

```yaml
storage:
  - name: prtg
    params:
      server_url: "https://prtg.example.com"
      interval: 60s
      data_retention_period: 2m

  - name: event
    params:
      server_url: "https://eu-west-1.intake.senhub.io"
      queue_size: 1000
      sync_interval: 30s
```

| Parameter | Storage | Default | Description |
|-----------|---------|---------|-------------|
| `server_url` | both | — | Required. The destination the agent pushes to |
| `interval` | `prtg` | `60s` | How often the cached values are pushed |
| `data_retention_period` | `prtg` | `2m` | How long a pushed value stays valid. A value older than this is dropped rather than sent, so a stalled probe reports no data instead of a stale reading |
| `queue_size` | `event` | `1000` | Events held in memory awaiting a push. Past it the oldest are dropped and counted |
| `sync_interval` | `event` | `30s` | How often the queue is flushed. A large batch is flushed earlier on its own |

## Cache Section

The `cache` section controls how long collected metrics are kept in memory before being discarded.

```yaml
cache:
  retention_minutes: 5
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `retention_minutes` | `5` | Number of minutes a value stays served by the pull outputs (PRTG, Nagios, Prometheus, Web UI) after its probe last produced it. A probe that runs less often keeps its last value until its next run is due, whatever this setting. |

Monitoring systems (PRTG, Nagios, etc.) read metrics from the cache. Set the retention period longer than the longest polling interval of your monitoring system.

## Configuration Examples

### Minimal Configuration

The simplest configuration with system monitoring probes:

```yaml
config_version: 3

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
config_version: 3

agent:
  key: "550e8400-e29b-41d4-a716-446655440000"

storage:
  - name: http
    params:
      port: 8080
      endpoints: ["prtg", "web", "nagios", "prometheus"]

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
config_version: 3

agent:
  key: "550e8400-e29b-41d4-a716-446655440000"
  # Paid tier? Place your license file as license.jwt next to this config
  # (see the License section) — no need to put the token inline.

storage:
  - name: http
    params:
      port: 8443
      endpoints: ["prtg", "web", "nagios", "prometheus"]
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
      director:
        url: "https://director.company.com"
        auth:
          username: "DOMAIN\\svc-monitoring"
          password: "${secret:citrix-production.password}"
        verify_ssl: true
      interval: 120
      timeout: 30
    custom_tags:
      environment: "production"
      site: "paris"

  - name: "NetScaler LB"
    type: netscaler
    params:
      base_url: "https://netscaler.company.com"
      username: "monitoring-user"
      password: "${secret:netscaler-lb.password}"
      interval: 60
    custom_tags:
      environment: "production"

  - name: "Hardware iDRAC"
    type: redfish
    params:
      endpoint: "https://idrac-server01.company.com"
      username: "monitoring"
      password: "${secret:hardware-idrac.password}"
      interval: 300
      verify_ssl: false
```

### Web Application Monitoring Configuration

Monitoring web application availability and load times:

```yaml
config_version: 3

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

  - name: "Website Load Time"
    type: load_webapp
    params:
      url: "https://www.company.com"
      interval: 60
      timeout: "30s"

  # One instance per host. The gateway is read from the routing table,
  # so the probe takes no parameter beyond the common interval.
  - name: "Gateway"
    type: ping_gateway
    params:
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

Without a license the agent runs every Free-tier probe: the whole universal collection tier — OS/host, logs, network checks, and the application, database and broker probes (MySQL, PostgreSQL, Redis, Kafka, Docker, and more). Each page of the [probe catalog](probes/index.md) shows a Free/Pro tier badge. Only the Pro probes (deep vendor integrations) need a license.

### Obtaining a License

Contact SenHub support (support@senhub.io) to request a license token. Specify the probe types you need:

- **Pro license**: adds the deep vendor, HA, cloud and active-check integrations — `citrix`, `netscaler`, `veeam`, `redfish`, `ibmi`, `powerstore`, `mssql_ha`, `oracle_enterprise`, `hyperv_ha`, `vsphere_ha`, `ad_hybrid`, `exchange_online`, `azure_container_apps`, `ping_gateway`, `ping_webapp`, `load_webapp`
- **Enterprise license**: all current and future probe types

### Where the license is stored

The license is kept in a dedicated file, `license.jwt`, next to `agent.yaml`:

- Linux: `/etc/senhub-agent/license.jwt`
- Windows: `%ProgramData%\SenHub\license.jwt`

Keeping it in its own file makes it easy to hand over and avoids pasting a long
token into your YAML. The token is stored in clear text there by design (it is
signed, bound to your organisation or to one agent key, and grants nothing on
its own), so it is not sealed.

> An existing install that still has the token inline under `agent:` `license:`
> in `agent.yaml` keeps working, and is moved to `license.jwt` automatically on
> the next start.

### License Format

A licence is a signed JWT, a token of about 700 characters starting with
`eyJ`. The short `SH-...` keys of versions before 0.3.0 are no longer
accepted; the agent says so when it meets one, and support re-issues it
as a JWT.

### Activating a License

**Option A — drop the file (simplest).** Save the license file you received
from support as `license.jwt` next to `agent.yaml` (see paths above), then
restart the agent:

```bash
# Linux
sudo cp license.jwt /etc/senhub-agent/license.jwt
sudo systemctl restart senhub-agent
```

**Option B — CLI.** Activate with the token; this validates it, verifies that it
is issued for this agent (or for your organisation), and writes `license.jwt` for you:

```bash
sudo /usr/local/bin/senhub-agent license activate - < license.jwt
```

Read from standard input, the token stays out of the process list and the
shell history; it is also accepted as the argument.

Either way, **the license takes effect after restarting the agent** — a license
change is not picked up while the agent is running.

### Verifying License Status

You can check the license status at any time:

```bash
sudo /usr/local/bin/senhub-agent license show
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
  "authorized_probes": ["citrix", "netscaler", "redfish", "veeam"],
  "free_tier_probes": ["cpu", "memory", "logicaldisk", "network", "mysql", "postgresql", "redis", "docker", "syslog", "..."]
}
```

`authorized_probes` lists the Pro probes this license unlocks (Free probes are
always available and are not repeated here). `free_tier_probes` is the full
Free tier — the whole universal collection tier, abbreviated above; see the
[probe catalog](probes/index.md) for the tier badge on every probe.

### License Tiers

| Tier | Available Probes |
|------|-----------------|
| **Free** | The universal collection tier — OS/host, logs, network checks, application, database and broker probes. See the [probe catalog](probes/index.md) for the tier badge on each probe. |
| **Pro** | All free + citrix, netscaler, veeam, redfish, ibmi, powerstore, mssql_ha, oracle_enterprise, hyperv_ha, vsphere_ha, ad_hybrid, exchange_online, azure_container_apps, ping_gateway, ping_webapp, load_webapp |
| **Enterprise** | All probes (including future additions) |

### Grace Period

When a license expires, there is a 7-day grace period during which premium probes continue to work. After the grace period, the agent reverts to free-tier probes only.

### Other License Commands

```bash
sudo /usr/local/bin/senhub-agent license show            # Show current license details
sudo /usr/local/bin/senhub-agent license key             # Print the agent key a licence is bound to
sudo /usr/local/bin/senhub-agent license remove          # Remove license (reverts to free tier)
sudo /usr/local/bin/senhub-agent license remove --force  # Remove without confirmation prompt
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
| `url` | SenHub releases | Update server URL. The **base** the agent appends to — a value carrying `/releases` or `/download` makes every derived URL double it |
| `version` | `latest` | Target the periodic updater tracks. `latest` resolves the newest stable; an explicit version pins to it |

Even with `enabled: false`, the agent checks for new versions at startup and logs a message if an update is available. Use `sudo /usr/local/bin/senhub-agent update --list` to see available versions and `sudo /usr/local/bin/senhub-agent update <version>` to install manually.

## Validating Configuration

Use the built-in configuration checker before deploying changes:

```bash
sudo /usr/local/bin/senhub-agent config check
sudo /usr/local/bin/senhub-agent config check /etc/senhub-agent/agent.yaml
```

This validates:
- YAML syntax (with line-level error context for syntax errors)
- Required fields and values
- License validity and agent key binding
- Probe types and required parameters
- Storage strategy names

Example output:
```
Checking configuration: /etc/senhub-agent/agent.yaml

  [OK]   config_version: 3
  [OK]   agent.key: set (UUID, value hidden; `senhub-agent key show` prints it)
  [OK]   agent.license: tier=pro, expires=2031-04-14
  [OK]   License binding verified
  [OK]   1 probe(s) configured
  [OK]   Probe "veeam-prod" (type: veeam)
  [OK]   Storage: http

Configuration is valid.
```

## Multi-File Configuration Layout

The multi-file layout is what `install`, `config init` and the Windows MSI generate. An existing monolithic `agent-config.yaml` continues to work unchanged, and `senhub-agent config migrate` converts it.

### Per-OS default paths

The agent looks for the multi-file layout in the **same directory as `agent.yaml`** (or `agent-config.yaml`). The defaults match how the installer lays things out per OS:

| OS | `agent.yaml` | `probes.d/` | `strategies.d/` |
|---|---|---|---|
| **Linux** | `/etc/senhub-agent/agent.yaml` | `/etc/senhub-agent/probes.d/` | `/etc/senhub-agent/strategies.d/` |
| **Windows (MSI)** | `%ProgramData%\SenHub\agent.yaml` | `%ProgramData%\SenHub\probes.d\` | `%ProgramData%\SenHub\strategies.d\` |
| **macOS** | `/usr/local/etc/senhub-agent/agent.yaml` | `/usr/local/etc/senhub-agent/probes.d/` | `/usr/local/etc/senhub-agent/strategies.d/` |

Override any of these by passing `--config-path` to the agent. An absolute path is used as given; a relative path resolves next to the binary. The directories `probes.d/` and `strategies.d/` are always resolved next to whichever `agent.yaml` is loaded.

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
- Only files ending in `.yaml` or `.yml` are read. A copy taken before an edit (`otlp.yaml.bak`, `otlp.yaml.20260908`) is not loaded, and the agent names the files it left out at start, so a copy is never mistaken for a live fragment. Rename a file to `*.disabled` to set it aside on purpose.

### `agent.yaml` example (global only)

```yaml
config_version: 3
agent:
  key: "550e8400-e29b-41d4-a716-446655440000"
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
probes:
  - name: db
    type: mysql
    params:
      host: "${env:DB_HOST:-127.0.0.1}"
      username: monitor
      password: "${file:/etc/senhub-agent/secrets/db_password}"
```

A missing required reference (file not found, no default) **aborts agent boot** with the offending reference in the error message. An unset environment variable without a default substitutes to an empty string and does **not** abort — match POSIX shell behaviour.

## Configuring probes from environment variables

A probe can be declared, and any probe of the files adjusted, from environment variables, with one rule that holds for a container, a systemd unit, a Helm chart or Podman alike. Nothing to place on the disk; the agent reads the variables at every start, whatever it was started from.

```
SENHUB_PROBE_<NAME>_TYPE=<probe type>      declares a probe named <name>
SENHUB_PROBE_<NAME>_<PARAM>=value          sets one of its parameters
SENHUB_PROBE_<NAME>_<BLOCK>__<PARAM>=value sets a parameter inside a block
SENHUB_PROBE_<NAME>_<PARAM>_FILE=/path     reads the value from a file
```

### How a name is read

- After `SENHUB_PROBE_`, the **first underscore ends the name**. The name holds letters and digits only and is lowercased: `SENHUB_PROBE_PG_HOST` is the parameter `host` of the probe `pg`. A name with a hyphen cannot be written in a variable; to tune a probe called `smtp-prod` from the environment, write `SENHUB_PROBE_SMTPPROD_...`: hyphens and underscores are ignored when the name is matched against the probes of the files. A probe declared only by the environment takes the name as written, lowercased.
- Everything after the name is the field. A **double underscore nests**, a single underscore stays part of the key: `SENHUB_PROBE_PG_MAX_REPLICATION_LAG_SECONDS` is `max_replication_lag_seconds`, and `SENHUB_PROBE_ACA_DISCOVERY__MAX_APPS` is `discovery.max_apps`.
- Parameter names are matched to the probe's schema without regard to case, and an alternative spelling the probe accepts resolves to its canonical key.
- A variable that is empty counts as unset.

### Fields of the probe entry

Five keys of a probe entry sit beside `params`. They are reached by their own name, ahead of any parameter:

| Variable | Sets |
|---|---|
| `SENHUB_PROBE_<NAME>_TYPE` | `type`; required to declare a probe that no file defines |
| `SENHUB_PROBE_<NAME>_ENABLED` | `enabled` (`true` or `false`) |
| `SENHUB_PROBE_<NAME>_LOG_STRATEGIES` | `log_strategies`, comma-separated (`otlp,event`) |
| `SENHUB_PROBE_<NAME>_CUSTOM_TAGS__<KEY>` | one entry of `custom_tags` |
| `SENHUB_PROBE_<NAME>_GOVERNANCE__<KEY>` | `governance`, for example `GOVERNANCE__CRITICALITY` or `GOVERNANCE__LABELS__APPLICATION` |

Everything else is a parameter. `interval` is a parameter of each probe, so `SENHUB_PROBE_PG_INTERVAL=5m` sets it. In the rare case a parameter carries one of the names above, prefix it with `PARAMS__`: `SENHUB_PROBE_<NAME>_PARAMS__<KEY>`. `snmp_poll`, whose device block is also called `governance`, is reached that way (`PARAMS__GOVERNANCE__...`).

### Typed from the probe's schema

The value is read according to the type the probe declares for the parameter:

| Declared type | Written as |
|---|---|
| string | as is |
| integer, number | `5433`, `0.5` |
| boolean | `true`, `false` (also `1`, `0`, `yes`, `no`, `on`, `off`) |
| duration | seconds (`90`) or a duration (`30s`, `5m`) |
| list of strings | comma-separated, spaces around items ignored: `a, b, c`. A JSON array (`["a,b","c"]`) when an item holds a comma |
| mapping (headers, labels) | one entry per variable, `SENHUB_PROBE_<NAME>_HEADERS__X_REQUEST_NAME=v` (the entry name is lowercased), or the whole mapping as a JSON object, which keeps the case |
| block | one variable per field with `__`, or the whole block as a JSON object |
| list of blocks | a JSON array of objects |

A probe type unknown to this build, a parameter its schema does not declare, a value that does not fit its type or its list of accepted values, or a required parameter left out, **stops the agent at load** and the message names the variable. A probe type that has no schema (a few commercial probes) accepts string values only, with a warning in the log, and nothing is checked.

### Secrets

A parameter the schema marks secret, or whose name looks like one (`password`, `token`, `secret`, `api_key`, `dsn`, `community`), is never written into the loaded configuration: it holds a reference to the variable, and `config show` prints that reference with `--raw` and `***` by default. For a secret that lives in a file, which is how Docker, Kubernetes and Podman deliver them, add `_FILE`:

```
SENHUB_PROBE_PG_PASSWORD_FILE=/run/secrets/pg_password
```

The file is read at every start and its content trimmed. Giving both `SENHUB_PROBE_PG_PASSWORD` and `SENHUB_PROBE_PG_PASSWORD_FILE` is refused. When the schema owns a key that itself ends in `_file` (`tls.ca_file`), the exact name wins: `SENHUB_PROBE_PG_TLS__CA_FILE` is that key, and reading it from a file takes `SENHUB_PROBE_PG_TLS__CA_FILE_FILE`.

### Precedence

The variables are applied on top of the files (`agent.yaml`, `probes.d/`, and in a container the fragment of `SENHUB_PROBES`), once they are merged:

- A probe of the same name in a file keeps what the file says, **except for the parameters the environment sets, which win** (as in Grafana). Blocks are merged key by key; a list is replaced whole.
- A name found in no file creates the probe, and `SENHUB_PROBE_<NAME>_TYPE` is then required.
- A `TYPE` that differs from the type of the probe in the file is refused.

`agent config show` prints the merged result, and a first line names the `SENHUB_PROBE_*` variables the probes were read from (names, never values). `--raw` shows a secret as its `${env:...}` or `${file:...}` reference.

### Examples

A PostgreSQL probe, from nothing but the environment:

```bash
SENHUB_PROBE_PG_TYPE=postgresql
SENHUB_PROBE_PG_HOST=db.internal
SENHUB_PROBE_PG_USERNAME=monitor
SENHUB_PROBE_PG_PASSWORD_FILE=/run/secrets/pg_password
SENHUB_PROBE_PG_INTERVAL=30
SENHUB_PROBE_PG_TLS__CA_FILE=/etc/ssl/pg-ca.pem
SENHUB_PROBE_PG_GOVERNANCE__LABELS__APPLICATION=billing
```

The probe `web` of a file keeps its targets and has its interval and its state changed:

```bash
SENHUB_PROBE_WEB_INTERVAL=15
SENHUB_PROBE_WEB_ENABLED=false
```

Azure Container Apps, [list mode](probes/azure_container_apps.md), one probe per application, sharing the credentials (the probe name is yours, the application name goes in `APP`):

```bash
SENHUB_PROBE_OLTP_TYPE=azure_container_apps
SENHUB_PROBE_OLTP_APP=oltp
SENHUB_PROBE_OLTP_TENANT_ID=00000000-0000-0000-0000-000000000000
SENHUB_PROBE_OLTP_CLIENT_ID=11111111-1111-1111-1111-111111111111
SENHUB_PROBE_OLTP_CLIENT_SECRET_FILE=/run/secrets/aca_client_secret
SENHUB_PROBE_OLTP_SUBSCRIPTION_ID=22222222-2222-2222-2222-222222222222
SENHUB_PROBE_OLTP_RESOURCE_GROUP=rg-squash
# the same eight lines under SENHUB_PROBE_BILLING_ for a second application
```

Azure Container Apps, discovery mode, one probe for the whole subscription:

```bash
SENHUB_PROBE_ACA_TYPE=azure_container_apps
SENHUB_PROBE_ACA_TENANT_ID=00000000-0000-0000-0000-000000000000
SENHUB_PROBE_ACA_CLIENT_ID=11111111-1111-1111-1111-111111111111
SENHUB_PROBE_ACA_CLIENT_SECRET_FILE=/run/secrets/aca_client_secret
SENHUB_PROBE_ACA_SUBSCRIPTION_ID=22222222-2222-2222-2222-222222222222
SENHUB_PROBE_ACA_DISCOVERY__INTERVAL=300
SENHUB_PROBE_ACA_DISCOVERY__EXCLUDE=*-preview
```

## Inspecting the merged configuration

The `agent config show` command prints the final, merged configuration as YAML with map keys sorted alphabetically:

```bash
sudo /usr/local/bin/senhub-agent config show              # default: --redact
sudo /usr/local/bin/senhub-agent config show --redact     # secrets masked with ***
sudo /usr/local/bin/senhub-agent config show --resolved   # references substituted, secrets in cleartext
sudo /usr/local/bin/senhub-agent config show --raw        # references preserved
```

- `--redact` (default): resolved configuration, but with values that came from `${file:..}` or `${secret:..}`, and any value whose YAML key matches `(?i)(key|token|password|passphrase|secret|community|credential|authorization|bearer|license|jwt)`, masked with `***`. Safe to copy into a support ticket or commit to source control.
- `--resolved`: the same configuration the agent boots with — `${env:..}` / `${file:..}` / `${secret:..}` resolved against the current environment, filesystem and secret store, secrets in cleartext. Ask for it explicitly.
- `--raw`: the merged configuration BEFORE substitution. Useful for auditing the layout (which files contributed which entries) before comparing against the resolved output.

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
