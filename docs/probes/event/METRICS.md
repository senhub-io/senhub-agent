# Event Probe - Metrics Reference

## Introduction

The Event probe is **event-driven** and does not collect traditional numerical metrics. Instead, it receives custom application events via HTTP POST and generates event DataPoints with user-defined fields. Each event is validated and stored with structured metadata.

## Event DataPoint Structure

### Overview

Each received event creates one DataPoint with the following structure:

| Field | Type | Description | Example |
|-------|------|-------------|---------|
| `name` | string | Always "event_event" | "event_event" |
| `timestamp` | datetime | Event timestamp (from message or receipt time) | "2025-01-13T10:30:45Z" |
| `value` | float32 | Constant value (1.0) | 1.0 |
| `tags` | Tag[] | Event metadata (see below) | [...] |

### Required Event Fields

Each HTTP POST event **must** include these fields:

| Field | Type | Required | Description | Example |
|-------|------|----------|-------------|---------|
| `host` | string | ✅ Yes | Source hostname or identifier | "app-server-01" |
| `message` | string | ✅ Yes | Event description | "User login successful" |
| `severity` | string | ✅ Yes | Event severity level | "INFO" |

### Optional Event Fields

| Field | Type | Required | Description | Example |
|-------|------|----------|-------------|---------|
| `timestamp` | string | ❌ No | Event timestamp (ISO8601) | "2025-01-13T10:30:45Z" |
| *custom* | any | ❌ No | Custom application fields | "user_id": "12345" |

**Timestamp Format**: RFC3339/ISO8601 (`YYYY-MM-DDTHH:MM:SSZ`)  
**Maximum Fields**: 20 total fields per event  
**Complex Values**: Arrays and objects are serialized as JSON in special `_complex_values` tag

## Severity Levels

### Level Definitions (Syslog-Compatible)

| Level | Name | Code | Description | Use Case |
|-------|------|------|-------------|----------|
| EMERG | Emergency | 0 | System is unusable | Complete application failure |
| ALERT | Alert | 1 | Action must be taken immediately | Database corruption, data loss |
| CRIT | Critical | 2 | Critical conditions | Application crash, critical failure |
| ERR | Error | 3 | Error conditions | Failed operations, exceptions |
| WARNING | Warning | 4 | Warning conditions | Deprecated features, slow operations |
| NOTICE | Notice | 5 | Normal but significant | Configuration changes, restarts |
| INFO | Informational | 6 | Informational messages | User actions, normal operations |
| DEBUG | Debug | 7 | Debug-level messages | Detailed diagnostics |

### Severity Filtering

**Critical Events Only**:
```
severity: EMERG, ALERT, CRIT
```

**Operational Alerts** (Errors and above):
```
severity: EMERG, ALERT, CRIT, ERR
```

**All Important Events** (Warnings and above):
```
severity: EMERG, ALERT, CRIT, ERR, WARNING
```

**All Events** (Including info/debug):
```
severity: EMERG, ALERT, CRIT, ERR, WARNING, NOTICE, INFO, DEBUG
```

## HTTP API Specification

### Endpoint

**POST** `http://localhost:5656/event`

**Default Configuration**:
- **Address**: 127.0.0.1 (localhost only)
- **Port**: 5656
- **Protocol**: TCP
- **Timeouts**: Read 10s, Write 10s, Idle 120s

### Request Format

**Headers**:
```
Content-Type: application/json
```

**Body** (JSON):
```json
{
  "host": "app-server-01",
  "message": "User login successful",
  "severity": "INFO",
  "timestamp": "2025-01-13T10:30:45Z",
  "user_id": "12345",
  "ip_address": "192.168.1.100",
  "session_id": "abc-def-123"
}
```

### Response Codes

| Code | Status | Description |
|------|--------|-------------|
| 200 | OK | Event processed successfully |
| 400 | Bad Request | Missing required field or invalid format |
| 405 | Method Not Allowed | Non-POST request |
| 500 | Internal Server Error | Failed to process event |

### Success Response

```
HTTP/1.1 200 OK
Content-Type: text/plain

Event processed successfully
```

### Error Responses

**Missing Required Field**:
```json
HTTP/1.1 400 Bad Request

missing required field: host
```

**Invalid Severity**:
```json
HTTP/1.1 400 Bad Request

invalid severity value: INVALID
```

**Too Many Fields**:
```json
HTTP/1.1 400 Bad Request

too many fields, maximum allowed is 20
```

**Invalid Timestamp**:
```json
HTTP/1.1 400 Bad Request

invalid timestamp format, must be ISO8601: parsing time "2025-01-13" as "2006-01-02T15:04:05Z07:00"
```

