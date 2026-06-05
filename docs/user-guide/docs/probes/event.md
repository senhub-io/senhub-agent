!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Event Probe

The Event probe exposes an HTTP endpoint that accepts custom events from applications, scripts, or external systems. Events are validated, enriched with timestamps and tags, and forwarded to the configured strategies (Senhub, PRTG, Nagios) for storage and alerting.

Use cases:
- Application lifecycle events (deployment, restart, upgrade)
- Custom business events (user login, transaction, alert)
- Scripted health checks from cron jobs
- Bridging third-party tooling that cannot push directly to SenHub

## Quick Start

```yaml
probes:
  - name: event
    type: event
    params:
      address: 127.0.0.1   # Bind address (default: 127.0.0.1)
      port: 5656           # Listening port (default: 5656)
      protocol: tcp        # tcp or udp (default: tcp)
```

Once started, the probe listens on `http://<address>:<port>/event` and accepts `POST` requests with a JSON body.

## Configuration Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `address` | string | No | `127.0.0.1` | Bind address. Use `0.0.0.0` to listen on all interfaces |
| `port` | integer | No | `5656` | HTTP listening port (1–65535) |
| `protocol` | string | No | `tcp` | Transport protocol (`tcp` or `udp`) |

## Event Format

Events are sent as JSON via `POST /event`.

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `host` | string | Source host or service name (non-empty) |
| `message` | string | Human-readable event description (non-empty) |
| `severity` | string | Severity level (see values below) |

### Optional Fields

| Field | Type | Description |
|-------|------|-------------|
| `timestamp` | string | ISO 8601 / RFC 3339 timestamp. If omitted, the agent's clock is used |
| any custom key | string / number / array / object | Custom metadata (stored as tags) |

**Maximum fields per event: 20** (including required and custom).

### Supported Severity Values

| Value | Description |
|-------|-------------|
| `EMERG` | System is unusable |
| `ALERT` | Action must be taken immediately |
| `CRIT` | Critical conditions |
| `ERR` | Error conditions |
| `WARNING` | Warning conditions |
| `NOTICE` | Normal but significant |
| `INFO` | Informational |
| `DEBUG` | Debug-level messages |

Values match RFC 5424 syslog severities in upper-case.

## Example Requests

### Minimal Event

```bash
curl -X POST http://localhost:5656/event \
  -H "Content-Type: application/json" \
  -d '{
    "host": "app-01",
    "severity": "INFO",
    "message": "Service started"
  }'
```

### Event with Custom Tags and Timestamp

```bash
curl -X POST http://localhost:5656/event \
  -H "Content-Type: application/json" \
  -d '{
    "host": "web-frontend-03",
    "severity": "ERR",
    "message": "Database connection failed",
    "timestamp": "2026-04-14T15:04:05Z",
    "service": "checkout-api",
    "environment": "production",
    "user_id": "12345"
  }'
```

### Event with Complex Metadata

Arrays and nested objects are preserved and stored in a `_complex_values` tag as JSON:

```bash
curl -X POST http://localhost:5656/event \
  -H "Content-Type: application/json" \
  -d '{
    "host": "ingest-01",
    "severity": "WARNING",
    "message": "Batch processing delayed",
    "queue": "orders",
    "pending": [101, 102, 103],
    "context": {"retry": 2, "backoff": "exponential"}
  }'
```

## Collected Metrics

The Event probe emits a single datapoint per received event:

| Metric | Unit | Description |
|--------|------|-------------|
| `event_event` | Count | Always `1` per event. Use tags to filter and aggregate |

### Tags Attached to Each Event

| Tag | Source | Description |
|-----|--------|-------------|
| `host` | request body | Source host (required field) |
| `severity` | request body | RFC 5424 severity level (required field) |
| `message` | request body | Event message (required field) |
| `<custom>` | request body | Any additional fields sent in the JSON body |
| `_complex_values` | auto-generated | JSON-serialized complex fields (arrays, nested objects) when present |

All custom fields are stored as string tags. Complex types (arrays, objects) are additionally preserved in the `_complex_values` tag in their original JSON form.

## HTTP Responses

| Status | When |
|--------|------|
| `200 OK` | Event accepted and forwarded |
| `400 Bad Request` | Invalid JSON, missing required field, invalid severity, invalid timestamp, or more than 20 fields |
| `405 Method Not Allowed` | Request method is not `POST` |
| `500 Internal Server Error` | Event could not be forwarded to the data store |

## PRTG Integration

Query event metrics via the agent's PRTG endpoint:

```
http://<agent-ip>:<http-port>/api/<agent-key>/prtg/metrics/event
```

Filter by severity or custom tags using the `?tags=` query parameter:

```
http://<agent-ip>:<http-port>/api/<agent-key>/prtg/metrics/event?tags=severity:ERR
```

## Troubleshooting

### `400 Bad Request` — missing required field

All three of `host`, `message`, `severity` must be present and non-empty strings. Check your request body with `jq`.

### `400 Bad Request` — invalid severity value

Severity must be upper-case and exactly match one of: `EMERG`, `ALERT`, `CRIT`, `ERR`, `WARNING`, `NOTICE`, `INFO`, `DEBUG`.

### `400 Bad Request` — invalid timestamp format

Timestamps must be ISO 8601 / RFC 3339 (e.g. `2026-04-14T15:04:05Z` or `2026-04-14T15:04:05+02:00`).

### Port already in use

If another process occupies the default port, change `port` in the probe config. On Linux, ports below 1024 require the agent to run as root or with `CAP_NET_BIND_SERVICE`.

### Event received but not visible in PRTG

Verify the event strategy is enabled in your agent config and that the PRTG query filters (tags, probe name) match the events you are sending.

## Debug Logging

```bash
curl -X POST http://localhost:8080/api/{key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [{"module": "probe.event", "level": "debug"}]}'
```

Or start the agent with:

```bash
./senhub-agent run --verbose --debug-modules probe.event
```
