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
<!-- sha256:35a81450ce5ec571e785b556be8331759d0f090940e238a3a730b5e5fe4da2cf -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `units` | No | - | Unit names or shell globs to watch; empty watches every unit of the included types. Example: `nginx.service, ssh*.service` |
| `include_types` | No | `[service socket timer mount]` | Unit type suffixes to include. Example: `service, timer` |
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

- The probe reads from the local system D-Bus socket. No special privileges are needed beyond D-Bus access, which is granted to root by default.
- Transient units (runtime-generated, without a unit file) are excluded.
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
| `systemd.unit.active_state` | `systemd.unit.active_state` | Systemd {systemd.unit} Active State | # | 1 when the unit active state is 'active', 0 otherwise |
| `systemd.unit.sub_state` | `systemd.unit.sub_state` | Systemd {systemd.unit} Sub State | # | 1 when the unit sub state is 'running' or 'listening', 0 otherwise. The sub_state tag carries the raw sub state value. |
| `systemd.unit.load_state` | `systemd.unit.load_state` | Systemd {systemd.unit} Load State | # | 1 when the unit load state is 'loaded', 0 otherwise (not-found, error, masked) |
| `systemd.unit.restarts` | `systemd.unit.restarts` | Systemd {systemd.unit} Restarts | # | Cumulative restart count for service units (NRestarts D-Bus property). Only emitted for units of type 'service'. |

<!-- schema:metrics:end -->
