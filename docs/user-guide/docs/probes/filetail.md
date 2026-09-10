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
# probes.d/10-filetail.yaml — each file under probes.d/ is a YAML array of probes
- name: app-logs
  type: filetail
  params:
    paths:
      - /var/log/myapp/*.log
    bookmark_path: /var/lib/senhub-agent/filetail.bookmark
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `paths` | Yes | - | File paths or glob patterns, re-expanded every 15 seconds. Example: `/var/log/app/*.log` |
| `bookmark_path` | No | - | File persisting read offsets across restarts; use a distinct one per instance. Example: `/var/lib/senhub-agent/filetail-app.json` |
| `from_beginning` | No | `false` | Read existing content the first time a file is seen |
| `max_bytes_per_line` | No | `1048576` | Cap on one record after multiline folding, in bytes |
| `multiline` | No | - | Fold continuation lines into one record |
| `multiline.pattern` | No | - | Regular expression tested against each line. Example: `^\d{4}-\d{2}-\d{2}` |
| `multiline.negate` | No | `false` | Invert the pattern match |
| `multiline.match` | No | `after` | Whether a matching line starts a record or flushes the previous one. One of `after`, `before` |
| `parser` | No | - | Structured parsing of each record |
| `parser.type` | No | `raw` | Record format. One of `raw`, `regex`, `json`, `logfmt` |
| `parser.pattern` | No | - | Regular expression with named groups; required for the regex type |
| `parser.timestamp_field` | No | - | Field carrying the record timestamp |
| `parser.timestamp_format` | No | - | Go reference-time layout of that field. Example: `2006-01-02 15:04:05` |

<!-- schema:params:end -->

Without `bookmark_path` the probe tails from the end of each file on every
start. `from_beginning` only applies to a file no bookmark knows yet.

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

With `match: after`, a matching line starts a new record and non-matching
lines are continuations. With `match: before`, a matching line flushes the
accumulated record first.

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

With `regex`, each named capture group becomes a log attribute. With
`json` and `logfmt`, every key becomes a log attribute. With `raw`, the
line is the record body, unparsed. `timestamp_field` names a capture group
or a JSON/logfmt key.

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
