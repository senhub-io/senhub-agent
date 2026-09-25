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
<!-- sha256:bbd70d969aadcd3e95a5789062ca61c7d63aa9cefae3a0eae290ce6a45adfbd1 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `services` | No | - | Service short names to monitor; empty means every service. Example: `wuauserv, Spooler` |
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

## Operational notes

- The agent must run with sufficient privileges to query the SCM. Administrator or LocalSystem is required for full enumeration.
- When `services` is empty, every service the SCM enumerates is reported — this can generate a large number of PRTG channels on busy servers. Restrict with an explicit list for PRTG deployments.
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

<!-- schema:metrics:end -->
