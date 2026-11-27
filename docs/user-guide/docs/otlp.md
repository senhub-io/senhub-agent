# OTLP / OpenTelemetry

SenHub Agent can push metrics, logs and traces natively over **OTLP/gRPC** or **OTLP/HTTP** to any
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

1. Create `strategies.d/10-otlp.yaml`:

```yaml
otlp:
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

An installation that still uses the legacy monolithic `agent-config.yaml`
holds the same parameters under `storage:`, in the `params` of an entry
named `otlp`.

2. Save the file: the running agent picks the output up without a
   restart. It connects lazily, so a collector that is not reachable yet
   does not block anything; failures appear as retried export attempts.

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
# strategies.d/10-otlp.yaml
otlp:
  # Required: host:port of the receiver, no scheme prefix.
  endpoint: "otel-collector.internal:4317"

  # Transport: grpc (OTLP/gRPC, port 4317) or http (OTLP/HTTP protobuf,
  # port 4318). "http/protobuf" is accepted as an alias of http.
  protocol: grpc                  # default grpc

  # Standby receivers, tried in order when an export to the primary
  # fails; the agent returns to the primary once it recovers.
  fallback_endpoints:
    - "otel-collector-standby.internal:4317"

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
  timeout: 60s                    # per-export deadline — default 60s

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
      max_export_bytes: 3145728   # estimated size cap of one export (default 3 MiB);
                                  # larger batches are split between records,
                                  # halved again if the receiver refuses the size
    traces:
      enabled: true               # default false — relays spans ingested by an
                                  # otlp_receiver probe (signals: [traces])
      batch_size: 512
      batch_timeout: 5s
      buffer_size: 2048           # bounded queue; drop beyond
      sample_ratio: 1.0           # head sampling, 0.0-1.0

  # Telemetry forwarded on behalf of applications (otlp_receiver).
  relay:
    enrichment: true              # default true; add the agent's context
                                  # to relayed telemetry (see below)

  # Cardinality caps and staleness (see "Cardinality controls").
  max_store_size: 50000           # default 50000 series in all; 0 = unbounded
  max_active_series_per_probe: 10000  # default 10000 per probe; 0 = no per-probe cap
  staleness_ttl: 10m              # default 10m; a series silent this long is
                                  # evicted. 0s disables eviction
  keepalive_interval: 4m          # default 4m; a gauge from a probe that
                                  # runs less often than this is exported
                                  # again at this interval. 0s exports
                                  # each measurement once

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
    enabled: true                 # default true: the logs, metrics and traces
                                  # queues are on in the state directory; the
                                  # checkpoint stays off until `path` is set;
                                  # enabled: false turns everything off
    path: /var/lib/senhub-agent/otlp   # checkpoint and queues directory;
                                  # empty = no checkpoint, queues in the state
                                  # directory (see "Logs, metrics and traces
                                  # survive an outage")
    interval: 30s                 # default 30s: how often the checkpoint is
                                  # written (atomic rewrite with fsync, even when
                                  # nothing changed); raise it on a host where
                                  # disk writes matter, at the cost of older
                                  # values restored after a crash
    # Disk cap for the logs dead-letter queue, which holds batches
    # the receiver could not take during an outage. Past it the
    # oldest batches are evicted. 0 keeps the default.
    logs_queue_max_bytes: 134217728   # default 128 MiB
    # Batches that waited longer than this are dropped, at boot and
    # every 10 minutes. 0 = no age limit.
    logs_queue_max_age: 24h       # default 24h
    # The same two bounds for the metrics and traces queues, which keep
    # the export requests the collector did not take. 0 keeps the default
    # size; 0 age = no age limit.
    metrics_queue_max_bytes: 268435456   # default 256 MiB
    metrics_queue_max_age: 24h    # default 24h
    traces_queue_max_bytes: 67108864     # default 64 MiB
    traces_queue_max_age: 24h     # default 24h

  # Resource attributes attached to every emitted batch. Defaults
  # are derived from agent identity if omitted.
  resource:
    service.name: senhub-agent             # default
    service.instance.id: <uuid>             # default: an RFC 4122 UUID derived from agent.key
    deployment.environment: prod
    # Any additional keys are passed through as resource attributes.
    k8s.cluster.name: edge-01
```

