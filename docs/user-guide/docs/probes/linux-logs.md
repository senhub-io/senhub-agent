<img src="https://cdn.simpleicons.org/linux" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — included in the free tier alongside CPU, memory,
    network, and logical disk. Same posture as the other host probes:
    local-OS observability, no remote API call, no vendor SDK.

!!! warning
    **Linux only.** The probe spawns `journalctl --output=json --follow`
    as a subprocess to stream entries from the local systemd journal.
    The agent compiles for Windows and macOS as well, but the probe
    returns a clear `not supported on <GOOS>` error on those platforms
    — operators can keep a single configuration file across mixed
    fleets without conditional logic.

# Linux Logs Probe

The `linux_logs` probe collects entries from the local systemd
journal and publishes them as structured OTel log records.
Combined with the [OTLP storage](../otlp.md), this
gives a fully self-contained agent → OTel collector pipeline for OS
logs — no Filebeat / journald-exporter sidecar to deploy.

## Quick start

```yaml
probes:
  - name: system-logs
    type: linux_logs
    params:
      priority: 6                          # max severity (0..7)
```

This emits every entry with priority ≤ 6 (i.e. notice and below;
drops debug). The records flow through the agent log channel
identical to records produced by the `syslog` and `event` probes
— any storage that consumes logs (today: OTLP) ships them.

## Parameters

All parameters are optional.

| Parameter | Default | Description |
|---|---|---|
| `units` | `[]` | Filter to specific systemd units. Each entry becomes a `--unit=<u>` flag. Empty = no unit filter. |
| `identifiers` | `[]` | Filter by `SYSLOG_IDENTIFIER` (the program name in the journal, e.g. `sshd`, `kernel`). Each entry becomes a `--identifier=<id>` flag. |
| `priority` | `7` | Maximum syslog priority to include (0..7). 7 = debug+everything above; 4 = warning+errors+critical; 0 = emergency only. |
| `include_boot` | `false` | When `true`, replay entries from the start of the current boot. Default: stream only new entries after probe start (`--since=now`). |

Examples:

```yaml
# Only ssh + kernel, error severity and above
probes:
  - name: critical-only
    type: linux_logs
    params:
      identifiers: [sshd, kernel]
      priority: 3
```

```yaml
# Multiple service filters
probes:
  - name: app-services
    type: linux_logs
    params:
      units: [nginx.service, postgresql.service, my-app.service]
      priority: 6
```

## Output

Each journal entry becomes an OTel log record with these
attributes (mapped per the OTel semantic conventions):

| Attribute | Source |
|---|---|
| Severity (number + text) | `PRIORITY` field, mapped via RFC 5424 → OTel table |
| Body | `MESSAGE` field |
| `host.name` | `_HOSTNAME` |
| `systemd.unit` | `_SYSTEMD_UNIT` |
| `syslog.appname` | `SYSLOG_IDENTIFIER` |
| `process.pid` | `_PID` |
| `process.owner.uid` | `_UID` |
| `process.executable.name` | `_COMM` |
| `systemd.transport` | `_TRANSPORT` |
| `senhub.probe.name` | The probe instance name (from `probes[].name`) |
| `senhub.probe.type` | `"linux_logs"` |

Severity mapping (RFC 5424 → OTel):

| Syslog | Name | OTel SeverityNumber | OTel SeverityText |
|---|---|---|---|
| 0 | emergency | 24 | FATAL4 |
| 1 | alert | 23 | FATAL3 |
| 2 | critical | 22 | FATAL2 |
| 3 | error | 17 | ERROR |
| 4 | warning | 13 | WARN |
| 5 | notice | 10 | INFO2 |
| 6 | info | 9 | INFO |
| 7 | debug | 5 | DEBUG |

## Operational notes

- **No periodic Collect.** The probe is event-driven: the subprocess
  pushes records as they appear. The poller's tick is a no-op.
- **Subprocess lifecycle.** On probe start, `journalctl` is spawned
  with `Setpgid` so signals reach only it, not the agent process
  group. On stop, SIGTERM is sent, then SIGKILL escalates if the
  process doesn't exit within the shutdown deadline.
- **Resilience.** Each malformed JSON line is logged at DEBUG and
  skipped — a single garbled entry never breaks the stream.
- **Cardinality.** Setting `priority: 7` on a busy host can produce
  thousands of records per minute. The default `6` filters out
  debug; consider `4` (warnings and errors only) on chatty hosts
  where storage in VictoriaLogs is a concern.

## Cross-platform notes

- **Linux:** uses the standard `journalctl` from systemd. Works
  with systemd 230+ (universal across modern distros).
- **Windows + macOS:** the probe registers but its `OnStart` returns
  `"linux_logs probe is not supported on <GOOS> (requires
  systemd-journald)"`. The agent keeps running with other probes;
  only this probe is inert.
- **No CGO.** Uses subprocess + JSON parsing, not `libsystemd` —
  preserves the agent's pure-Go build profile.
