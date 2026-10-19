# Architecture

How the SenHub agent is put together: the packages, what each is
responsible for, and how data and entities move through the process. Per-area
contracts (probes, outputs, configuration, tests) live in `.claude/rules/`;
naming lives in [OTel semantic conventions](./otel/senhub-semantic-conventions.md).

## Shape of the process

One static binary. `cmd/agent/main.go` only calls `app.Main()`; everything else
is importable so the commercial edition can reuse the same dispatch.

```
                       +------------------------------+
  agent.yaml           |  configuration.Local         |
  probes.d/*.yaml  --> |  (loader, watcher, secrets,  | --- change callbacks --+
  strategies.d/*.yaml  |   substitution, versioning)  |                        |
                       +------------------------------+                        |
                                                                               v
  +-----------+  DataPoints   +---------------------------+  per-strategy  +------------+
  |  sensor   | ------------> |  data_store               | -------------> | strategies |
  |  probes   |  (callback)   |  unit corrections, tags,  |  AddDataPoints | otlp, http,|
  | (pollers) |               |  routing by probe target  |                | prtg, zabbix|
  +-----------+               +---------------------------+                | senhub,    |
        |                                                                  | event      |
        | entity.Source                                                    +------------+
        v                                                                        ^
  +--------------+  entity.Event   +------------------+                          |
  | entitydetect | --------------> | entity channel   | --- SubscribeEvents -----+
  | (detector)   |  (fan-out)      | (process-wide)   |     (only the otlp strategy
  +--------------+                 +------------------+      has a subscriber today)
```

`internal/agent/agent.go` builds these services and hands them to a
`lifecycle.Supervisor`.

## Packages and responsibilities

| Package | Responsibility |
|---|---|
| `cmd/agent`, `cmd/console-launcher` | Entry points. The first is a one-line shell around `app.Main()`. |
| `app` | CLI dispatch (`run`, `install`, `config check/show/set/migrate`, `doctor`, `status`, `update`, `secret`, `license`, `key`, `console`, `zabbix setup`), service installation (systemd, Windows SCM, MSI), privilege gate, and the blank-import catalogue of the probes this build ships (`probes_register.go`). |
| `internal/agent` | `agent.go` composes the services; `update.go` is the manual update command. |
| `internal/agent/lifecycle` | The single start/stop contract (`Service`: `GetName`, `Start(ctx)`, `Shutdown(ctx)`), optional per-service `StopBudget`, and the `Supervisor` that starts services in order and stops them in reverse. |
| `internal/agent/probes` | Probe registry (`RegisterProbe`, self-registration from `init()`), and `ProbePoller`, the per-instance runner. Each probe type lives in its own subpackage. `types` holds the `Probe` interface and `BaseProbe`; `spec` holds the declarative parameter schema. |
| `internal/agent/periodic_scheduler` | Interval ticker with retry and backoff used by `ProbePoller`. |
| `internal/agent/services/sensor` | The probe pool. Reads the probe configuration, applies the licence gate, creates and starts one `ProbePoller` per enabled probe, reconciles on configuration change, retries probes whose start failed. |
| `internal/agent/services/configuration` | Local configuration provider: loader (legacy and multi-file), fsnotify watcher, `${env:}` / `${file:}` / `${secret:}` substitution, atomic writes, versioning and migration, the `config set` / `config show` machinery, governance parsing. The `secret` subpackage implements the secret store backends (age key file, systemd credentials, DPAPI). |
| `internal/agent/services/data_store` | The hub between probes and outputs. Builds strategies from the configuration, owns the callback probes use, applies unit corrections (`transformers`) and global/custom tags, routes by target strategy. `otelmapper` turns cached metrics into OTel records; `outputspec`, `prtgnames`, `pushqueue`, `agentmetrics` are shared helpers. |
| `.../data_store/strategies/*` | The outputs: `otlp`, `http` (REST, Prometheus exposition, Nagios, web console, PRTG pull endpoints), `prtg` (push), `zabbix` (native active agent, passive listener, LLD, TLS and PSK), `senhub` (cloud push), `event`. `strategyreg` registers them, so the hub never imports a concrete sink. |
| `internal/agent/services/entity` | The entity model: `Source`, `Observation`, `Event`, the detector registry, host sub-detectors (`hostiface`, `hostnet`, `hostsvc`, `hostdep`, `macindex`), identity re-keying, and the process-wide event channel. |
| `internal/agent/services/entitydetect` | The producer service: polls the registered sources on a heartbeat and publishes events on the channel. Independent of any output. |
| `internal/agent/services/governance` | Operator-supplied owner, criticality, location and label metadata turned into governance attributes on entities. |
| `internal/agent/services/license` | JWT licence validation and the free / paid probe catalogue (`probe_catalog.go`). See [LICENSE-SYSTEM](../LICENSE-SYSTEM.md). |
| `internal/agent/services/auto_update` | Signed self-update (download, signature check, staging, MSI path on Windows, leftovers cleanup). Runs as a service only when `auto_update.enabled` is set. |
| `internal/agent/services/agentstate` | Process-lifetime counters and snapshots (probe health, export activity, event log, channel buffers) that independent packages share without import cycles. Feeds the self-observability metrics and the status views. |
| `internal/agent/services/statuschan` | Read-only local status channel (Unix socket, named pipe on Windows) used by `senhub-agent status`. |
| `internal/agent/services/instanceid`, `fsown`, `exporthook`, `exporterrors`, `debugshipper`, `logger` | Agent instance identity shared between agents on one host, file ownership checks, hooks a build can attach to the export path, export error classification, debug log shipping, module loggers with secret masking. |
| `internal/agent/services/snmpcore`, `snmpmib` | Shared SNMP client and USM code, and OID resolution used by `snmp_poll`, `snmp_trap` and the entity sources. |
| `probesdk` | Public mirror of the probe-authoring surface (`probes`, `types`, `datapoint`, `entity`, `config`, `logger`, `spec`, `docsparams`, ...). Go forbids importing `internal/` across modules, so the enterprise module compiles its probes against these thin re-exports. |
| `internal/docscoverage`, `docsparams`, `docsmetrics` | Documentation guards and generators run by `make test` and `make docs-params`. |