### `tenant` (optional multi-tenant routing)

Sets the **`X-Scope-OrgID`** header — the standard tenant key used by Mimir,
Loki, Tempo and VictoriaMetrics — on every signal. `org_id` is accepted as an
alias; an explicit `X-Scope-OrgID` in `headers:` takes precedence. The value is
env-expandable (`tenant: "${env:ORG_ID}"`). `config show --redact` and the
configuration reported to a fleet server show an `X-Scope-OrgID` header as it is
and mask every other header value.

Ingest authentication is enforced at the **collector/edge** (a standard OTLP
authenticator: bearer token, basic auth, OIDC or mTLS), not by the agent. A
self-hosted edge trusts the agent's `tenant`; an untrusted multi-tenant edge
re-derives it from the authenticated token. **Your agent license does not gate
sending data** — it only governs which paid probes may collect; an unlicensed
agent can still push over OTLP.

### `endpoint` (required)

The endpoint of the OTLP receiver, written either as `host:port` or as
`http(s)://host:port`. Any scheme is stripped before use; when
`tls.enabled` is not set, `http://` disables TLS and `https://` enables
it. A scheme that contradicts an explicit `tls.enabled`, as well as a URL
path, query, or embedded credentials, is rejected with a configuration
error. `protocol` selects the transport: `grpc` (the default, OTLP/gRPC,
usual port **4317**) or `http` (OTLP/HTTP protobuf, usual port **4318**).
With `http`, the agent builds the URL itself: `https://` when
`tls.enabled` is true, `http://` otherwise, then the standard
`/v1/metrics`, `/v1/logs` and `/v1/traces` paths. There is intentionally
**no default** for this field — silently shipping data to localhost when
the operator forgets to set it would be a worse failure mode than
refusing to start.

A standby ingress can be listed under `fallback_endpoints`: the agent prefers
the primary, switches on a failed export and returns on its own once the
primary recovers. This covers metrics, logs and traces, the relayed signals
included, and the backlog of each queue is replayed to whichever endpoint is up. See
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

**Correlation enrichment (`relay.enrichment`, default `true`).** Relayed
spans keep the emitting application's own identity (`service.name`,
`service.instance.id`, `host.*`) — the agent never overwrites it. On top of
that, the agent inserts its own tenancy context so you can pivot from an app
trace to the infrastructure telemetry of the same tenant: your `global_tags`
(for example `tenant`, `site`) and `deployment.environment` are added **only
if the application didn't already set that key**. Only standard /
operator-defined attributes are used — no product-specific keys.

Set `relay.enrichment: false` for a verbatim pass-through. The older
spelling `signals.traces.relay_enrichment` is still read, so a host that set
it keeps its behaviour, but `relay.enrichment` is the key to write: it covers
every relayed signal, not traces alone. When a single
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
endpoint or batch knobs of its own — it reuses the log transport. What the
agent reports, what is off by default and how a consumer reads a deletion
are on the [Topology](topology.md) page.

