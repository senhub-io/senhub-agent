# Event Probe - README

## Overview

The Event probe provides a custom HTTP endpoint for receiving application events. Applications can POST JSON events to the agent, which processes and forwards them to configured storage strategies.

## Quick Start

```yaml
# probes.d/10-event.yaml — each file under probes.d/ is a YAML array of probes
- name: event
  params:
    port: 5656
    endpoint: "/events"
```

## Key Features

- **HTTP POST endpoint**: Simple JSON API
- **Event validation**: Schema validation
- **Custom metadata**: Flexible event structure
- **Authentication**: Optional API key validation

## API Endpoint

**POST** `http://localhost:5656/events`

**Request Body**:
```json
{
  "name": "user_login",
  "severity": "info",
  "message": "User admin logged in",
  "metadata": {
    "user_id": "12345",
    "ip_address": "192.168.1.100"
  }
}
```

## Configuration

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `port` | integer | 5656 | HTTP listening port |
| `endpoint` | string | /events | API endpoint path |
| `api_key` | string | - | Optional API key for authentication — reference a stored secret via `${secret:event.api_key}`, `${env:VAR}` or `${file:/path}`. Inline plaintext is auto-sealed into the OS secret store on install. |

## Use Cases

1. **Application Events** - Custom business events
2. **User Actions** - Track user activities
3. **System Events** - Application lifecycle events
4. **Integration Events** - External system notifications

## Related Documentation

- [METRICS.md](./METRICS.md) - Event structure reference
- [Syslog Probe](./README.md) - Network log collection
