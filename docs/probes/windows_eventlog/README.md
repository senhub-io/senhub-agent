# Windows Event Log Probe

The `windows_eventlog` probe reads the local Windows Event Log via the
native Windows Event Log API (`wevtapi`) and ships each matching event to
the agent's OTLP **logs** signal as a structured record. It is the
Windows counterpart of the `linux_logs` probe — host-local OS log
collection — and is part of the **Free tier**.

Primary use cases:

- **Workstations / servers**: application crashes, EDR alerts, service
  failures from the `Application` and `System` channels.
- **Citrix VDA**: VDA diagnostics from Citrix-specific operational
  channels (`Citrix-XenDesktop-VdaPlugin/Operational`, …).
- **FSLogix**: profile-container errors from the FSLogix providers.

## Platform

**Windows only.** The probe registers on every OS so a single fleet-wide
config is valid, but on non-Windows hosts `OnStart` fails loudly with a
clear message instead of silently disabling itself. Reading the
`Security` channel requires elevated privileges (run as `LocalSystem` or
add the agent's service account to the **Event Log Readers** group).

## Configuration

```yaml
- type: windows_eventlog
  name: vda_citrix_events
  params:
    channels:
      - System
      - Application
      - "Citrix-XenDesktop-VdaPlugin/Operational"
    levels: [Critical, Error, Warning]   # optional; default all levels
    include_event_ids: [1001, 1024]      # optional allow-list
    exclude_event_ids: [4624]            # optional deny-list (noise)
    sources: ["Citrix*", "FSLogix*"]     # optional provider globs
    poll_interval: 30s                   # bookmark flush cadence
    bookmark_path: "C:\\ProgramData\\SenHub\\bookmarks\\vda_citrix.xml"
    backlog: false                       # replay from bookmark/oldest on start
    redact_pii: false                    # GDPR redaction for Security
```

| Key | Type | Default | Notes |
|---|---|---|---|
| `channels` | list | **required** | One subscription per channel. |
| `levels` | list | all | `Critical`/`Error`/`Warning`/`Information`/`Verbose`. Pre-filtered at the source via XPath. |
| `include_event_ids` | list[int] | none | Allow-list; empty means all. |
| `exclude_event_ids` | list[int] | none | Deny-list; takes precedence over include. |
| `sources` | list | all | Provider-name shell globs, case-insensitive. |
| `poll_interval` | duration | `30s` | Drives bookmark flush cadence (events are push-delivered). |
| `bookmark_path` | path | none | Persist progress here; empty = tail-from-now each start. |
| `backlog` | bool | `false` | Replay from the bookmark (or oldest record) before tailing. |
| `redact_pii` | bool | `false` | Blank sensitive Security `EventData` fields and body. |

## Output

The probe emits **OTel logs only** — no metrics. Enable the OTLP logs
signal on your storage so the records are consumed:

```yaml
storage:
  - type: otlp
    signals:
      logs: true
```

Each record carries the mandated attributes `event_id`, `event_level`,
`event_channel`, `event_provider`, `event_source`, `record_id`, plus
OTel-canonical `host.name` / `process.pid` / `user.id` and the full
`EventData` payload under `eventdata.<Name>`. Severity maps Windows
levels to the OTel SeverityNumber range (Critical→FATAL, Error→ERROR,
Warning→WARN, Information→INFO, Verbose→DEBUG).

Full attribute and severity reference:
[`senhub-semantic-conventions.md` §4.16](../../developer-guide/otel/senhub-semantic-conventions.md).

## Bookmarks (no duplication, no loss)

When `bookmark_path` is set, the probe persists one wevtapi bookmark per
channel (JSON, atomic write) and resumes with `StartAfterBookmark` on the
next start. A missing file means first run; a corrupt file is reported and
replaced rather than silently re-reading everything.

## Examples

### Citrix VDA diagnostics

```yaml
- type: windows_eventlog
  name: vda_diagnostics
  params:
    channels: ["Citrix-XenDesktop-VdaPlugin/Operational", "System", "Application"]
    levels: [Critical, Error, Warning]
    sources: ["Citrix*"]
    bookmark_path: "C:\\ProgramData\\SenHub\\bookmarks\\vda.xml"
```

### Security audit with PII redaction

```yaml
- type: windows_eventlog
  name: security_audit
  params:
    channels: ["Security"]
    levels: [Critical, Error, Warning]
    exclude_event_ids: [4624]   # successful logon noise
    redact_pii: true            # required when shipping Security off-host
    bookmark_path: "C:\\ProgramData\\SenHub\\bookmarks\\security.xml"
```

> **GDPR**: the `Security` channel contains logon identities and source
> IPs. Enable `redact_pii: true` whenever Security records leave the host.

## Status

Validated end-to-end on **Windows Server 2022** (build 20348):

- Real `System` + `Application` events flow to OTLP logs with every
  mandated attribute (`event_id`, `event_level`, `event_channel`,
  `event_provider`, `event_source`, `record_id`) plus `host.name`,
  `process.pid` and the `eventdata.*` payload.
- Per-channel bookmarks persist and resume after a restart
  (`StartAfterBookmark`) with **no duplication and no loss**.
- Steady-state tail footprint ~22 MB / ~0% CPU; a 53k-event backlog
  flood drains at ~4400 events/s (~1 core, 83 MB peak).

The OS-agnostic surface (config, XML parsing, filtering, PII redaction,
bookmark persistence, LogRecord mapping) is unit-tested on every
platform.

> **Level vs EventType**: the `levels:` filter matches the Event schema
> `System/Level` field. Events written through the legacy ReportEvent API
> (e.g. `eventcreate`, some older providers) carry `Level=0` (LogAlways)
> even when their *EventType* is Error/Warning, so a `levels:` filter will
> exclude them. Modern providers set `Level` correctly. Omit `levels:` to
> capture legacy-API events regardless.
