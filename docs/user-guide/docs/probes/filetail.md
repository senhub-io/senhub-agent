<img src="../../assets/probe-logos/filetail.svg" alt="" class="probe-page-logo probe-page-logo-mdi">

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
<!-- sha256:d874161f4070f3751bfe0bed7380330677b173bcb843e68572f888646936ccaa -->

| Parameter | Must set | Default | Description |
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

A path that does not exist yet, or whose directory is not there yet (a
mount that comes up after the agent), is picked up by the rescan once it
appears. Such a file is read from its first line, whatever
`from_beginning` says, since all of it was written after the probe
started watching for it.

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
lines are continuations. With `match: before`, a matching line ends the
record: it is added to it, then the record is sent. `negate: true` inverts
the test, so a line that does *not* match the pattern is the one that
starts (or ends) a record.

The pattern describes the line that **starts** a record, which is not how
Filebeat reads the same keys. A Filebeat configuration for Java logs,
`pattern: '^\['` with `negate: true` and `match: after`, describes the
continuation lines; here the same record is written with `negate: false`:

```yaml
  multiline:
    pattern: '^\['      # "[2026-09-29T18:21:51,024+02:00] ..." starts a record
    negate: false
    match: after
```

Copied as is from Filebeat, `negate: true` takes every stack-trace line
for the start of a new record.

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

## Detecting a tail that no longer reads

For every file it follows, the probe reports two gauges carrying the
file's absolute path in the `log.file.path` attribute (`log_file_path`
label in Prometheus): `senhub.filetail.read_offset`, the byte offset the
tail has read up to, and `senhub.filetail.file_size`, the size of the file
at collection time. A healthy tail has an offset equal to the size, give
or take the lines being written at that instant: on an active file a
non-zero gap is normal, because the tail reads while the writer writes
(several kilobytes on a large busy log). What is abnormal is a tail whose
offset stops moving while the file grows, a condition the number of
records emitted cannot show, since a quiet file and a stuck tail both
leave it flat. A path that does not exist yet or cannot be read reports neither
metric (it is reported as a probe error instead); when the file cannot be
sized, only the offset is reported.

In Prometheus the metrics are `senhub_filetail_read_offset_bytes` and
`senhub_filetail_file_size_bytes`. This expression
fires when, over 15 minutes, the file grew and the offset did not move:

```promql
changes(senhub_filetail_read_offset_bytes[15m]) == 0
and on (instance, probe_name, log_file_path)
delta(senhub_filetail_file_size_bytes[15m]) > 0
```

The labels on each series are `log_file_path`, `probe_name` and
`probe_type`; `instance` is added by your Prometheus when it scrapes the
agent (use the label your backend attaches to identify the host, for
example `host_id` when the metrics arrive through OTLP). Both gauges
carry the same labels; compare the series count of each side before
relying on the rule.

Both are gauges, hence `changes()` and `delta()` rather than
`increase()`, which would treat any drop as a counter reset. Across a
rotation the size drops, so the window containing the switch stays
silent; a real freeze lasts hours and is caught on the next window. Do
not alert on `file_size - read_offset > 0`: that gap is rarely zero on a
busy file.

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
| `senhub.filetail.records_emitted` | `senhub.filetail.records_emitted` | File Tail Records Emitted | # | Cumulative count of log records this file-tail probe has published to the log rail |
| `senhub.filetail.read_offset` | `senhub.filetail.read_offset` | File Tail Read Offset {log.file.path} | bytes | Byte offset the tail of a followed file has read up to |
| `senhub.filetail.file_size` | `senhub.filetail.file_size` | File Tail File Size {log.file.path} | bytes | Current size in bytes of a followed file; a healthy tail has a read offset equal to it |

<!-- schema:metrics:end -->