Because they are log records, a collector that forwards every log record
to a log store stores the entity events too, where they have no message
and inflate the volume. Split them on their event name in the collector,
as shown in [OpenTelemetry Collector](#opentelemetry-collector-recommended):
`entity.state` and `entity.delete` go to the topology backend, every other
record to the log store.

When `entities.enabled` is off, the output logs one warning at start,
`entity emission is off on this OTLP output`, so an empty topology can be
traced to its cause from the agent log.

```yaml
    entities:
      enabled: true            # opt-in (default false)
      interval: 60s            # heartbeat cadence; also the liveness backstop
      buffer_size: 256         # bounded queue; drop-oldest beyond
      depends_on_enabled: false  # opt-in (default false): outbound dependency
                                 # edges. Needs root or two capabilities — see below
      depends_on_debounce: 3   # scrapes (not necessarily consecutive) before an outbound
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

This block describes the host, and descends from it, on two rules that
differ because the two halves of the vocabulary answer different questions.

The location (`site`, `datacenter`, `rack`, `room`) says where a thing
physically is. What runs on this host is where the host is, so it descends to
every entity the agent places here: a database on the loopback address, a
container, a listening port.

Ownership (`owner`, `criticality`, `lifecycle`) says who answers for a thing.
It descends to the entities an operator reasons about, a service instance, a
container, a database, a network device, and not to a listening port, which
reaches its host in one `runs_on` hop: the graph already answers "who owns
this port" by traversal rather than by copying the fact onto several hundred
entities. Criticality descends knowing it errs high, since a backup daemon on
a critical host is not itself critical; claiming too much is safer than
claiming too little, and a probe that knows better overrides it.

`labels` do **not** descend, and the head of an application chain is why.
"This host is part of application X" is not "everything running on this host
is application X". That holds on a dedicated machine and fails on a shared
one, and a cluster node is the shared case: on a host carrying one application
label, thirteen entities belonging to five different sets would have inherited
it, so a filter that returns exactly the chain would have returned a mixture.
Labels stay at host grain. Put one on a probe entry when you want it on what
that probe reports.

Neither descends to anything remote. A probe reading a database on another
machine reports an entity that does not run here, and giving it this host's
owner would be wrong rather than merely noisy. What a probe observes is
governed on the probe entry instead, with the same vocabulary: see
[Governance per probe](configuration.md#governance-per-probe). A value the
probe sets itself always wins over what it would inherit.

These attributes are the producer's facts, in the frozen `entity.*` and
semantic-convention vocabulary. An annotation a consumer adds beside them is a
comment, never a correction: once the agent asserts an owner, the way to change
it is this configuration.

!!! note "These keys are a fallback"

    The `depends_on_*` keys and `interval` below are read only when the
    configuration has no top-level `entities:` block. When that block
    exists, even with `enabled: false`, it wins and these keys are ignored.
    See [Entities Section](configuration.md#entities-section).

`depends_on_enabled` turns on outbound dependency discovery — the edges that
say "this service talks to that endpoint". It is off by default because mapping
a host's connections can be privacy-sensitive.

!!! warning "Outbound dependency discovery needs privileges"

    Mapping a socket to the process that owns it reads `/proc/<pid>/fd`, and
    only the owner may read it. The agent runs as a service account by
    default, so it can attribute its **own** connections and nothing else —
    every other service's dependencies are invisible.

    Two ways to see the whole host. Run the agent as root, or keep the
    service account and give the unit two capabilities in a drop-in
    (`systemctl edit senhub-agent`):

    ```ini
    [Service]
    AmbientCapabilities=CAP_DAC_READ_SEARCH CAP_SYS_PTRACE
    ```

    then `systemctl restart senhub-agent`. The processes of other users
    (databases, web servers, root daemons) are then named correctly. The
    packaged unit grants neither, so a default install stays blind, as
    described above.

    On a non-root install without them the agent says so once at startup,
    naming the counts and the account, and reports its view as failed rather
    than reporting an empty graph as fact. `senhub-agent doctor` warns
    (`install.depends_on_caps`) when `entities.depends_on` is on and the unit
    and its drop-ins lack either capability.

    A peer on a loopback address is kept when it listens on a service port,
    so the graph shows what talks to what inside one host. A loopback peer on
    an ephemeral port (the client side of a local connection) is dropped.

`depends_on_debounce` controls how durable an outbound connection must be
before it appears as a `depends_on` edge: a peer endpoint must be seen on
this many emission scrapes before its edge is emitted, which keeps a
single stray socket out of the graph. The scrapes need not run
consecutively: a peer that is seen, missed, then seen again keeps its hits. A peer keeps its progress across up to fifteen scrapes
without being seen, because a short-lived flow is precisely one that is
missing from most samples: a reverse proxy that opens a request to a
backend and closes it may appear in four samples out of fifteen and
never in two running ones, and it is still a real dependency. The
soonest a dependency can surface is `depends_on_debounce x interval` (so
the default `3 x 60s` is about three minutes); lower it for a more
responsive graph, raise it to demand more evidence.

The same window applies once the edge exists: an edge survives up to fifteen
consecutive scrapes without the peer being seen (or `depends_on_debounce`, if
larger) before it is given up, and then has to earn its way back. A
long-lived connection the socket table happens to miss once is not a dependency
that ended, and retracting it on a single miss would reach a topology consumer
as an edge flapping in and out.

Loopback peers on an OS dynamic port, the client side of a one-shot local exchange, are ignored whatever these settings say (since 0.6.2).

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
- `service.instance.id` = an RFC 4122 UUID (version 5) derived from the
  agent key: one per agent, stable for as long as the key is, as the
  OpenTelemetry semantic conventions recommend
- `service.version` = the agent build version (when known), on logs and traces only. Metrics leave it out, and their scope carries no version: a backend such as VictoriaMetrics turns resource and scope attributes into labels of every series, so a version there would recreate every series at each upgrade. The version is on `senhub.agent.build.info`, one series.

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

| OTel name (OTLP wire) | Prometheus exposition | After OTLP ingest in VictoriaMetrics |
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

### The agent's own warnings

On request, the agent also sends its own warnings and errors as log
records, so an
operator who reads only the logs backend sees, for example, a probe
reporting a missing cloud permission. Records come from the producer
`senhub-agent` (type `agent`) with severity WARN, ERROR or FATAL, the
message as body (` error=...` appended when the event carries one), the
agent module in `senhub.agent.module`, and the other fields of the event
as attributes, secrets masked as in the agent's own log file. Messages
of the log pipeline itself are never sent back into it.

At most 20 records a second are sent, with bursts up to 100; the rest
are dropped and counted in `senhub.agent.self_logs.dropped`
(`senhub.agent.self_logs.published` counts the others). The environment
variable `SENHUB_SELF_LOGS` turns it on: `warn` sends warnings and above, `error`
errors and above. Unset (or `off`), nothing is sent.

<a id="logs-survive-an-outage"></a>

### Logs, metrics and traces survive an outage

When the collector cannot take an export, the agent keeps it on disk and
sends it again. Each signal has its own queue, with its own bounds.

- **Event logs**: the batch is kept as a list of records.
- **Metrics**: each push that fails is kept as the OTLP request the agent
  built, with its own timestamps. The history between the first failure and
  the recovery reaches the collector, not only the last value.
- **Traces**: each request of spans that fails is kept the same way.

In every case a file is deleted from disk only after the collector has
acknowledged that exact request, so a failed or interrupted attempt never
removes anything. This is on by default on every install; nothing to set.

While the collector is known to be down, new exports go straight to disk
and the agent does not try the network for each one. It probes the
collector on a retry clock of 5, 10, 20 and 40 seconds, then every 60
seconds; the clock starts again at 5 seconds after the next outage. As soon as
one export succeeds, a live one included, it sends the backlog, oldest first,
stopping at the first
request the collector does not acknowledge. A probe never holds the agent for
more than 4 seconds, so a collector that drops packets does not stop the
exports that have to be written meanwhile.

When the collector is up but overloaded and says so, the agent slows down
instead of retrying on its own clock. It reads the wait the collector asks
for: `Retry-After` on an HTTP 429 or 503 (a number of seconds or an HTTP
date), or the `RetryInfo` of a gRPC `RESOURCE_EXHAUSTED` or `UNAVAILABLE`
answer. Until that wait has passed, no export and no replay probe goes out;
new data is kept on disk meanwhile, exactly as during an outage, and sent
afterwards. A wait is never shorter than the retry clock above, and never
longer than 10 minutes: a collector that asks for more is obeyed up to
10 minutes, and the agent logs that once. A throttled export is not an
export error. It is counted in `senhub.agent.otlp.throttled` (by `signal`),
and the agent logs one warning per throttling episode, not one per batch. A
collector that gives no wait is handled as above, on the retry clock.

Third-party logs that the agent relays from an OTLP receiver are kept the
same way, in a `logrelayqueue/` directory next to the others, sized and aged
by `logs_queue_max_bytes` and `logs_queue_max_age`, and replayed in order.
Its depth appears in `senhub.agent.otlp.queue.*` as `signal="logs_relay"`.
Its disk use adds to that of the logs queue: both can fill their cap. The two
queues are independent, one holding the agent's own logs and the other the
relayed ones, so no order is kept between them; within each queue the oldest
batch is sent first.

Metrics are delivered in order: while a backlog waits, a new push is kept
behind it instead of overtaking it, so a time-series store that refuses
samples older than the newest it holds receives them oldest first. Replayed
points keep the timestamps they were collected with, and the last-value store
that feeds new pushes is not touched by a replay. After an outage the live
values go on as before; the points replayed between are the history.

| Situation | Event logs | Entity events | Metrics | Traces |
|---|---|---|---|---|
| Collector down, agent running | Kept on disk, sent in order once the collector answers | Re-sent at the next heartbeat | Every push kept on disk with its timestamps, sent in order once the collector answers | Spans kept on disk, sent once the collector answers |
| Agent stopped normally (restart, upgrade) during an outage | The records still in memory are written to disk in the first second of the stop, without trying a collector that is down, then sent at the next boot with the rest | Re-sent at the next heartbeat | A last push of the current values is written to disk, then sent at the next boot with the rest | The spans still in memory are written to disk, then sent at the next boot |
| Agent restarted, collector still down | Kept on disk, probed on the retry clock | Re-sent at the next heartbeat | Kept on disk, probed on the retry clock; nothing is collected while the agent is stopped | Kept on disk, probed on the retry clock |
| Agent killed or host crashed (kill -9, power cut) | Batches already on disk are kept; records not yet on disk are lost: at most the batch interval (5 s by default) plus one export timeout (4 s), so under 10 s of logs | Re-sent | Requests already on disk are kept; the values collected since the last push are lost unless `persistence.path` is set | Requests already on disk are kept; the spans of the current batch are lost (the trace batch interval) |
| Outage longer than 24 hours or past the size cap (128 MiB logs, 256 MiB metrics, 64 MiB traces) | The oldest batches are dropped and counted | Re-sent | The oldest requests are dropped and counted | The oldest requests are dropped and counted |

Delivery is at-least-once: if the collector received a request but its
acknowledgement was lost, the request is sent again, so a record or a point
can arrive twice. Replayed logs and spans can also arrive after newer ones;
they keep their original timestamp, so order by the record timestamp and
deduplicate on timestamp, body and attributes (logs) or on series and
timestamp (metrics) if duplicates matter.

Where the queues live:

| Install | Directory |
|---|---|
| Linux package (systemd) | `/var/lib/senhub-agent/otlp-queue` (`STATE_DIRECTORY`) |
| Windows (MSI) | `C:\ProgramData\SenHub\otlp-queue` |
| Container | `$SENHUB_STATE_DIR/otlp-queue` (default `/var/lib/senhub-agent/otlp-queue`) |
| `persistence.path` set | `<path>/logqueue`, `<path>/metricqueue`, `<path>/tracequeue`, `<path>/logrelayqueue` |

Under `otlp-queue/` the queues are the subdirectories `logqueue/`,
`metricqueue/`, `tracequeue/` and `logrelayqueue/`.

Each queue is bounded two ways: a size cap, past which the oldest requests
are evicted first, and an age limit (24 hours), swept at boot and every 10
minutes. Each sweep that drops aged requests logs one warning with the count.
Eviction by the size cap is reported the same way: one warning per queue and
per minute, and one at each sweep, carrying the number of batches and
records, data points or spans evicted since the last one.
What is dropped is counted in `senhub.agent.otlp.dropped`, in records for
logs, in data points for metrics and in spans for traces:

| Queue | Size cap (default) | Size reason | Age limit (default) | Age reason |
|---|---|---|---|---|
| Logs | `logs_queue_max_bytes` (128 MiB) | `logs_queue_full` | `logs_queue_max_age` (24 h) | `dropped_by_age` |
| Metrics | `metrics_queue_max_bytes` (256 MiB) | `metrics_queue_full` | `metrics_queue_max_age` (24 h) | `metrics_dropped_by_age` |
| Traces | `traces_queue_max_bytes` (64 MiB) | `traces_queue_full` | `traces_queue_max_age` (24 h) | `traces_dropped_by_age` |

If the directory is not writable the agent logs one warning for all the OTLP
queues together (it names the logs, metrics and traces data that are no longer
kept) and runs without any of them; each signal then keeps the SDK's own retry. A request that a
queue could not write (full disk) is counted as `queue_write_failed`.

Sizing the metrics queue. A metrics push is one point per active series, and
the queue keeps one request per push, compressed on disk. Measured on a
synthetic snapshot of 5,000 series, a 1 MB request takes 54 KB (about 11
bytes per point); real attributes compress less well, so plan 20 bytes per
point. For an outage of H hours: `series × (3600 / push interval in seconds)
× H × 20 bytes`. At the default 30-second interval, 5,000 series for 4 hours
take about 48 MiB; for 24 hours, about 290 MiB, over the default cap. A
busy host that must keep a full day raises `metrics_queue_max_bytes`; the
trade-off is the disk the state directory must be able to give up during an
outage. The default 256 MiB protects the disk first: past it the oldest
requests are evicted, the newest are kept, and the loss is counted.

Environment variables, which win over the file. Each queue has its own
switch: `SENHUB_LOG_QUEUE=false` turns off the logs queue (and the checkpoint),
`SENHUB_METRIC_QUEUE=false` the metrics queue, `SENHUB_TRACE_QUEUE=false` the
traces queue. `persistence.enabled: false` in the file turns all three off.

| Variable | Default | Effect |
|---|---|---|
| `SENHUB_LOG_QUEUE` | `true` | `false` turns off the logs queue and the checkpoint |
| `SENHUB_LOG_QUEUE_RETENTION` | `24h` | Maximum age of a queued log batch, a duration such as `6h`; `0` = no limit |
| `SENHUB_LOG_QUEUE_MAX_BYTES` | `134217728` | Logs disk cap, in bytes or with a suffix (`64MiB`) |
| `SENHUB_METRIC_QUEUE` | `true` | `false` turns the metrics queue off |
| `SENHUB_METRIC_QUEUE_RETENTION` | `24h` | Maximum age of a queued metrics request; `0` = no limit |
| `SENHUB_METRIC_QUEUE_MAX_BYTES` | `268435456` | Metrics disk cap, in bytes or with a suffix (`512MiB`) |
| `SENHUB_TRACE_QUEUE` | `true` | `false` turns the traces queue off |
| `SENHUB_TRACE_QUEUE_RETENTION` | `24h` | Maximum age of a queued traces request; `0` = no limit |
| `SENHUB_TRACE_QUEUE_MAX_BYTES` | `67108864` | Traces disk cap, in bytes or with a suffix (`128MiB`) |

In a container the state directory must be on a persistent volume (a
mounted volume, or an Azure Files share on Container Apps); without it the
queues disappear with the container and only survive an outage, not a
restart.

### Exports larger than the collector accepts

The agent cuts each logs export (event logs and entity events) into parts
whose estimated size stays under `signals.logs.max_export_bytes`, 3 MiB by
default. The estimate is of the uncompressed message, which is what a gRPC
receiver measures: its `max_recv_msg_size_mib` applies after decompression,
so gzip does not help with this limit. Parts are cut between records; an
entity event always keeps its complete relationships.

When the collector still refuses a part for its size (gRPC
`RESOURCE_EXHAUSTED` without a retry hint, HTTP 413, or the HTTP 400
"request body too large" the OpenTelemetry Collector answers over its
`max_request_body_size`), the agent halves
the part and sends each half, down to a single record, then sends the
following batches under a lower limit: half of the refused size, never
below 64 KiB. It logs this once:

```
OTLP logs export refused for size by the receiver; working export size limit lowered
```

with the configured limit (`configured_bytes`), the new one
(`working_bytes`) and the refused size (`refused_estimated_bytes`). The
lowered limit holds until the agent restarts. To avoid the first refusal
and the extra exports it costs, set `max_export_bytes` below the
collector's limit, for example `786432` (768 KiB) for a collector with
`max_recv_msg_size_mib: 1`.

A single record larger than the collector accepts cannot be cut. It is
dropped, counted in `senhub_agent_otlp_dropped_total{reason="record_too_large"}`,
and the first one is logged with its estimated size:

```
OTLP logs record larger than the receiver accepts; dropped
```

Records refused for another reason (for example `INVALID_ARGUMENT`) are
counted with `reason="receiver_rejected"`. Neither kind is written to the
disk queue: the same bytes would be refused again. When a part fails because
the collector is unreachable, only the records of that part go to the disk
queue; the parts already delivered are not sent again.

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

A minimal collector that accepts OTLP/gRPC and forwards metrics to
VictoriaMetrics, log records to VictoriaLogs and entity events to a
topology backend:

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: "0.0.0.0:4317"

processors:
  # Entity events are log records whose event name is entity.state or
  # entity.delete (the OpenTelemetry entity-events convention). Split on
  # the event name, not on the instrumentation scope: the scope carries
  # the discovery method and varies.
  filter/entities_only:
    error_mode: ignore
    logs:
      log_record:
        - 'event_name != "entity.state" and event_name != "entity.delete"'
  filter/entities_excluded:
    error_mode: ignore
    logs:
      log_record:
        - 'event_name == "entity.state" or event_name == "entity.delete"'

exporters:
  prometheusremotewrite:
    endpoint: "http://vm.internal:8428/api/v1/write"
    resource_to_telemetry_conversion:
      enabled: true

  otlphttp/logs:
    logs_endpoint: "http://victorialogs.internal:9428/insert/opentelemetry/v1/logs"

  otlphttp/topology:
    logs_endpoint: "http://topology.internal:4318/v1/logs"

service:
  pipelines:
    metrics:
      receivers: [otlp]
      exporters: [prometheusremotewrite]
    logs:
      receivers: [otlp]
      processors: [filter/entities_excluded]
      exporters: [otlphttp/logs]
    logs/entities:
      receivers: [otlp]
      processors: [filter/entities_only]
      exporters: [otlphttp/topology]
```

The `filter` processor drops what its condition matches, so each pipeline
keeps the other half. The `event_name` path needs a recent
`opentelemetry-collector-contrib`; this configuration runs on 0.158.0.
Without the split, every entity event is also stored in VictoriaLogs,
with no message (`missing _msg field`).

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
- `host.id` is the machine's own identifier: on Linux the SMBIOS
  product UUID when it is readable, otherwise `/etc/machine-id`; on
  Windows the `MachineGuid` registry value. The Windows value is not the
  SMBIOS UUID a hypervisor or a BMC reports for the same machine, so
  join a Windows host to its hardware on `host.name`, not on `host.id`.
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
otlp:
  endpoint: "otlp-gateway-prod-eu-west-2.grafana.net:443"
  headers:
    Authorization: "Basic eW91...="   # from Grafana Cloud console
  tls:
    enabled: true
```

### Elastic

Elastic takes OTLP in two ways, depending on where the stack runs.

**Elastic Cloud (Serverless or Elastic Cloud Hosted)** exposes a managed
OTLP endpoint. Copy its address from the Elastic Cloud console (Serverless:
*Manage* > *Application endpoints* > *OpenTelemetry*; Hosted: *Managed
OTLP*) and create an API key with the `event:write` privilege on the `apm`
application. The header value is `ApiKey`, a space, then the encoded key:

```yaml
otlp:
  protocol: http
  endpoint: "my-deployment.ingest.eu-west-1.aws.elastic.cloud:443"
  tls:
    enabled: true
  headers:
    Authorization: "ApiKey ${env:ELASTIC_API_KEY}"
  signals:
    metrics:
      enabled: true
    logs:
      enabled: true
```

The managed endpoint is not available on self-managed, ECE or ECK
clusters.

**Self-managed Elastic** receives OTLP through an Elastic Agent (EDOT
Collector) running in gateway mode, which writes to Elasticsearch. Its OTLP
receiver listens on 4317 (gRPC) and 4318 (HTTP). When the gateway checks
callers with the `apikeyauth` extension, the agent sends an Elasticsearch
API key with the `config_agent:read` privilege on the `apm` application,
in the same `ApiKey` form:

```yaml
otlp:
  protocol: grpc
  endpoint: "elastic-gateway.internal:4317"
  tls:
    enabled: true
    ca_file: /etc/ssl/private/elastic-gateway-ca.pem
  headers:
    Authorization: "ApiKey ${env:ELASTIC_API_KEY}"
```

Leave out `headers` if the gateway does not authenticate its callers. Use a
different API key from the one the gateway uses to write to Elasticsearch.

Temporality stays at the default, `cumulative`: Elasticsearch 9.5 and later
store both delta and cumulative metrics natively (on Elastic Cloud Hosted,
use 9.5.3 or later). Older stacks store delta counters as gauges and do not
support cumulative histograms.

Data lands in the `metrics-generic.otel-default`,
`logs-generic.otel-default` and `traces-generic.otel-default` data streams;
in Kibana, open *Discover* on one of them to see what the agent sent.

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
- **Web console** — the OTLP output page shows the same counters in
  its right column, next to the connection test. See
  [Web console](web-interface.md#otlp-output).
- **JSON endpoint** — `GET /api/{agentkey}/info/otlp` returns the same
  data as a single JSON snapshot. Useful for custom dashboards or
  external alerting. The field reference lives in
  [OTLP observability](https://github.com/senhub-io/senhub-agent/blob/master/docs/admin-guide/OTLP-OBSERVABILITY.md) (admin guide).

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
                                       # instead of being kept forever
                                       # (zombie series); "0s" disables
```

Both knobs are gauges of last resort — they protect the agent's memory
under runaway-cardinality conditions. Raise them only when you know
what's generating the series, otherwise the agent will happily eat
RAM tracking churning timeseries.

### Series metering (Enterprise)

A time-series backend such as VictoriaMetrics sizes and bills on active
series: a metric name with one set of point attributes, which received a
sample within the last hour. The Enterprise edition counts them on the
agent, as it exports, so you know what a host, a container or a pod
costs before you size the backend:

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `senhub.agent.series.active` | gauge | `output`, `probe` | Series this output exported within the last hour |
| `senhub.agent.series.created` | counter | `output`, `probe` | Series exported for the first time since the agent started; a steady climb is churn the backend stores as new series |
| `senhub.agent.series.untracked` | counter | – | New series not counted because the agent reached its 500,000-series memory bound; above 0, the active counts are a floor |

The agent's own metrics are counted under `probe="__agent__"`. Global
tags ride on the resource and do not multiply the count. The figures
describe the exports up to the previous push. Metering needs an
Enterprise licence; without it the agent counts nothing and emits none
of these metrics.

### What time a point carries

Each measurement is exported once, stamped with the time it was taken.
A push that finds no new measurement for a series exports nothing for
it, so a probe that runs every 60 seconds under a 30-second push sends
one point per run, and the points show the probe's cadence.

A probe slower than the backend's lookback would then vanish from a
dashboard between two runs. For gauges, the agent exports the current
value again while the probe still vouches for it:

- A probe vouches for its last value for one and a half times its
  `interval`, plus one push interval.
- While it does, and the series' last exported point is older than
  `keepalive_interval` (4 minutes by default), the push exports the
  same value again, stamped with the push time: the value is still the
  probe's current reading. A probe that runs every 30 minutes thus
  gets a point every 4 minutes and stays visible under a 5-minute
  lookback.
- Keep `keepalive_interval` below the lookback of the backend
  (5 minutes by default for Prometheus and VictoriaMetrics) and below
  `staleness_ttl`; `agent config check` warns when it is not below
  `staleness_ttl`. A probe that runs more often than
  `keepalive_interval` never gets a keepalive point.
- Counters and histograms are never exported again: their value at a
  given instant is only known when the probe measures it, and a
  repeated point would put a false zero in a `rate()`.
- Once the probe stops vouching (it missed its own next run, or the
  target is no longer in its configuration), nothing more is exported
  for the series, which is evicted after `staleness_ttl`.
- A series restored from the checkpoint at start-up is exported once,
  stamped with its measurement time, and never kept alive until its
  probe observes it again in this process.

`senhub.agent.otlp.keepalive_points` counts the points exported this
way. `keepalive_interval: 0s` turns it off: every measurement is then
exported once and only once.

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

### What the dependency discovery can and cannot see

The discovery reads the socket table on each emission scrape. It sees a
connection that is open at the moment of a scrape. It does not see a flow
that opens and closes entirely between two scrapes, however many times
that happens: nothing on the host records it by then. An absent edge
therefore means "not observed", never "does not exist", and that
distinction matters before concluding from a map that two machines do
not talk to each other. A shorter `interval` narrows the blind spot; it
does not close it.
