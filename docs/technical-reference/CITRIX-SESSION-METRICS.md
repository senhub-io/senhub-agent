# Citrix Session Metrics Documentation

## Overview

This document provides detailed documentation for all Citrix session metrics collected by the SenHub Agent. Each metric is explained with its calculation logic, data source, and mapping to the Citrix Director portal.

## Session Metrics

### 1. sessions_connected

**Description:** Number of currently active/connected sessions in the Citrix environment.

**Calculation Logic:**
- **OData Filter:** `ConnectionState eq 5`
- **Citrix Portal Equivalent:** "Active" sessions
- **Technical Details:** Counts sessions with `ConnectionState = 5` (Active state)

**Why ConnectionState = 5 (not 1)?**
Based on analysis of production data, Citrix Director considers sessions "connected" when they are in `ConnectionState = 5` (Active) rather than `ConnectionState = 1` (Connected). In the analyzed environment:
- ConnectionState = 1: 0 sessions (unused)  
- ConnectionState = 5: All active sessions

**Example Value:** 448 sessions

**Tags:**
- `metric_type`: "sessions"
- `frequency`: "1min"
- `data_source`: "session_count"

---

### 2. sessions_disconnected

**Description:** Number of sessions that are disconnected but still exist in memory.

**Calculation Logic:**
- **OData Filter:** `ConnectionState eq 2`
- **Citrix Portal Equivalent:** "Disconnected" sessions
- **Technical Details:** Counts sessions with `ConnectionState = 2` (Disconnected state)

**Business Impact:** 
- Disconnected sessions consume server resources
- Users can reconnect to these sessions
- High numbers may indicate connectivity issues

**Example Value:** 169 sessions

**Tags:**
- `metric_type`: "sessions"
- `frequency`: "1min"
- `data_source`: "session_count"

---

### 3. sessions_zombie

**Description:** Subset of disconnected sessions that have been disconnected for more than 24 hours.

**Calculation Logic:**
1. Get all disconnected sessions (`ConnectionState = 2`)
2. Filter sessions where `SessionStateChangeTime < (current_time - 24h)`
3. Count the filtered sessions

**Business Impact:**
- Zombie sessions waste server resources
- May indicate configuration issues with session timeout policies
- Should be investigated if numbers are consistently high

**Typical Scenario:** When `sessions_zombie = sessions_disconnected`, it means all disconnected sessions have been idle for over 24 hours, suggesting good session cleanup policies.

**Example Value:** 169 sessions (same as disconnected in this case)

**Tags:**
- `metric_type`: "sessions"
- `frequency`: "1min"
- `data_source`: "session_count"
- `time_window`: "24h"

---

### 4. sessions_simultaneous_users

**Description:** Number of unique users who have multiple active sessions simultaneously.

**Calculation Logic:**
1. Get all active sessions (`ConnectionState = 5`)
2. Group sessions by `UserId`
3. Count users who have more than 1 session
4. Return the count of such users

**Business Impact:**
- Indicates users connecting from multiple devices
- Useful for license planning and user behavior analysis
- May indicate shared accounts if numbers are unexpectedly high

**Example Value:** 47 users

**Tags:**
- `metric_type`: "sessions"
- `frequency`: "1min"
- `data_source`: "session_analysis"
- `description`: "users_with_multiple_sessions"

---

### 5. sessions_simultaneous_total

**Description:** Total number of sessions belonging to users who have multiple active sessions.

**Calculation Logic:**
1. Get all active sessions (`ConnectionState = 5`)
2. Group sessions by `UserId`
3. For users with more than 1 session, sum all their sessions
4. Return the total count

**Relationship:** If 47 users have multiple sessions totaling 95 sessions, then on average each multi-session user has ~2 sessions.

**Business Impact:**
- Helps understand multi-device usage patterns
- Important for capacity planning
- License optimization opportunities

**Example Value:** 95 sessions

**Tags:**
- `metric_type`: "sessions"
- `frequency`: "1min"
- `data_source`: "session_analysis"
- `description`: "total_sessions_from_multi_users"