## Probes

### Interface

`types.Probe` (in `internal/agent/probes/types/probe.go`):

```go
type Probe interface {
    GetName() string
    ShouldStart() bool
    GetInterval() time.Duration
    Collect() ([]datapoint.DataPoint, error)
    OnStart(quitChannel chan struct{}) error
    OnShutdown(ctx context.Context) error
    EntitySource() entity.Source
}
```

Optional capabilities are separate interfaces: `ProbeWithCallback` (event
driven probes push through `SetCallback`), `ListenerProbe` (health is the
listener's state, not the last `Collect`), `PartialResultsProbe` (points of a
failed cycle are still routed), and `data_store.StrategyRouter`
(`GetTargetStrategies`). Probes embed `*types.BaseProbe`, which supplies the
name, type, log targets and default routing.

### Registration

Each probe package calls `probes.RegisterProbe("<type>", New...)` from `init()`.
A build contains exactly the probes it blank-imports: `app/probes_register.go`
for this repository, the enterprise entry point for the paid ones, using the
`probesdk/probes` mirror. A duplicate name panics at start, by design.

### Lifecycle of one probe instance

1. The configuration provider loads a `ProbeConfig` (`name`, `type`, `params`,
   optional `governance`, `log_strategies`, `custom_tags`).
2. `sensor` checks the licence (`license.GetFreeTierProbes`, paid set) and that
   the type exists in this build, then calls `probes.NewProbePoller`.
3. `NewProbePoller` looks up the constructor, runs it while collecting
   parameter issues (a value the probe could not read is warned about, not
   silently defaulted), reports legacy parameters, sets name, type and log
   targets, applies governance, and builds the `PeriodicScheduler`.
4. `Start` calls `ShouldStart`, starts the scheduler (`OnStart`, an immediate
   first run, then one `Collect` per interval with retry), and registers the
   probe's `EntitySource` with the entity detector (skipped for the no-op
   source of host-level probes and log conduits).
5. Each cycle is a `probe.collect` trace span. The poller records health and
   events in `agentstate`, stamps `probe_name` / `probe_type` on every point,
   and passes the points to the data store callback with the probe's router.
6. On configuration change `sensor.SyncConfiguration` starts new probes, stops
   removed ones, and for a probe whose identity is unchanged only re-applies
   governance. `Shutdown` unregisters the entity source, stops the scheduler
   and calls `OnShutdown`.

A failed start does not stop the others; `sensor` keeps a failed list, publishes
it to the status views and retries.

## Data flow: probe to data store to strategies

```
Probe.Collect()                       -> []datapoint.DataPoint
ProbePoller.collect()                 health, events, identity tags
    -> data_store.AddCallback(points, router)
dataStore.applyUnitCorrections()      transformer YAML definitions per probe type
dataStore.enrichWithConfiguredTags()  global_tags, custom_tags
for each active strategy:
    in router.GetTargetStrategies() ? -> strategy.AddDataPoints(points)
```

- The active strategy set is an immutable snapshot behind an `atomic.Pointer`;
  a configuration refresh builds a new slice and swaps it, so probe goroutines
  never see a half-built list.
- The default router sends to `senhub`, `prtg`, `http` and `otlp`. A strategy
  that consumes OTel records (`consumesOtelMetrics`) also receives points whose
  target list names `otlp`.
- Each strategy buffers and exports on its own. `otlp` resolves points to OTel
  metrics through `otelmapper` and the transformer definitions; `http` keeps a
  cache and serves it as REST, Prometheus, Nagios and PRTG; `zabbix` maps
  points to items and discovery rules.