## Event Validation Rules

### Field Validation

1. **host** (required):
   - Must be non-empty string
   - Typically: hostname, server name, or application identifier
   - Example: `"web-server-01"`, `"api-gateway"`, `"mobile-app"`

2. **message** (required):
   - Must be non-empty string
   - Human-readable event description
   - Maximum recommended length: 500 characters
   - Example: `"User admin logged in from 192.168.1.5"`

3. **severity** (required):
   - Must be one of: `EMERG`, `ALERT`, `CRIT`, `ERR`, `WARNING`, `NOTICE`, `INFO`, `DEBUG`
   - Case-sensitive (uppercase only)
   - Example: `"INFO"`, `"ERR"`, `"WARNING"`

4. **timestamp** (optional):
   - Must be RFC3339/ISO8601 format if provided
   - If omitted, current server time is used
   - Timezone support: `Z` (UTC) or offset `+01:00`
   - Example: `"2025-01-13T10:30:45Z"`, `"2025-01-13T10:30:45+01:00"`

5. **Custom Fields** (optional):
   - Maximum 20 total fields (including required fields)
   - Any JSON-compatible types: string, number, boolean, array, object
   - Complex types (arrays, objects) stored in `_complex_values` tag
   - Example: `"user_id": "12345"`, `"tags": ["login", "success"]`

### Validation Examples

**Valid Event**:
```json
{
  "host": "api-gateway",
  "message": "API request processed",
  "severity": "INFO",
  "timestamp": "2025-01-13T10:30:45Z",
  "endpoint": "/api/users",
  "method": "GET",
  "status_code": 200,
  "response_time_ms": 45
}
```

**Invalid Event** (missing required field):
```json
{
  "message": "Something happened",
  "severity": "INFO"
}
// Error: missing required field: host
```

**Invalid Event** (invalid severity):
```json
{
  "host": "app-server",
  "message": "Event occurred",
  "severity": "INVALID"
}
// Error: invalid severity value: INVALID
```

**Invalid Event** (too many fields):
```json
{
  "host": "app",
  "message": "test",
  "severity": "INFO",
  "field1": "...",
  "field2": "...",
  // ... 18 more fields
}
// Error: too many fields, maximum allowed is 20
```

## Event Processing

### Processing Flow

```
HTTP Client → POST /event → Validation → DataPoint Creation
           → Callback to DataStore → Storage Strategies → SIEM/Database/API
```

### DataPoint Creation

**Input Event**:
```json
{
  "host": "web-server-01",
  "message": "User login successful",
  "severity": "INFO",
  "timestamp": "2025-01-13T10:30:45Z",
  "user_id": "12345",
  "ip_address": "192.168.1.100"
}
```

**Generated DataPoint**:
```go
DataPoint{
  Name:      "event_event",
  Timestamp: time.Parse("2025-01-13T10:30:45Z"),
  Value:     1.0,
  Tags: []Tag{
    {Key: "host", Value: "web-server-01"},
    {Key: "message", Value: "User login successful"},
    {Key: "severity", Value: "INFO"},
    {Key: "user_id", Value: "12345"},
    {Key: "ip_address", Value: "192.168.1.100"},
  }
}
```

### Complex Value Handling

**Input with Arrays/Objects**:
```json
{
  "host": "api-gateway",
  "message": "API request processed",
  "severity": "INFO",
  "tags": ["authentication", "success"],
  "metadata": {
    "user": "admin",
    "role": "superuser"
  }
}
```

**Processing**:
1. Simple values stored as regular tags
2. Complex values (arrays, objects) preserved in `_complex_values` tag:
```json
{
  "_complex_values": "{\"tags\":[\"authentication\",\"success\"],\"metadata\":{\"user\":\"admin\",\"role\":\"superuser\"}}"
}
```

## Use Cases

### 1. Application Event Tracking

**Event Filter**:
```
severity: INFO, NOTICE
host: application servers
```

**Example Events**:
- Application startup/shutdown
- Configuration changes
- Feature flag toggles
- Scheduled task execution

**Sample Event**:
```json
{
  "host": "app-server-01",
  "message": "Application started successfully",
  "severity": "NOTICE",
  "version": "2.5.1",
  "startup_time_ms": 1234
}
```

### 2. User Action Logging

**Event Filter**:
```
severity: INFO, NOTICE
Custom fields: user_id, action
```

**Example Events**:
- User login/logout
- Password changes
- Profile updates
- Permission changes

