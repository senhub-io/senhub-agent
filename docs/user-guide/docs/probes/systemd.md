!!! info
    **License: Free** — part of the universal collection tier.

# Systemd Units

The `systemd` probe supervises systemd units on Linux via D-Bus, reporting the
active state, sub-state and restart counter per unit. One metric per unit,
tagged with `systemd.unit`.

**Linux only.** Returns an error on Windows and macOS.

## Quick start

```yaml
probes:
  - name: systemd
    type: systemd
```

All non-transient units are monitored by default. No parameters are required.

## Parameters

This probe takes no configuration parameters.

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `systemd.unit.active_state` | 1 | 1 when the unit active state is `active`, 0 otherwise, tagged with `systemd.unit` |
| `systemd.unit.sub_state` | 1 | 1 when the unit sub-state is `running` or `listening`, 0 otherwise; the raw sub-state value is available in the `sub_state` tag |
| `systemd.unit.load_state` | 1 | 1 when the unit load state is `loaded`, 0 otherwise |
| `systemd.unit.restart_count` | {restart} | Number of times the unit has been restarted by systemd |

## Operational notes

- The probe reads from the local system D-Bus socket. No special privileges are needed beyond D-Bus access, which is granted to root by default.
- Transient units (runtime-generated, without a unit file) are excluded.
- The `systemd.unit.type` tag carries the unit type suffix (service, socket, mount, timer, …).