- Logs and spans travel on their own channels (`agentstate` log and span
  channels) to the strategies that implement them, not through the metric
  callback.
- A probe's definition under `data_store/transformers/definitions` is the
  source of names, units and display names for every output. A probe without
  one falls back to a legacy transformer and increments a counter.

Adding an output: implement `data_store.SyncStrategy`, register a factory in
`strategyreg`, declare its parameters in a `spec.go` and its known keys with
`RegisterKnownParams`. Contracts in `.claude/rules/output-*.md`.

## Entity flow

Entities (what the host is and what it runs, with relations) are produced
separately from metrics:

1. Probes declare an `entity.Source` (`EntitySource()`); `ProbePoller`
   registers it. Probes never call `entity.RegisterSource` themselves.
2. `entitydetect.Service` is an agent service started by the supervisor,
   whatever outputs are configured. On each heartbeat it polls the registered
   sources plus its own host foundation (host, interfaces, addresses,
   listeners, dependencies) and publishes `entity.Event` values on the
   process-wide channel (`entity.PublishEvent`). Every entity and relation is
   re-emitted each interval; the interval travels on the event as the
   consumer's liveness backstop.
3. The channel fans out. A consumer calls `entity.SubscribeEvents`. Today the
   only subscriber is the OTLP strategy's `entityPump`, which encodes events as
   OTel log records for the entity pipeline. The other outputs do not carry
   entities.
4. `entitydetect.Resolve` derives the detector configuration from the `entities`
   block and the storage configuration, and the detector follows configuration
   changes through `Reconfigure`.

Identity, attributes and the Toise contract are defined in
[ENTITY-DETECTION](./engineering/ENTITY-DETECTION.md) and
[ENTITY-TELEMETRY-CONTRACT](./engineering/ENTITY-TELEMETRY-CONTRACT.md). Any
change to the entity model is validated with Toise first.

## Configuration

`LocalConfiguration` is the only provider. Two layouts are detected by content:

- Legacy monolithic: one file with `probes:` / `storage:`; the `.d/`
  directories are ignored with a warning.
- Multi-file (what `install` writes): `agent.yaml` for global keys, one YAML
  array per file in `probes.d/`, one strategy per file in `strategies.d/`.
  Files load alphabetically; dotfiles and `*.disabled` are skipped; for a
  duplicated strategy the later file wins.

Values go through `${env:}`, `${file:}` and `${secret:}` substitution after the
merge. Secrets resolve through the `secret` package (age key file, systemd
credentials, DPAPI). A fsnotify watcher over the file and both directories
reloads, swaps the snapshot atomically and calls the registered callbacks:
`sensor` reconciles probes, `data_store` rebuilds strategies, the entity
detector reconfigures. Details: `.claude/rules/configuration.md` and
[CONFIG-VERSION-CHANGELOG](../admin-guide/CONFIG-VERSION-CHANGELOG.md).

## Startup and shutdown

`agent.services()` returns the ordered set: configuration, data store, sensor,
status channel, entity detector, and the auto-updater when enabled. The
supervisor starts them in that order and stops them in reverse, so probes stop
before the data store and the configuration outlives its readers. Each service
has its own stop budget (`DefaultStopBudget` 3 s; data store 7 s, sensor 8 s),
sized so the whole agent stops under the 10 s a container runtime allows. A
service that fails to start makes the process exit non-zero so the service
manager does not restart it in a loop.

## Licence tiers and editions

The open-source build registers the free probes only. The paid probes live in
the private `senhub-agent-enterprise` module, which compiles them against
`probesdk`, blank-imports them next to `app.Main()` and ships the release
binaries. `license` validates a JWT bound to the customer or agent; an invalid,
unbound or expired licence leaves the agent on the free tier and sets the
`senhub.agent.license.invalid` self-metric. The authoritative probe split is
`internal/agent/services/license/probe_catalog.go`.

## Cross-cutting conventions

- **Logging**: module loggers (`logger.NewModuleLogger(base, "probe.cpu")`),
  structured fields only, debug level per module with `--filter`.
  Secrets are masked by the logger writer.
- **Errors**: wrap with `%w`; a non-fatal failure is logged at Warn with
  context, never dropped.
- **Concurrency**: one scheduler goroutine per probe; shared state behind a
  mutex or an atomic pointer; every goroutine ends on its context.
- **Tests**: `make test` is the only entry point. Guard tests under
  `data_store/transformers`, `strategyreg` and `probes` read the code and fail
  when a definition, a documented key or a registry entry drifts. See
  [Build System](./build-system.md) and `.claude/rules/tests.md`.

## Next steps

- [Design Patterns](./design-patterns.md) for code patterns.
- [Development Workflow](./development-workflow.md) for branches and review.
- [Current Development](./current-development.md) for where things stand.