**Sample Event**:
```json
{
  "host": "auth-service",
  "message": "User password changed",
  "severity": "NOTICE",
  "user_id": "12345",
  "user_email": "admin@company.com",
  "ip_address": "192.168.1.100",
  "timestamp": "2025-01-13T10:30:45Z"
}
```

### 3. Error and Exception Tracking

**Event Filter**:
```
severity: ERR, CRIT, ALERT, EMERG
```

**Example Events**:
- Unhandled exceptions
- Database errors
- API failures
- Third-party integration errors

**Sample Event**:
```json
{
  "host": "payment-service",
  "message": "Payment gateway connection failed",
  "severity": "ERR",
  "error_code": "CONN_TIMEOUT",
  "gateway": "stripe",
  "retry_count": 3,
  "timestamp": "2025-01-13T10:30:45Z"
}
```

### 4. Business Event Tracking

**Event Filter**:
```
severity: INFO, NOTICE
Custom fields: transaction_id, amount
```

**Example Events**:
- Order placement
- Payment processing
- Shipment tracking
- Invoice generation

**Sample Event**:
```json
{
  "host": "ecommerce-api",
  "message": "Order placed successfully",
  "severity": "INFO",
  "order_id": "ORD-12345",
  "customer_id": "CUST-67890",
  "amount": 199.99,
  "currency": "USD",
  "items_count": 3
}
```

### 5. System Integration Events

**Event Filter**:
```
severity: INFO, WARNING, ERR
host: integration services
```

**Example Events**:
- External API calls
- Webhook deliveries
- Data synchronization
- Third-party service status

**Sample Event**:
```json
{
  "host": "integration-service",
  "message": "Webhook delivered successfully",
  "severity": "INFO",
  "webhook_url": "https://api.partner.com/webhook",
  "event_type": "order.created",
  "response_code": 200,
  "response_time_ms": 234
}
```

## Monitoring Best Practices

### Event Volume Metrics

Track event rates and patterns:
- **Events per second** - Monitor ingestion rate
- **Events by severity** - Count critical/error events
- **Events by host** - Identify chatty applications
- **Events by custom field** - Track specific patterns

### Alert Configuration

**Critical Application Errors**:
```yaml
alerts:
  - name: Application Critical Error
    condition: severity == CRIT OR severity == ALERT
    action: page_oncall
    
  - name: High Error Rate
    condition: severity == ERR
    threshold: 10 events in 5 minutes
    action: notify_ops
```

**Business Event Monitoring**:
```yaml
alerts:
  - name: Payment Failures
    condition: message contains "payment" AND severity == ERR
    threshold: 5 events in 1 minute
    action: notify_finance_team
    
  - name: High Order Volume
    condition: message contains "order placed"
    threshold: 100 events in 1 minute
    action: notify_ops
```

### Storage Strategy

**SIEM Integration**:
```yaml
storage:
  - name: event
    params:
      endpoint: "https://siem.company.com/api/events"
      auth_token: "${SIEM_TOKEN}"
      batch_size: 100
```

**Database Storage**:
```yaml
storage:
  - name: http
    params:
      endpoint: "https://events.company.com/ingest"
      buffer_size: 1000
```

### Performance Tuning

**High Volume (>1000 events/sec)**:
- Use event batching in storage strategy
- Increase buffer sizes
- Consider asynchronous processing
- Monitor callback performance

**Low Volume (<100 events/sec)**:
- Default settings work well
- Single agent sufficient
- Real-time processing enabled

## Client Integration Examples

### cURL

**Basic Event**:
```bash
curl -X POST http://localhost:5656/event \
  -H "Content-Type: application/json" \
  -d '{
    "host": "web-server-01",
    "message": "User login successful",
    "severity": "INFO",
    "user_id": "12345"
  }'
```

**Event with Timestamp**:
```bash
curl -X POST http://localhost:5656/event \
  -H "Content-Type: application/json" \
  -d '{
    "host": "api-gateway",
    "message": "API request processed",
    "severity": "INFO",
    "timestamp": "2025-01-13T10:30:45Z",
    "endpoint": "/api/users",
    "method": "GET",
    "status_code": 200
  }'
```

### Python

```python
import requests
import json
from datetime import datetime

def send_event(host, message, severity, **kwargs):
    event = {
        "host": host,
        "message": message,
        "severity": severity,
        "timestamp": datetime.utcnow().isoformat() + "Z",
        **kwargs
    }
    
    response = requests.post(
        "http://localhost:5656/event",
        headers={"Content-Type": "application/json"},
        data=json.dumps(event)
    )
    
    if response.status_code == 200:
        print("Event sent successfully")
    else:
        print(f"Error: {response.text}")

# Usage
send_event(
    host="app-server-01",
    message="User login successful",
    severity="INFO",
    user_id="12345",
    ip_address="192.168.1.100"
)
```

