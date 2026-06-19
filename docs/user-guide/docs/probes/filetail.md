<img src="https://api.iconify.design/mdi/file-document-outline.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier.

# File Tail Probe

The `filetail` probe tails flat log files — application logs, web
server access logs, anything line-oriented — and ships each record
as a structured OTel log record through the
[OTLP storage](../otlp.md). It handles glob patterns, log rotation,
multiline records (stacktraces), and optional structured parsing
(regex, JSON, logfmt) with timestamp extraction.

Works on Linux, Windows and macOS; files are opened in shared-read
mode so the producing application is never blocked.

## Quick start

```yaml
probes:
  - name: app-logs
    type: filetail
    params:
      paths:
        - /var/log/myapp/*.log
      bookmark_path: /var/lib/senhub-agent/filetail.bookmark
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `paths` | required | File paths or glob patterns. Globs are re-evaluated every 15 seconds, so new files are picked up without a restart |
| `bookmark_path` | none | JSON file persisting per-file read offsets across restarts. Without it the probe tails from end-of-file on every start |
| `from_beginning` | `false` | Read existing content from offset 0 the first time a file is seen (when no bookmark exists) |
| `max_bytes_per_line` | `1048576` | Cap on a single logical record after multiline folding (protects memory against runaway lines) |
| `multiline` | none | Fold continuation lines into one record (see below) |
| `parser` | `raw` | Structured parsing of each record (see below) |

### Multiline folding

Java stacktraces, Python tracebacks and pretty-printed payloads span
several physical lines. The `multiline` block folds them into one
record:

```yaml
params:
  paths: [/var/log/myapp/server.log]
  multiline:
    pattern: '^\d{4}-\d{2}-\d{2}'   # a timestamp starts a new record
    negate: false
    match: after
```

| Field | Default | Description |
|---|---|---|
| `pattern` | none | Regex tested against each physical line |
| `negate` | `false` | Invert the pattern match |
| `match` | `after` | `after`: a matching line starts a new record, non-matching lines are continuations. `before`: a matching line flushes the accumulated record first |

### Structured parsing

```yaml
params:
  paths: [/var/log/nginx/access.log]
  parser:
    type: regex
    pattern: '^(?P<remote_addr>\S+) \S+ \S+ \[(?P<time_local>[^\]]+)\] "(?P<request>[^"]*)" (?P<status>\d+)'
    timestamp_field: time_local
    timestamp_format: "02/Jan/2006:15:04:05 -0700"
```

| Field | Default | Description |
|---|---|---|
| `type` | `raw` | `raw`, `regex`, `json`, or `logfmt` |
| `pattern` | none | Required for `regex`; must contain named capture groups — each group becomes a log attribute |
| `timestamp_field` | none | Field (capture group or JSON/logfmt key) carrying the record timestamp |
| `timestamp_format` | none | Go reference-time layout for parsing `timestamp_field` |

With `json` and `logfmt`, every key becomes a log attribute. With
`raw`, the line is the record body, unparsed.

## Operational notes

- **Rotation-safe.** Each file is fingerprinted (hash of its first
  bytes), so copytruncate, daily rotation and file replacement are
  detected and the probe restarts from offset 0 of the new file —
  no silent gaps, no duplicates.
- **At-least-once delivery.** Bookmarks are flushed every 2 seconds;
  after a crash, up to 2 seconds of records may be re-read. Plan
  deduplication downstream if exact-once matters.
- **Event-driven.** No polling cycle: records ship as lines are
  written.
- **Start position.** Default is tail-from-now. Set
  `from_beginning: true` for files whose full history matters on
  first ingestion (combine with `bookmark_path` so it only happens
  once).
