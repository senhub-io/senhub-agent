<img src="../../assets/probe-logos/windows-services.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Windows Services

The `winservices` probe enumerates Windows services via the Service Control
Manager and reports the running/stopped state and raw SCM status code per
service. It is the Windows counterpart of the `systemd` probe on Linux.

**Windows only.** The probe does not start on Linux or macOS.

## Quick start

```yaml
# probes.d/10-winservices.yaml — each file under probes.d/ is a YAML array of probes
- name: winservices
  type: winservices
```

All services visible to the SCM are monitored by default. Use `services` to
restrict to a subset.

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:40df3e35dbc4aaca3727731a44b80223853b71376950d4a8cf6a68bc5e786a2a -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `services` | No | - | Service short names to monitor; empty means services configured for automatic start. Example: `wuauserv, Spooler` |
| `detail` | No | `state` | Metrics detail: state emits only windows.service.state per service; full emits all winservices families |
| `interval` | No | `30s` | Collection interval |

<!-- schema:params:end -->

`interval` accepts a number of seconds or a duration such as `1m`. A listed
service that cannot be opened or queried is skipped for that cycle rather
than failing the whole collection.

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.winservices.up` | 1 | 1 when the SCM is reachable and the probe completed its cycle |
| `windows.service.state` | 1 | 1 when the service is in the Running state, 0 otherwise, tagged with `windows.service.name` |
| `windows.service.status` | 1 | Raw SCM service status code (1=Stopped, 2=Start Pending, 3=Stop Pending, 4=Running, …) per service |
| `windows.service.start_type` | 1 | How the service is configured to start: 0 boot, 1 system, 2 automatic, 3 manual, 4 disabled, 5 automatic with delayed start. Absent for a service whose configuration cannot be read. Not a PRTG channel |

## Operational notes

- The agent must run with sufficient privileges to query the SCM. Administrator or LocalSystem is required for full enumeration.
- When `services` is empty, only the services Windows is supposed to start by itself are watched: start type Automatic, delayed included. Name a demand-start or disabled service in `services` to watch it.
- `detail: state` (the default) emits one state series per watched service; `detail: full` adds the raw status and the start type. The probe is installed on every host, so its default stays at one series per service.
- Service names are the short internal names used by `sc query` and `Get-Service`, not display names (e.g. `wuauserv` not "Windows Update").

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
| `senhub.winservices.up` | `senhub.winservices.up` | Windows Services SCM Up | # | 1 when the Service Control Manager is reachable and the probe completed its cycle |
| `windows.service.state` | `windows.service.state` | Service {windows.service.name} Running | # | 1 when the service is in the Running state, 0 otherwise |
| `windows.service.status` | `windows.service.status` | Service {windows.service.name} Status | # | Numeric SCM state of the service: 1=stopped 2=start_pending 3=stop_pending 4=running 5=continue_pending 6=pause_pending 7=paused |
| `windows.service.start_type` | `windows.service.start_type` | Service {windows.service.name} Start Type | # | How the service is configured to start: 0=boot 1=system 2=automatic 3=manual 4=disabled 5=automatic (delayed start) |
| `senhub.probe.check.result` | `senhub.probe.check.result` | Check Result | # | Why the last check failed: 0 ok, 1 target down, 2 auth denied, 3 timeout, 4 TLS error, 5 config error, 6 protocol error, 7 permission denied |

<!-- schema:metrics:end -->
