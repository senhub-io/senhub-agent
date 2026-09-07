# OTLP / OpenTelemetry

SenHub Agent can push metrics, logs and traces natively over **OTLP/gRPC** to any
OpenTelemetry receiver — an OTel collector, vmagent's OTLP endpoint, a
direct VictoriaMetrics / VictoriaLogs ingest, Grafana Cloud OTLP, etc.

OTLP is a **push** path, complementary to the [Prometheus pull
endpoint](prometheus/index.md). Both signals carry the
**same metric names, types, units, and attributes** because both are
produced by the same neutral OTel mapper internally — operators
querying Prometheus today and switching to OTLP push tomorrow do not
rewrite a single PromQL query.

## Quick start

The fastest path is to provision OTLP at install time — one flag writes a
ready-to-use `strategies.d/10-otlp.yaml`:

```bash
senhub-agent config init --otlp-endpoint otel-collector.internal:4317
# direct to a native VictoriaMetrics / Grafana Alloy OTLP/HTTP endpoint:
senhub-agent config init --otlp-endpoint vm.internal:4318 --otlp-protocol http
```

This enables metrics + logs export out of the box. To wire it by hand instead:

1. Add an `otlp` storage block to your config:

```yaml
storage:
  - name: otlp
    params:
      endpoint: "otel-collector.internal:4317"   # required
      tls:
        enabled: true
      signals:
        metrics:
          enabled: true
          interval: 30s
        logs:
          enabled: true
```

2. Restart the agent. The agent connects lazily — a missing collector
   does not block startup; failures appear as retried export attempts.

3. Verify the collector is receiving data:

```bash
# At the collector side
journalctl -u otel-collector -f
# or query VictoriaMetrics directly
curl -G http://vm:8428/api/v1/query \
  --data-urlencode 'query=count by(__name__) ({service_name="senhub-agent"})'
```

## Configuration

The full set of knobs (every key maps directly to an OTel SDK option):

```yaml
storage:
  - name: otlp
    params:
      # Required: gRPC endpoint of the receiver (no scheme prefix).
      endpoint: "otel-collector.internal:4317"

      # Optional multi-tenant routing: sugar for the X-Scope-OrgID header
      # (the standard tenant key for Mimir / Loki / Tempo / VictoriaMetrics).
      # "org_id" is accepted as an alias. Env-expandable.
      tenant: "acme"

      # Optional headers (for example bearer auth at the gateway).
      headers:
        Authorization: "Bearer YOUR-INGEST-TOKEN"

      # TLS — defaults to enabled. Disable explicitly for plaintext
      # localhost / lab environments.
      tls:
        enabled: true                 # default true
        insecure_skip_verify: false   # opt-in for self-signed certs in test
        ca_file: /etc/ssl/private/otlp-ca.pem
        cert_file: /etc/ssl/private/agent.pem    # mTLS, optional
        key_file:  /etc/ssl/private/agent.key    # required if cert_file set

      # Base path for backends that serve OTLP under a prefix rather than
      # at the root (see below). OTLP/HTTP only.
      url_path_prefix: "/api/v2/otlp"

      # Close an idle HTTP connection before the ingress does (see below).
      # OTLP/HTTP only; unset keeps the Go default of 90s.
      idle_conn_timeout: 45s

      compression: gzip               # gzip | none — default gzip
      timeout: 10s                    # per-export deadline

      retry:
        enabled: true
        initial_interval: 5s
        max_interval: 30s
        max_elapsed_time: 1m

      signals:
        metrics:
          enabled: true
          interval: 30s               # push cadence
          temporality: cumulative     # cumulative | delta — default cumulative
        logs:
          enabled: true
          batch_size: 1000            # max records per gRPC export
          batch_timeout: 5s           # flush even if batch_size not reached
          buffer_size: 10000          # bounded queue; drop-oldest beyond
        traces:
          enabled: true               # default false — relays spans ingested by an
                                      # otlp_receiver probe (signals: [traces])
          batch_size: 512
          batch_timeout: 5s
          buffer_size: 2048           # bounded queue; drop beyond
          sample_ratio: 1.0           # head sampling, 0.0-1.0
          relay_enrichment: true      # default true; add agent correlation
                                      # context to relayed spans (see below)

      # How many exports may be in flight at once. The push splits a
      # large cycle per probe and ships the parts in parallel; 1 means
      # the single-batch path. Accepted range 1-64.
      max_concurrent_exports: 4       # default 4

      # Cap the memory the metric store may use, in MiB of Go heap.
      # Past the soft limit the store keeps its existing series and
      # refuses new ones; past the hard limit it refuses all writes and
      # forces a collection. 0 disables either threshold.
      memory_limit:
        soft_mib: 200                 # default 200
        hard_mib: 400                 # default 400
        check_interval: 5s            # default 5s

      # Survive a restart: the last value of every series is written to
      # disk and restored at boot, so cumulative counters continue
      # instead of resetting.
      persistence:
        enabled: false                # default false
        path: /var/lib/senhub-agent/otlp
        interval: 30s                 # default 30s
        # Disk cap for the logs dead-letter queue, which holds batches
        # the receiver could not take during an outage. Past it the
        # oldest batches are evicted. 0 keeps the default.
        logs_queue_max_bytes: 134217728   # default 128 MiB

      # Resource attributes attached to every emitted batch. Defaults
      # are derived from agent identity if omitted.
      resource:
        service.name: senhub-agent             # default
        service.instance.id: <agent-key prefix> # default: first 8 chars of agent.key
        deployment.environment: prod
        # Any additional keys are passed through as resource attributes.
        k8s.cluster.name: edge-01
```

