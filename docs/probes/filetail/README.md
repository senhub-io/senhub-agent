# File Tail Probe

The `filetail` probe tails one or more flat log files (or globs) and
ships each parsed line to the agent's OTLP **logs** signal as a
structured record. It is the generic, cross-platform counterpart of
`linux_logs` (systemd-journal-only) and `windows_eventlog` (Event Log):
its targets are application log files, Citrix VDA / Workspace logs,
FSLogix / Profile Management logs, IIS logs, and any flat file produced
before an app is natively instrumented. Part of the **Free tier**.

## Platform

Cross-platform (no build tags). Files are opened in shared-read mode —
the non-exclusive access Windows requires to read an actively-written
log — via `github.com/nxadm/tail`, which also handles rotation and
reopen.

## Configuration

```yaml
- type: filetail
  name: citrix_vda_logs
  params:
    paths:
      - "C:\\Windows\\Logs\\Citrix\\*.log"
      - "C:\\ProgramData\\Citrix\\GroupPolicy\\Logs\\*.log"
    parser:
      type: regex            # regex | json | logfmt | raw
      pattern: '^(?P<ts>\S+ \S+) \[(?P<level>\w+)\] (?P<component>\w+): (?P<message>.+)$'
      timestamp_field: ts
      timestamp_format: "2006-01-02 15:04:05.000"
    multiline:
      pattern: '^\d{4}-\d{2}-\d{2}'   # a line starting with a date begins a new record
      negate: false
      match: after
    bookmark_path: "C:\\ProgramData\\SenHub\\bookmarks\\citrix_vda.json"
    from_beginning: false
    max_bytes_per_line: 1048576
```

| Key | Type | Default | Notes |
|---|---|---|---|
| `paths` | list | **required** | Files or globs; re-expanded every 15s so new/rotated files are picked up. |
| `parser.type` | string | `raw` | `regex` (named groups), `json` (jsonl), `logfmt`, or `raw` (whole line as body). |
| `parser.pattern` | string | — | Required for `regex`; must contain at least one named capture group. |
| `parser.timestamp_field` | string | — | Field/group holding the record timestamp. |
| `parser.timestamp_format` | string | — | Go reference layout; falls back to common layouts + unix epoch. |
| `multiline.pattern` | string | — | Folds continuation lines (stacktraces) into one record. |
| `multiline.match` | string | `after` | `after` = pattern marks a new record's first line; `before` = pattern is appended then flushes. |
| `bookmark_path` | path | — | Persists per-file read offsets; empty = tail from EOF each start. |
| `from_beginning` | bool | `false` | On first sight of a file with no bookmark, read from offset 0. |
| `max_bytes_per_line` | int | 1 MiB | Caps one assembled record to guard against OOM on monstrous lines. |

## Output

OTel logs only — no metrics. Enable the OTLP logs signal so records are
consumed:

```yaml
storage:
  - type: otlp
    signals:
      logs: true
```

Each record carries:

- **Body** — the `message`/`msg`/`body` field when a structured parser
  extracts one, otherwise the raw line.
- **Severity** — derived from a `level`/`severity`/`lvl` field
  (TRACE/DEBUG/INFO/WARN/ERROR/FATAL, case-insensitive).
- **Timestamp** — from `timestamp_field` when configured and parseable,
  otherwise the time the line was read.
- **Attributes** — every parsed field, plus `log.file.path` and the
  `senhub.probe.*` producer identity.

## Rotation & bookmarks (no duplication, no loss)

`nxadm/tail` reopens the file on rotation (`mv app.log app.log.1; touch
app.log`, daily, size-based, or `copytruncate`). With `bookmark_path`
set, per-file offsets are persisted (~every 2s and on stop) and a
restart resumes from the stored offset.

File identity uses a fingerprint (CRC32 of the first 1000 bytes) so a
replaced/truncated file at the same path is not re-read from a stale
offset. Files **smaller** than the fingerprint window have no stable head
hash (it changes as they grow), so identity there falls back to an
offset-vs-size comparison — the one case it cannot resolve is a tiny file
replaced by an unrelated, larger tiny file while the agent was down,
which is an inherent limit of sub-fingerprint-size files.

## Status

Validated end-to-end on macOS/Linux: regex parsing with named groups →
structured OTel logs; tail of a growing file (<5s latency); rotation
(`mv` + recreate) handled with no loss; bookmark resume across restart
with no duplication and no loss. Parser (regex/json/logfmt/raw),
multiline folding, fingerprint stability and bookmark persistence are
unit-tested.
