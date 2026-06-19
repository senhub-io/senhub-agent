!!! info
    **License: Free** — part of the universal collection tier.

# Windows Services

The `winservices` probe enumerates Windows services via the Service Control
Manager and reports the running/stopped state and raw SCM status code per
service. It is the Windows counterpart of the `systemd` probe on Linux.

**Windows only.** The probe does not start on Linux or macOS.

## Quick start

```yaml
probes:
  - name: winservices
    type: winservices
```

All services visible to the SCM are monitored by default. Use `services` to
restrict to a subset.

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `services` | all | List of service names to monitor (empty = all services). Case-insensitive, matches the service's short name (`sc query` output) |

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
