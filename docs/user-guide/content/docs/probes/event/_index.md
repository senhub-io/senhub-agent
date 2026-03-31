---
title: "Event"
weight: 9
---

{{< hint warning >}}
**License: Pro** - Requires a Pro or Enterprise license.
{{< /hint >}}

{{< hint info >}}
**Online mode only** - This probe requires an active connection to the SenHub Observability Platform. It is not available in offline mode.
{{< /hint >}}

# Event Probe

## Overview

The Event probe provides a custom HTTP endpoint for receiving application events. Applications can POST JSON events to the agent, which processes and forwards them to configured storage strategies.

## Quick Start

```yaml
probes:
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
| `api_key` | string | - | Optional API key for authentication |

## Use Cases

1. **Application Events** - Custom business events
2. **User Actions** - Track user activities
3. **System Events** - Application lifecycle events
4. **Integration Events** - External system notifications
