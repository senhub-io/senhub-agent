<img src="https://api.iconify.design/mdi/console.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier.

!!! warning
    The configured program runs **as the agent user** (root on Linux,
    LocalSystem on Windows). Read the security notes below before
    enabling this probe.

# Exec Probe

The `exec` probe runs an operator-supplied program on interval and
turns its output into metrics — the custom-check long tail. Two
output contracts are supported:

- **Nagios plugin convention** (default): exit code carries the
  status, perfdata after the `|` carries the measurements. Existing
  `check_*` plugins work unchanged.
- **JSON contract**: a structured document for new scripts.

## Quick start

```yaml
probes:
  - name: raid-status
    type: exec
    params:
      command: /usr/lib/nagios/plugins/check_raid
      args: ["-p", "1"]
      interval: 120
      timeout: 30
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `command` | required | Absolute path to the program. Relative paths and PATH lookup are refused |
| `args` | `[]` | Arguments, passed verbatim — no shell is involved |
| `format` | `nagios` | `nagios` or `json` |
| `interval` | `60` | Seconds between runs |
| `timeout` | `10` | Hard deadline in seconds; on expiry the whole process group is killed |
| `env` | none | Extra environment variables (the agent's environment is inherited) |
| `workdir` | agent's | Working directory for the run |

## Nagios contract

```
$ ./check_queue
WARN - queue depth high | depth=42;10;50 'wait time'=230ms
$ echo $?
1
```

- Exit code 0/1/2 maps to ok/warning/critical; anything else (and a
  timeout kill) is unknown (3). Reported as `senhub.exec.status`.
- Each perfdata token becomes a metric named
  `senhub.exec.<label>` (label lowercased and sanitized). Time units
  are normalized to seconds (`230ms` becomes 0.23), byte units to
  bytes, and the `c` UOM marks the value as a counter. Warn/crit/min/
  max thresholds are accepted and ignored — thresholds belong in your
  alerting layer.
- Malformed perfdata tokens are skipped; one bad token never voids
  the check.

## JSON contract

```json
{
  "status": 0,
  "metrics": [
    {"name": "queue.depth", "value": 12, "tags": {"queue": "orders"}},
    {"name": "processed", "value": 4012, "type": "counter"}
  ]
}
```

- `status` (0..3) is optional; without it the exit code is mapped
  exactly like the Nagios contract.
- `type` is `gauge` (default) or `counter`. `tags` become metric
  tags. Names are namespaced under `senhub.exec.*` and sanitized.

## Self-metrics

| Metric | Description |
|---|---|
| `senhub.exec.status` | 0 ok, 1 warning, 2 critical, 3 unknown |
| `senhub.exec.duration` | Wall-clock run time |
| `senhub.exec.timeout` | 1 when the run was killed on the deadline |
| `senhub.exec.skipped` | 1 when a cycle was skipped because the previous run was still going |

## Security notes

This probe executes whatever the configuration points at, with the
agent's privileges. The probe enforces what it can:

- **Absolute paths only.** PATH lookup is refused, so a writable
  directory earlier in PATH cannot shadow your check.
- **World-writable executables are refused** on Linux/macOS at probe
  start: a script any local user can rewrite is a privilege
  escalation, not a check.
- **No shell.** `command` + `args` go straight to the OS. Pipelines
  and redirections belong inside a script file that you own.

What remains your responsibility:

- Keep check scripts owned by root (or an admin account on Windows)
  and not group-writable; treat their directory the same way.
- Review what the script itself calls — the probe cannot audit
  transitive trust.
- Prefer read-only checks. A "check" that mutates state will run
  every cycle, forever.
- On Windows, lock down ACLs on the script; the world-writable test
  is Unix-only.

## Operational notes

- **Overlap protection.** If a run outlives the interval, the next
  cycle is skipped and reported via `senhub.exec.skipped` — processes
  never pile up.
- **Output caps.** stdout/stderr are captured up to 1 MiB each.
- **One probe instance per check.** Each check gets its own probe
  block with its own interval and timeout, and shows up under its own
  probe name.