### `tenant` (optional multi-tenant routing)

Sets the **`X-Scope-OrgID`** header — the standard tenant key used by Mimir,
Loki, Tempo and VictoriaMetrics — on every signal. `org_id` is accepted as an
alias; an explicit `X-Scope-OrgID` in `headers:` takes precedence. The value is
env-expandable (`tenant: "${env:ORG_ID}"`).

Ingest authentication is enforced at the **collector/edge** (a standard OTLP
authenticator: bearer token, basic auth, OIDC or mTLS), not by the agent. A
self-hosted edge trusts the agent's `tenant`; an untrusted multi-tenant edge
re-derives it from the authenticated token. **Your agent license does not gate
sending data** — it only governs which paid probes may collect; an unlicensed
agent can still push over OTLP.

### `endpoint` (required)

`host:port` of the OTLP/gRPC receiver. No scheme prefix (it's gRPC).
Default OTLP/gRPC port is **4317**. There is intentionally **no
default** for this field — silently shipping data to localhost when
the operator forgets to set it would be a worse failure mode than
refusing to start.

A standby ingress can be listed under `fallback_endpoints`: the agent prefers
the primary, switches on a failed export and returns on its own once the
primary recovers. See
[the backpressure guide](https://github.com/senhub-io/senhub-agent/blob/master/docs/admin-guide/BACKPRESSURE.md)
for the shape and the trade-offs.

### `url_path_prefix` (OTLP/HTTP only)

`endpoint` is a `host:port` pair, so it cannot carry a path. Backends that
serve OTLP under a base path instead of at the root need this prefix, which
the agent prepends to the standard signal paths: `/api/v2/otlp` yields
`/api/v2/otlp/v1/metrics`, `/api/v2/otlp/v1/logs` and `/api/v2/otlp/v1/traces`.

Setting it with `protocol: grpc` is refused at config load rather than
silently ignored: the gRPC transport addresses services, not URL paths.

Pushing straight to Dynatrace, which serves OTLP at `/api/v2/otlp`, expects
delta temporality and authenticates with an API token:

```yaml
otlp:
  protocol: http
  endpoint: "abc12345.live.dynatrace.com:443"
  url_path_prefix: "/api/v2/otlp"
  tls:
    enabled: true
  headers:
    Authorization: "Api-Token ${env:DT_API_TOKEN}"
  signals:
    metrics:
      enabled: true
      temporality: delta
    logs:
      enabled: true
```

### `idle_conn_timeout` (OTLP/HTTP only)

Load balancers and reverse proxies close connections that have been idle for
some time. A signal that pushes continuously never reaches that point, but a
sparse one does: the agent then discovers the connection is gone only when it
tries to use it, and pays a failed request before reconnecting. Logs are the
signal this affects, since metrics push on a fixed interval and keep their
connection warm.

Setting `idle_conn_timeout` **below your ingress idle timeout** makes the
agent close first, turning that failure into a clean reconnect. Unset, the Go
default of 90 seconds applies, which is longer than most ingress timeouts.

The cost is one extra TCP and TLS handshake per idle period, on a signal that
by definition is not busy. Setting it with `protocol: grpc` is refused at
config load: gRPC connection keepalive is a separate mechanism.

### `tls`

Default is `enabled: true`. For **production push to a remote
collector you should always keep TLS enabled**. The plaintext path is
provided only for localhost / lab tests where the gRPC traffic never
leaves the loopback interface.

mTLS is supported by setting both `cert_file` and `key_file`. Setting
only one is rejected at config validation.

### `signals.metrics` and `signals.logs`

Each signal can be enabled/disabled independently. Disabling one stops
the corresponding pipeline and keeps the gRPC connection idle.

`temporality` defaults to **cumulative** (the OTel default since
OTel v1.0). Cumulative means counter values are reported as absolute
totals since the agent start; consumers that prefer deltas (some
Datadog setups, vmagent ingest) can set `temporality: delta`.

### `signals.traces`

The agent produces no spans of its own. The traces signal relays spans
received by an [otlp_receiver probe](probes/otlp-receiver.md) configured
with `signals: [traces]`; without such a probe there is nothing to
export and the signal stays idle.

**Correlation enrichment (`relay_enrichment`, default `true`).** Relayed
spans keep the emitting application's own identity (`service.name`,
`service.instance.id`, `host.*`) — the agent never overwrites it. On top of
that, the agent inserts its own tenancy context so you can pivot from an app
trace to the infrastructure telemetry of the same tenant: your `global_tags`
(for example `tenant`, `site`) and `deployment.environment` are added **only
if the application didn't already set that key**. Only standard /
operator-defined attributes are used — no product-specific keys.

Set `relay_enrichment: false` for a verbatim pass-through. When a single
agent relays traffic for several clients (a shared gateway), assign each
source its own tenant with per-source rules:

```yaml
        traces:
          enabled: true
          relay_tenant_overrides:
            - match: { key: "service.namespace", value: "client-b" }
              tags: { tenant: "client-b", site: "lyon" }
```

A rule applies to relayed spans whose resource attribute `key` equals
`value`; its `tags` are inserted (only when absent) instead of the default
`global_tags`.

!!! note
    `service.instance.id` is **not** a join key between the agent's own
    metrics/logs and a relayed third-party trace — they are different
    services. The reliable cross-signal pivot is **tenant/site**, i.e. "this
    app trace is slow → show the infra telemetry of the same tenant".

### `signals.entities`

Entity events (the infrastructure graph: hosts, interfaces, services and
their relationships) ride the OTLP **log** signal, so this signal has no
endpoint or batch knobs of its own — it reuses the log transport.

```yaml
    entities:
      enabled: true            # opt-in (default false)
      interval: 60s            # heartbeat cadence; also the liveness backstop
      buffer_size: 256         # bounded queue; drop-oldest beyond
      depends_on_enabled: false  # opt-in (default false): outbound dependency
                                 # edges. Needs root — see below
      depends_on_debounce: 3   # consecutive scrapes before an outbound
                               # dependency edge is emitted (>= 1, default 3)
      depends_on_exclude_cidrs: []  # peer ranges dropped before anything is
                                    # emitted (operator privacy filter)
      redact_attributes: []    # attribute keys DROPPED from every entity
                               # event before export (default: none)
      governance:              # operator-asserted ownership / criticality /
        criticality: low       # location / lifecycle, stamped on the entities
        lifecycle: active      # this agent emits (default: none)
        owner:
          team: sre
          contact: sre@example.com
        location:
          site: paris
          rack: R12
        labels:
          environment: staging
```

`governance` stamps operator-asserted facts on the entities this agent emits —
who owns them, how critical they are, where they physically sit, and where they
are in their lifecycle. The agent cannot discover any of it; it comes from you.
A topology consumer can then filter or group on those keys, so an incident view
can be narrowed to one team's critical estate without touching the agent
configuration on every host.

`criticality` takes `critical` / `high` / `medium` / `low`, and `lifecycle`
takes `active` / `maintenance` / `decommissioning` / `retired`. `labels` is a
free key/value map for anything that is yours alone.

This block describes the host. Its location alone (`site`, `datacenter`,
`rack`, `room`) also descends to what the agent places on this host, a
database or a service reached on the loopback address; owner, criticality,
lifecycle and labels stay with the host. What a probe observes (a database, a
device, a remote application) is governed on the probe entry instead, with the
same vocabulary: see [Governance per probe](configuration.md#governance-per-probe).

`depends_on_enabled` turns on outbound dependency discovery — the edges that
say "this service talks to that endpoint". It is off by default because mapping
a host's connections can be privacy-sensitive.

!!! warning "Outbound dependency discovery needs root"

    Mapping a socket to the process that owns it reads `/proc/<pid>/fd`, and
    only the owner may read it. The agent runs as a service account by
    default, so it can attribute its **own** connections and nothing else —
    every other service's dependencies are invisible.

    On a non-root install the agent says so once at startup, naming the
    counts and the account, and reports its view as failed rather than
    reporting an empty graph as fact. Leave the option off, or run the agent
    as root if you want the whole host's dependencies.

`depends_on_debounce` controls how durable an outbound connection must be
before it appears as a `depends_on` edge: a peer endpoint must be seen on
this many consecutive emission scrapes before its edge is emitted, which
keeps ephemeral connections out of the graph. The latency to surface a
dependency is `depends_on_debounce x interval` (so the default `3 x 60s` is
about three minutes); lower it for a more responsive graph, raise it to
filter out shorter-lived connections.

The tolerance is symmetric: an edge that took `depends_on_debounce` scrapes to
appear survives the same number of missed ones before it is given up. A
long-lived connection the socket table happens to miss once is not a dependency
that ended, and retracting it on a single miss would reach a topology consumer
as an edge flapping in and out.

`redact_attributes` lists descriptive attribute keys the agent removes
from every entity event before export — useful when the entity stream
transits a shared collector or a third-party OTLP pipeline that should
not see inventory identifiers such as hardware serials or cloud account
ids:

```yaml
      redact_attributes: [hw.serial_number, cloud.account.id]
```

The listed keys are **dropped, not masked**: they simply never appear on
the wire (an entity state is a full snapshot, so absence is the clean
form of redaction — a `***` placeholder would pollute the graph with a
fake shared value). The filter applies to every entity this strategy
emits — the host entity and probe-emitted entities alike — and is
**per-strategy**: a second `otlp` strategy pointed at a trusted backend
can keep the full attribute set while this one redacts.

Two rules to know:

- **Identity keys cannot be redacted.** Keys that identify an entity
  (`host.id`, `service.instance.id`, `container.id`,
  `network.device.id`, `db.instance.id`, `vmid`) are refused at config
  load with an error — removing an entity's identity would destroy the
  entity, not protect it. Any other key is accepted.
- **Trade-off on `hw.serial_number`.** The hardware serial is the
  evidence an entity backend uses to reconcile the in-band host entity
  with an out-of-band BMC (Redfish) view of the same machine. Redacting
  it disables that `same_as` reconciliation for this export — list it
  only when the receiving backend must not see serials.

### `resource`

Resource attributes are attached **once per batch** by the SDK
(per-batch in OTLP wire format) and are how observability backends
group series and logs into entities. Defaults:

- `service.name` = `"senhub-agent"`
- `service.instance.id` = first 8 chars of the agent authentication
  key (avoids leaking the full secret to backends while keeping
  per-agent disambiguation)
- `service.version` = the agent build version (when known)

Any additional key/value pair under `resource:` is passed through
verbatim — useful for `k8s.*`, `cloud.*`, `host.*`, or your own
custom labels.

## Naming alignment with Prometheus

Metrics emitted via OTLP carry the **same internal names** as the
Prometheus exposition. The path is:

```
probe data → otelmapper.Resolve → []OtelRecord → { Prometheus serializer
                                                  OTLP exporter }
```

Both consumers read from the same `OtelRecord` stream. Prometheus
applies its sink-specific conventions (`senhub_` prefix, `_seconds`
suffix, dots → underscores), and OTLP keeps the dotted OTel names
intact. The OTel collector / VictoriaMetrics / Grafana that ingests
the OTLP push automatically applies the same transforms when storing
into a Prometheus-compatible TSDB, so the **observed metric names are
identical** at the query layer.

| OTel name (OTLP wire) | Prometheus exposition | After OTLP→PromQL ingest in VM |
|---|---|---|
| `system.cpu.utilization` (gauge, `1`, `cpu.mode=user`) | `senhub_system_cpu_utilization_ratio` | `system_cpu_utilization_ratio` |
| `system.memory.usage` (updowncounter, `By`) | `senhub_system_memory_usage_bytes` | `system_memory_usage_bytes` |
| `system.network.io` (gauge, `By/s`, `network.io.direction=receive`) | `senhub_system_network_io_bytes_per_second` | `system_network_io_bytes_per_second` |
| `senhub.netscaler.lbvserver.connections.active` | `senhub_netscaler_lbvserver_connections_active` | `senhub_netscaler_lbvserver_connections_active` |

The collector applies the standard `senhub_` prefix loss when going
through `prometheusremotewrite`, but the **dimensions** (probe_name,
probe_type, semantic-convention attributes like `cpu.mode`,
`system.memory.state`, `hw.state`) flow through identically. Aliasing
both paths under one PromQL query is straightforward.

## Logs signal

The OTLP strategy ships logs from these probe sources:

- **`syslog` probe** — every received syslog message becomes an OTel
  log record with severity mapped per the standard RFC 5424 → OTel
  table (info → INFO, error → ERROR, emergency → FATAL4, etc.) and
  attributes `syslog.facility`, `syslog.hostname`, `syslog.appname`,
  …
- **`event` probe** — every accepted HTTP event becomes a log record
  with severity from the event's `severity` field (EMERG..DEBUG)
  and the payload fields under the `senhub.event.*` namespace.
- **`linux_logs` probe** — reads the local systemd journal via
  `journalctl --output=json --follow` and emits a log per entry
  with `host.name`, `systemd.unit`, `syslog.appname`,
  `process.pid`, … See [linux_logs probe](probes/linux-logs.md).
- **`filetail` probe** — each line tailed from a watched log file
  becomes a log record.
- **`windows_eventlog` probe** — each entry read from a subscribed
  Windows Event Log channel becomes a log record.
- **`snmp_trap` probe** — each received SNMP trap becomes a log
  record.
- **`otlp_receiver` probe** with `signals: [logs]` — relays log
  records ingested over OTLP from other emitters.

Probes still emit DataPoints to the existing PRTG/Nagios/event
strategies; the logs path is **additive**.

## Severity mapping

| Source value | OTel SeverityNumber | OTel SeverityText |
|---|---|---|
| Syslog 0 — emergency | 24 | FATAL4 |
| Syslog 1 — alert     | 23 | FATAL3 |
| Syslog 2 — critical  | 22 | FATAL2 |
| Syslog 3 — error     | 17 | ERROR |
| Syslog 4 — warning   | 13 | WARN |
| Syslog 5 — notice    | 10 | INFO2 |
| Syslog 6 — info      | 9  | INFO |
| Syslog 7 — debug     | 5  | DEBUG |

Same table is used by `linux_logs` (PRIORITY field of the journal
entry) and the `event` probe (when the operator-supplied severity
string maps to one of the eight standard names).

## Routing probes to OTLP

Every metric probe routes to OTLP automatically when an `otlp` storage
is configured — the strategy is included in the default target list
alongside `senhub`, `prtg`, `http`. There's nothing to wire per probe.

The data_store router silently skips strategies that aren't
configured, so probes that target `[senhub, prtg, http, otlp]` continue
to work in agents where the operator has not enabled OTLP.

## Receiver setups

### OpenTelemetry Collector (recommended)

A minimal collector that accepts OTLP/gRPC and forwards to
VictoriaMetrics + VictoriaLogs:

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: "0.0.0.0:4317"

exporters:
  prometheusremotewrite:
    endpoint: "http://vm.internal:8428/api/v1/write"
    resource_to_telemetry_conversion:
      enabled: true

  otlphttp/logs:
    logs_endpoint: "http://victorialogs.internal:9428/insert/opentelemetry/v1/logs"

service:
  pipelines:
    metrics:
      receivers: [otlp]
      exporters: [prometheusremotewrite]
    logs:
      receivers: [otlp]
      exporters: [otlphttp/logs]
```

`resource_to_telemetry_conversion: enabled: true` ensures the OTel
resource attributes (service.name, service.instance.id, etc.) become
Prometheus labels rather than being discarded — usually what operators
want.

### Single public intake for a fleet (collector-fronted)

The recommended pattern for a fleet of agents across sites: every agent —
local or remote — pushes its whole OTLP stream (metrics, logs, entities) to
**one** public HTTPS endpoint, fronted by a reverse proxy that terminates TLS
and forwards to a central collector. The collector authenticates the bearer
token and fans out to the storage backends.

Agent-side, the entire client configuration is one strategy fragment:

```yaml
# strategies.d/10-otlp.yaml
otlp:
  endpoint: "ingest.example.com:443"
  tls:
    enabled: true
  headers:
    Authorization: "Bearer ${env:OTLP_BEARER_TOKEN}"
  compression: gzip
  timeout: 60s
  signals:
    metrics:
      enabled: true
      interval: 30s
    logs:
      enabled: true
    entities:
      enabled: true
      interval: 5m
  resource:
    service.name: my-host-prod        # one value per agent
    deployment.environment: prod
```

Notes:

- **One URL, one token, four signals** (metrics, logs, traces,
  entities). Entity events ride the logs
  transport — enabling `signals.entities` requires no extra endpoint.
- The bearer token comes from the environment (`${env:}`) or a root-only
  file (`${file:/etc/senhub-agent/bearer.token}`); never inline it.
- `host.id`, `host.name` and `os.*` resource attributes are auto-detected
  and attached to every signal — don't set them manually.
- The `resource: service.name` override groups **telemetry** only. The
  agent's own `service.instance` entity always carries
  `service.name: senhub-agent`, whatever the override says, so a fleet
  inventory filtered on that name sees every agent. The per-host label
  stays available on the telemetry resource and on the host entity.
- gRPC works through standard reverse proxies. nginx needs one location for
  OTLP/gRPC and one for OTLP/HTTP (the agent uses the standard `/v1/*`
  paths):

```nginx
server {
    listen 443 ssl;
    server_name ingest.example.com;

    # OTLP/gRPC -> collector
    location /opentelemetry.proto.collector. {
        grpc_pass grpcs://127.0.0.1:4317;
    }
    # OTLP/HTTP -> collector
    location /v1/ {
        proxy_pass http://127.0.0.1:4318;
    }
}
```

Authentication belongs on the collector (for example the `bearertokenauth`
extension), keeping the proxy a pure transport. To feed a second consumer
(another OTLP backend, an entity-graph store, ...), fan out **at the
collector** — add a second exporter there — rather than configuring a
second OTLP strategy on every agent.

### vmagent's native OTLP endpoint

vmagent v1.95+ ingests OTLP/gRPC directly. Configure the agent to
push at `vmagent.internal:4317` — no collector needed.

### Grafana Cloud OTLP

Grafana Cloud accepts OTLP push at a public gRPC endpoint. Get the
endpoint URL and ingest token from the Grafana Cloud console:

```yaml
storage:
  - name: otlp
    params:
      endpoint: "otlp-gateway-prod-eu-west-2.grafana.net:443"
      headers:
        Authorization: "Basic eW91...="   # from Grafana Cloud console
      tls:
        enabled: true
```

## Agent self-observability

The agent's existing `senhub_agent_*` metrics (uptime, probe counts,
collect errors, HTTP requests, build info) are emitted on the
**Prometheus path only** — the OTLP path does not push agent self-
metrics, to avoid feedback loops where a failing OTLP export would
itself emit metrics that need to go through OTLP.

The collector's own self-metrics (`otelcol_exporter_sent_*`,
`otelcol_receiver_accepted_*`) cover the OTLP transport health from
the receiver side.

## Monitoring the OTLP pipeline

The agent maintains its own snapshot of OTLP pipeline counters that
you can read without scraping Prometheus, via three surfaces:

- **CLI** — `senhub-agent status --otlp` appends a four-section block
  (Pipeline / Store & Export / Checkpoint / Parallel) to the standard
  status view. See the [CLI reference](cli.md#status).
- **Web dashboard** — when the HTTP strategy is enabled the dashboard
  renders an **OTLP Pipeline** card with the same four sections,
  refreshed every 30 seconds. See [Web Interface](web-interface.md#otlp-pipeline-card).
- **JSON endpoint** — `GET /api/{agentkey}/info/otlp` returns the same
  data as a single JSON snapshot. Useful for custom dashboards or
  external alerting. The field reference lives in
  [OTLP observability](https://github.com/senhub-io/senhub-agent/blob/dev/docs/admin-guide/OTLP-OBSERVABILITY.md) (admin guide).

### What to look at

| Symptom | What to check |
|---|---|
| Operator dashboards lag the agent by minutes | `Export last` and `Export mean` durations — sustained values close to the configured `timeout` (default 60 s) mean the sink is the bottleneck. |
| Series are missing without an explicit error | `Dropped` total and its `by_reason` breakdown. `probe_cardinality` = the per-probe budget was hit; `store_cap` = the global cap; `memory_soft_limit`/`memory_hard_limit` = the agent shed load to stay under its memory budget. |
| Recovery after a restart looks slow | `Checkpoint.Restored entries` — if 0 with persistence enabled, the checkpoint wasn't found or failed to parse (check `Errors` by stage). |
| Throughput plateaus despite headroom on the collector | `Parallel.Sub-batches` — value of 1 means the single-batch path; > 1 means the per-probe fan-out is firing. |

### Cardinality controls

If `Dropped by probe_cardinality` keeps climbing, lift the per-probe
budget in your strategy YAML:

```yaml
otlp:
  endpoint: "otel-collector.internal:4317"
  max_active_series_per_probe: 20000   # default 10000
  max_store_size: 100000               # default 50000 (global cap)
  staleness_ttl: "10m"                 # default 10m: a series with no new
                                       # datapoint for this long is evicted
                                       # instead of re-exporting forever
                                       # (zombie series); "0s" disables
```

Both knobs are gauges of last resort — they protect the agent's memory
under runaway-cardinality conditions. Raise them only when you know
what's generating the series, otherwise the agent will happily eat
RAM tracking churning timeseries.

## Backward compatibility

- **Existing storages** (PRTG, Nagios, SenHub, HTTP/Prometheus) are
  unchanged. Adding an `otlp` block is purely additive.
- **Probe configurations** (`type: cpu`, `type: redfish`, etc.) work
  identically. The OTel mapping comes from the bundled YAML
  transformer definitions.
- **No new probe types are required to push to OTLP** — every existing
  probe routes through the OTLP strategy when it's configured.

## Troubleshooting

### Agent starts but nothing reaches the collector

Check the agent log for:

```
strategy.otlp WRN OTLP metrics export failed   error=...
```

Common causes:

- **Wrong endpoint or unreachable host**. The agent uses lazy gRPC
  connect — the failure surfaces only on the first push attempt
  (after `metrics.interval`).
- **TLS handshake failure**. Set `insecure_skip_verify: true`
  temporarily to confirm certificate verification is the cause.
- **Authentication rejected by gateway**. Check the collector / vmagent
  / Grafana Cloud auth log; verify the `headers:` block.

### Metrics appear with unexpected names in VictoriaMetrics

The default VictoriaMetrics OTLP-to-Prometheus translation strips
the `senhub_` prefix you might see in the Prometheus exposition.
That's intentional — the OTLP path uses the canonical OTel names
(`system.cpu.utilization`, not `senhub_system_cpu_utilization`).
Add `resource_to_telemetry_conversion: enabled: true` to ensure
resource attributes (service.name, deployment.environment) are
preserved as labels.

### Some metrics are missing

Check the agent log for:

```
strategy.otlp WRN Metric has no OTel mapping — not exported via OTLP.
```

Same policy as the Prometheus path: a probe metric without an
`otel:` block in its YAML definition is silently skipped, and the
agent warns once per `(probe_type, metric_name)`. Add the mapping or
`otel.skip: true` directive in `definitions/<probe>.yaml` and
restart.

### Logs aren't arriving

Verify:

1. `signals.logs.enabled: true` in the storage params.
2. The collector pipeline includes the `otlp` receiver in the `logs:`
   pipeline (not just `metrics:`).
3. The probes you expect to produce logs are configured: `syslog`,
   `event`, `linux_logs`, `filetail`, `windows_eventlog`, `snmp_trap`,
   or an `otlp_receiver` with `signals: [logs]`. Other probes do not
   produce log records.
4. The receiver tolerates the OTel log data model; older versions of
   vmagent or Loki collectors may need a recent build.

### Reverse the agent → push direction

If you want a pull-based scrape model instead of push, use the
[Prometheus endpoint](prometheus/index.md) — same data,
same names, different transport.
