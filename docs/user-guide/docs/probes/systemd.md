<img src="../../assets/probe-logos/systemd.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Systemd Units

The `systemd` probe supervises systemd units on Linux via D-Bus, reporting the
active state, sub-state and restart counter per unit. One metric per unit,
tagged with `systemd.unit`.

**Linux only.** Returns an error on Windows and macOS.

## Quick start

```yaml
# probes.d/10-systemd.yaml — each file under probes.d/ is a YAML array of probes
- name: systemd
  type: systemd
```

All non-transient units are monitored by default; every parameter is optional.

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:f1322500755306ed87f18c15c14a849719467e5de67d40e490d609f2990eac9f -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `units` | No | - | Unit names or shell globs to watch; empty watches enabled units of the included types. Example: `nginx.service, ssh*.service` |
| `include_types` | No | `[service socket timer mount]` | Unit type suffixes to include. Example: `service, timer` |
| `detail` | No | `state` | Metrics detail: state emits only active_state per unit; full emits all systemd families |
| `interval` | No | `30` | Seconds between collections |

<!-- schema:params:end -->

Without `units`, every non-transient unit of the included types is
watched. Restrict `units` on a host with many units: each one costs several
series per cycle.

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `systemd.unit.active_state` | 1 | 1 when the unit active state is `active`, 0 otherwise, tagged with `systemd.unit` |
| `systemd.unit.sub_state` | 1 | 1 when the unit sub-state is `running` or `listening`, 0 otherwise; the raw sub-state value is available in the `sub_state` tag |
| `systemd.unit.load_state` | 1 | 1 when the unit load state is `loaded`, 0 otherwise |
| `systemd.unit.restarts` | {restart} | Number of times the unit has been restarted by systemd; service units only |

## Operational notes

- When `units` is empty, only the units systemd is supposed to start by itself are watched: `UnitFileState` enabled or enabled-runtime, among the included types. Name a static, disabled or masked unit in `units` to watch it.
- Each watched unit is a `host.service` entity on the topology graph (identity `host.id` + `host.service.name`, the full unit name, so `nginx.service` and `nginx.socket` are two entities), linked to its host by `runs_on`, with `host.service.manager` (`systemd`) and `host.service.start_mode`. Whether it runs is a measurement, not a graph fact: `host.service.running` gives it as 1 or 0, the same series for systemd and Windows, with the same `host.id` and `host.service.name`. Agents before 0.6.4 published units as `service.instance`; the first start retires those with the reason `rekeyed` and links each new entity to its old one.
- `detail: state` (the default) emits `host.service.running` alone, one series per watched unit; `detail: full` adds `systemd.unit.active_state`, `load_state`, `sub_state` and the restart counter. The probe is installed on every host, so its default stays at one series per unit.
- Without `units`, the probe watches every loaded unit of the included
  types, except the units systemd creates for each user login and removes
  at logout (`run-user-*.mount`, `user@*.service`,
  `user-runtime-dir@*.service`). Services started on demand (packagekit,
  systemd-oomd) also leave systemd's list when they stop. With a PRTG
  sensor, which keeps every channel it has seen, name the units to watch
  in `units` so the channel list stays stable.
- The probe reads from the local system D-Bus socket. No special privileges are needed beyond D-Bus access, which is granted to root by default.
- The `systemd.unit.type` tag carries the unit type suffix (service, socket, mount, timer, …).

## Metric reference

Every metric this probe can emit. **Metric** is the OpenTelemetry name the
OTLP, Prometheus and Zabbix outputs derive theirs from. **Name** is what a
[Nagios check](../nagios.md) and the API `metrics=` filter match.
**PRTG channel** is the label PRTG shows, placeholders filled from the
series' tags.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Name | PRTG channel | Unit | Description |
|---|---|---|---|---|
| `host.service.running` | `host.service.running` | Service {systemd.unit} Running | # | 1 when the service runs, 0 when it does not (stopped, failed, starting, stopping or paused), the same series for systemd and Windows |
| `systemd.unit.active_state` | `systemd.unit.active_state` | Systemd {systemd.unit} Active State | # | 1 when the unit active state is 'active', 0 otherwise |
| `systemd.unit.sub_state` | `systemd.unit.sub_state` | Systemd {systemd.unit} Sub State | # | 1 when the unit sub state is 'running' or 'listening', 0 otherwise. The sub_state tag carries the raw sub state value. |
| `systemd.unit.load_state` | `systemd.unit.load_state` | Systemd {systemd.unit} Load State | # | 1 when the unit load state is 'loaded', 0 otherwise (not-found, error, masked) |
| `systemd.unit.restarts` | `systemd.unit.restarts` | Systemd {systemd.unit} Restarts | # | Cumulative restart count for service units (NRestarts D-Bus property). Only emitted for units of type 'service'. |
| `senhub.probe.check.result` | `senhub.probe.check.result` | Check Result | # | Why the last check failed: 0 ok, 1 target down, 2 auth denied, 3 timeout, 4 TLS error, 5 config error, 6 protocol error, 7 permission denied |

<!-- schema:metrics:end -->