---

## Data Sources and API Endpoints

### Primary Endpoint
- **URL:** `/Sessions`
- **Method:** GET with OData filters
- **Pagination:** Automatic handling of all pages
- **Authentication:** NTLM (basic auth fallback)

### OData Filters Used
```
ConnectionState eq 1    # Connected (typically unused)
ConnectionState eq 2    # Disconnected  
ConnectionState eq 5    # Active (main "connected" state)
```

### Key Fields from API Response
```json
{
  "SessionKey": "guid",
  "UserId": 12345,
  "ConnectionState": 5,
  "SessionStateChangeTime": "2025-06-26T10:30:00Z",
  "ModifiedDate": "2025-06-26T10:30:00Z"
}
```

## Performance Optimizations

### 1. Filtered Queries
Instead of retrieving all sessions and filtering locally, the agent uses OData filters to retrieve only relevant sessions:

```
✅ Optimized: GET /Sessions?$filter=ConnectionState eq 5
❌ Inefficient: GET /Sessions + local filtering
```

### 2. Parallel Requests
Different session states are queried in parallel to reduce collection time.

### 3. Minimal Data Transfer
Only essential fields are processed, reducing network overhead.

## Validation Against Citrix Director

### Test Data Comparison
```
Citrix Director Portal: 472 "Active" sessions
Agent sessions_connected: 486 sessions
Difference: +14 sessions (normal variation)

Citrix Director Portal: 179 "Disconnected" sessions  
Agent sessions_disconnected: 188 sessions
Difference: +9 sessions (normal variation)
```

### Acceptable Variance
- **±5-15 sessions:** Normal due to timing differences
- **>20 sessions:** May indicate configuration or filtering differences
- **>50 sessions:** Requires investigation

## Troubleshooting

### Common Issues

1. **Zero Connected Sessions**
   - **Cause:** Using ConnectionState = 1 instead of 5
   - **Solution:** Verify portal shows "Active" sessions, not "Connected"

2. **High Zombie Sessions**
   - **Cause:** Session timeout policies not configured
   - **Solution:** Review Citrix policies for automatic session cleanup

3. **Unexpected Simultaneous Sessions**
   - **Cause:** Users sharing accounts or legitimate multi-device usage
   - **Solution:** Analyze user patterns and adjust policies if needed

### Debug Commands
```bash
# Enable debug logging for sessions
./agent run --verbose --debug-modules probe.citrix

# Check cache for session metrics
curl http://localhost:8080/api/{agentkey}/debug/cache | grep sessions
```

## Business Intelligence

### Key Performance Indicators (KPIs)

1. **Session Utilization:** `sessions_connected / total_licenses * 100`
2. **Session Health:** `sessions_zombie / sessions_disconnected * 100`
3. **Multi-Device Usage:** `sessions_simultaneous_total / sessions_connected * 100`
4. **User Efficiency:** `sessions_connected / unique_active_users`

### Alerting Thresholds

- **High Connected Sessions:** > 80% of license capacity
- **High Zombie Sessions:** > 20% of total sessions
- **Low Simultaneous Usage:** < 5% (may indicate underutilized licenses)

## Integration Examples

### PRTG JSON Format
```json
{
  "prtg": {
    "result": [
      {
        "channel": "Sessions Connected",
        "value": 448,
        "unit": "Count",
        "mode": "Absolute"
      }
    ]
  }
}
```

### Prometheus Format
```
citrix_sessions_connected{environment="PROD"} 448
citrix_sessions_disconnected{environment="PROD"} 169
citrix_sessions_zombie{environment="PROD"} 169
```

## Changelog

### v2.0 (Current)
- Optimized OData queries with server-side filtering
- Added simultaneous session metrics  
- Corrected ConnectionState mapping (5 = Active)
- Added comprehensive documentation

### v1.0 (Legacy)
- Basic session counting with local filtering
- Used incorrect ConnectionState = 1 for connected sessions
- No simultaneous session analysis