### JavaScript/Node.js

```javascript
const axios = require('axios');

async function sendEvent(host, message, severity, metadata = {}) {
  const event = {
    host,
    message,
    severity,
    timestamp: new Date().toISOString(),
    ...metadata
  };

  try {
    const response = await axios.post('http://localhost:5656/event', event);
    console.log('Event sent successfully');
  } catch (error) {
    console.error('Error sending event:', error.response?.data || error.message);
  }
}

// Usage
sendEvent(
  'web-app',
  'User logged in',
  'INFO',
  { user_id: '12345', session_id: 'abc-def-123' }
);
```

### Go

```go
package main

import (
    "bytes"
    "encoding/json"
    "net/http"
    "time"
)

type Event struct {
    Host      string                 `json:"host"`
    Message   string                 `json:"message"`
    Severity  string                 `json:"severity"`
    Timestamp string                 `json:"timestamp"`
    Metadata  map[string]interface{} `json:",inline"`
}

func sendEvent(host, message, severity string, metadata map[string]interface{}) error {
    event := Event{
        Host:      host,
        Message:   message,
        Severity:  severity,
        Timestamp: time.Now().UTC().Format(time.RFC3339),
        Metadata:  metadata,
    }

    data, err := json.Marshal(event)
    if err != nil {
        return err
    }

    resp, err := http.Post(
        "http://localhost:5656/event",
        "application/json",
        bytes.NewBuffer(data),
    )
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    return nil
}

// Usage
func main() {
    metadata := map[string]interface{}{
        "user_id": "12345",
        "action":  "login",
    }
    
    sendEvent("app-server", "User logged in", "INFO", metadata)
}
```

## Troubleshooting

### No Events Received

**Check Server Status**:
```bash
# Verify probe is listening
netstat -an | grep 5656  # Unix/Linux
netstat -an | findstr 5656  # Windows

# Test with curl
curl -X POST http://localhost:5656/event \
  -H "Content-Type: application/json" \
  -d '{"host":"test","message":"test","severity":"INFO"}'
```

**Enable Debug Logging**:
```bash
./agent run --verbose --debug-modules probe.event
```

### 400 Bad Request Errors

**Missing Required Field**:
```
Error: missing required field: severity
```
**Solution**: Ensure all required fields (host, message, severity) are present

**Invalid Severity**:
```
Error: invalid severity value: info
```
**Solution**: Use uppercase severity levels (INFO, not info)

**Invalid Timestamp**:
```
Error: invalid timestamp format
```
**Solution**: Use ISO8601/RFC3339 format: `2025-01-13T10:30:45Z`

### 500 Internal Server Error

**Symptom**: Events rejected with HTTP 500

**Possible Causes**:
- Callback to DataStore failed
- Storage strategy unavailable
- Buffer full

**Debug**:
```bash
./agent run --verbose --debug-modules probe.event,strategy
```

### High Memory Usage

**Symptom**: Memory usage grows over time

**Solutions**:
- Reduce event rate at source
- Implement batching in client
- Increase storage strategy batch size
- Monitor callback performance

## Security Considerations

### Network Security

- **Firewall**: Restrict access to trusted sources
- **TLS**: Use reverse proxy (nginx) for HTTPS encryption
- **Authentication**: Add custom auth token field in events
- **Rate Limiting**: Implement at reverse proxy level

### Event Validation

- **Field Limits**: Maximum 20 fields enforced
- **Size Limits**: Consider adding maximum message length
- **Sanitization**: Validate/sanitize user-provided data
- **Injection Prevention**: Escape special characters in storage

### Data Privacy

- **PII Handling**: Be cautious with personally identifiable information
- **Field Filtering**: Filter sensitive fields before storage
- **Encryption**: Encrypt events at rest in storage backend
- **Retention**: Define data retention policies

## Performance Characteristics

- **CPU**: Minimal (~0.1% per 100 events/sec)
- **Memory**: ~5MB base + event buffers
- **Network**: Depends on event size and rate
- **Latency**: <5ms processing per event
- **Throughput**: 1000+ events/sec per agent

## Related Documentation

- [Event Probe README](./README.md) - Overview and configuration
- [Syslog Probe](./README.md) - Network syslog collection
- [Windows Events](./WINEVENTS-README.md) - Windows Event Log collection
