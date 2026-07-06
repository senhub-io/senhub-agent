<img src="https://cdn.simpleicons.org/windows" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

!!! warning
    **Windows only.** The probe subscribes to channels through the
    Windows Event Log API. On Linux and macOS it reports a clear
    `not supported on <OS>` error at start and stays inert — the
    same configuration file can ship to a mixed fleet.

# Windows Event Log Probe

The `windows_eventlog` probe subscribes to Windows Event Log
channels — System, Application, Security, or any operational channel
(Citrix, FSLogix, ...) — and ships each event as a structured OTel
log record through the [OTLP storage](../otlp.md). Filtering by
level, EventID and provider happens at the agent, so only the events
you asked for leave the host.

## Quick start

```yaml
# probes.d/10-windows_eventlog.yaml — each file under probes.d/ is a YAML array of probes
- name: windows-events
  type: windows_eventlog
  params:
    channels: [System, Application]
    levels: [Critical, Error, Warning]
    bookmark_path: 'C:\ProgramData\senhub-agent\eventlog.bookmark'
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `channels` | required | Channel names, e.g. `System`, `Security`, `Citrix-XenDesktop-VdaPlugin/Operational` |
| `levels` | all | Filter: `Critical`, `Error`, `Warning`, `Information`, `Verbose` (case-insensitive). Evaluated by the Event Log engine itself, so filtered events are never rendered |
| `include_event_ids` | all | Allow-list of EventIDs — only these are emitted |
| `exclude_event_ids` | none | Deny-list of EventIDs; takes precedence over the allow-list. The standard noise-suppression knob |
| `sources` | all | Provider name globs, e.g. `Citrix*`, `FSLogix*` |
| `bookmark_path` | none | File persisting the per-channel subscription position, so a restart resumes without loss or duplication. Without it, the probe tails from now on each start |
| `backlog` | `false` | Replay events from the persisted bookmark (or from the start of the channel) before switching to live tail |
| `redact_pii` | `false` | Blank sensitive Security-channel fields (account names, IP addresses) in the rendered body and event data — for GDPR-constrained environments |
| `poll_interval` | `30s` | Bookmark flush cadence. Event delivery itself is push-based; this does not add latency |

## Output

Each event becomes one OTel log record: channel, provider, EventID,
level (mapped to OTel severity), task, keywords, the rendered
message as the body, and the EventData fields as attributes.

## Operational notes

- **Push, not poll.** Events are delivered by subscription the
  moment they are written; `poll_interval` only drives bookmark
  persistence.
- **Filter at the source.** Level filters compile to an XPath query
  evaluated by the Event Log engine. EventID lists and provider
  globs are applied in-process. On a noisy Security channel,
  combining `levels` with `exclude_event_ids` keeps volume sane.
- **Security channel.** Reading it requires the agent to run with
  sufficient privilege (the service installs as LocalSystem by
  default, which suffices). Consider `redact_pii: true` when the
  records leave a controlled perimeter.
- **Bookmarks are cheap insurance.** Without `bookmark_path`, an
  agent restart loses whatever fired while it was down. With it,
  the subscription resumes exactly where it stopped.
