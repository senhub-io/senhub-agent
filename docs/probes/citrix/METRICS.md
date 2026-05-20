# Citrix Metrics Complete Reference

This document provides a comprehensive reference for all Citrix metrics collected by the SenHub Agent Citrix probe. Each metric includes detailed calculation methods, data sources, Citrix Director equivalents, and monitoring best practices.

## Table of Contents

- [Introduction](#introduction)
- [Session Metrics](#session-metrics)
- [Logon Performance Metrics](#logon-performance-metrics)
- [Infrastructure Metrics](#infrastructure-metrics)
- [Connection Failure Metrics](#connection-failure-metrics)
- [Multi-Session Machine Fault Metrics](#multi-session-machine-fault-metrics)
- [Analytics Metrics](#analytics-metrics)
- [Metric Tags](#metric-tags)
- [Calculation Details](#calculation-details)
- [Use Cases by Metric](#use-cases-by-metric)
- [Platform Comparison](#platform-comparison)
- [Monitoring Best Practices](#monitoring-best-practices)
- [Advanced Analysis](#advanced-analysis)
- [Troubleshooting](#troubleshooting)

## Introduction

The Citrix probe monitors Citrix Virtual Apps and Desktops (CVAD) environments through two primary APIs:

- **OData API (Director)**: Real-time metrics for sessions, machines, and connections
- **DDC REST API**: Site inventory, delivery group management, and site-based filtering

All metrics are collected at the configured interval (default: 120 seconds) and align with Citrix Director portal calculations for consistency.

### Data Sources

| API Endpoint | Purpose | Authentication |
|-------------|---------|---------------|
| `/Sessions` | Session state and counts | NTLM or Basic |
| `/Connections` | Detailed logon timing data | NTLM or Basic |
| `/Machines` | Infrastructure state and health | NTLM or Basic |
| `/ConnectionFailureLogs` | Connection failure analysis | NTLM or Basic |
| `/ConnectionFailureCategories` | Failure type categorization | NTLM or Basic |
| DDC REST API | Site inventory and filtering | Bearer Token |

## Session Metrics

These metrics track active, disconnected, and simultaneous user sessions in the Citrix environment.

### Basic Session Counts

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `sessions_connected` | Sessions Connected | Gauge | `#` | Number of currently active/connected sessions |
| `sessions_disconnected` | Sessions Disconnected | Gauge | `#` | Number of sessions in disconnected state |

**Tags:** `metric_type=sessions`, `frequency=2min`, `data_source=session_count`

**Example Values:**
- `sessions_connected`: `448` sessions
- `sessions_disconnected`: `169` sessions

**Calculation Logic:**

**sessions_connected:**
- **OData Filter**: `ConnectionState eq 5` (Active state)
- **Citrix Director Equivalent**: "Active" sessions
- **Why ConnectionState = 5?**: Based on production analysis, Director considers sessions "connected" when in Active state (5) rather than Connected state (1)

**sessions_disconnected:**
- **OData Filter**: `ConnectionState eq 2` (Disconnected state)
- **Citrix Director Equivalent**: "Disconnected" sessions
- **Business Impact**: Disconnected sessions consume server resources; high numbers may indicate connectivity issues

**Connection State Reference:**
```
0 = Unknown
1 = Connected (typically unused)
2 = Disconnected
3 = Terminated
4 = PreparingSession
5 = Active (primary "connected" state)
6 = Reconnecting
8 = Other
9 = Pending
```

### Advanced Session Metrics

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `sessions_zombie` | Zombie Sessions | Gauge | `#` | Sessions disconnected for more than 24 hours |
| `sessions_simultaneous_users` | Simultaneous Users | Gauge | `#` | Unique users with multiple active sessions |
| `sessions_simultaneous_total` | Total Simultaneous Sessions | Gauge | `#` | Total sessions from users with multiple sessions |

**Tags:** `metric_type=sessions`, `frequency=2min`

**Example Values:**
- `sessions_zombie`: `169` sessions
- `sessions_simultaneous_users`: `47` users
- `sessions_simultaneous_total`: `95` sessions

**Calculation Logic:**

**sessions_zombie:**
1. Get all disconnected sessions (`ConnectionState = 2`)
2. Filter sessions where `SessionStateChangeTime < (current_time - 24h)`
3. Count filtered sessions
4. **Business Impact**: Zombie sessions waste resources; may indicate session timeout policy issues

**sessions_simultaneous_users:**
1. Get all active sessions (`ConnectionState = 5`)
2. Group sessions by `UserId`
3. Count users who have more than 1 session
4. **Business Impact**: Useful for license planning and detecting shared accounts

**sessions_simultaneous_total:**
1. Get all active sessions (`ConnectionState = 5`)
2. Group sessions by `UserId`
3. For users with > 1 session, sum all their sessions
4. **Relationship**: 47 users with 95 sessions = ~2 sessions per multi-session user

**Use Cases:**
- **License optimization**: Track multi-device usage patterns
- **Session health**: Monitor zombie ratio (`sessions_zombie / sessions_disconnected`)
- **User behavior**: Analyze multi-device adoption (`sessions_simultaneous_total / sessions_connected`)
- **Capacity planning**: Peak usage patterns and resource allocation

## Logon Performance Metrics

These metrics measure the time required for users to establish sessions and complete the logon process.

### 2-Minute Window Metrics (Logon Breakdown)

All logon breakdown metrics use a **complete 2-minute window** calculation aligned on minute boundaries.

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `logon_brokering` | Brokering Duration | Gauge | `s` | Average brokering phase duration |
| `logon_vmstart` | VM Start Duration | Gauge | `s` | Average VM start duration |
| `logon_hdx` | HDX Connection Duration | Gauge | `s` | Average HDX connection establishment |
| `logon_authentication` | Authentication Duration | Gauge | `s` | Average authentication duration |
| `logon_gpo` | GPO Processing Duration | Gauge | `s` | Average Group Policy processing time |
| `logon_scripts` | Logon Scripts Duration | Gauge | `s` | Average logon scripts execution time |
| `logon_profile` | Profile Load Duration | Gauge | `s` | Average user profile load time |
| `logon_interactive` | Interactive Session Duration | Gauge | `s` | Average interactive session start time |
| `logon_duration_total` | Total Logon Duration | Gauge | `s` | Average total logon duration |
| `logon_sessions_opened` | Sessions Opened | Gauge | `#` | Number of new sessions started |

**Tags:** `metric_type=logon`, `frequency=2min`, `time_window=2min`

**Example Values:**
- `logon_brokering`: `2.45` seconds
- `logon_authentication`: `0.04` seconds
- `logon_gpo`: `3.21` seconds
- `logon_profile`: `5.67` seconds
- `logon_duration_total`: `18.34` seconds
- `logon_sessions_opened`: `12` sessions

**Calculation Method:**
1. At collection time (e.g., 14:07:30), analyze complete 2-minute window (14:05:00 to 14:07:00)
2. Get all `Connections` with `LogOnStartDate` in time window
3. Calculate phase durations using timestamp fields
4. Average durations across all connections
5. Round to 2 decimal places for precision

**Phase Duration Calculations:**

| Phase | Calculation | Source |
|-------|------------|--------|
| Brokering | `BrokeringDuration` (ms) → seconds | Direct field |
| VM Start | `VMStartEndDate - VMStartStartDate` | Timestamp calculation |
| HDX Connection | `HdxEndDate - HdxStartDate` | Timestamp calculation |
| Authentication | `AuthenticationDuration` (ms) → seconds | Direct field |
| GPO Processing | `GpoEndDate - GpoStartDate` | Timestamp calculation |
| Logon Scripts | `LogOnScriptsEndDate - LogOnScriptsStartDate` | Timestamp calculation |
| Profile Load | `ProfileLoadEndDate - ProfileLoadStartDate` | Timestamp calculation |
| Interactive Session | `InteractiveEndDate - InteractiveStartDate` | Timestamp calculation |
| Total Logon | `LogOnEndDate - LogOnStartDate` | Timestamp calculation |

**Important Notes:**
- Only connections with valid (non-zero) timestamps are included
- Zero metrics returned when no connections in time window
- Phases may overlap according to Citrix documentation
- Data source: `Connections` endpoint (more reliable than `Sessions` for timing)

### 1-Hour Average Metric

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `logon_duration_avg_1h` | Logon Duration Average (1h) | Gauge | `s` | Average logon time over 1-hour rolling window |

**Tags:** `metric_type=logon`, `frequency=2min`, `time_window=1h`

**Example Value:** `18` seconds

**Calculation Method:**
1. Get all `Connections` from last 1 hour
2. Filter: `Protocol = "HDX"` AND `LogOnEndDate != null` AND `IsReconnect = false`
3. Calculate duration: `LogOnEndDate - LogOnStartDate` for each connection
4. Average all durations and round to whole seconds

**Director Console Alignment:**
- Matches Director "Average Logon Duration" (1-hour view)
- Excludes reconnections (per Director filtering logic)
- HDX-only connections (ignores ICA Direct, etc.)
- Only complete sessions with valid `LogOnEndDate`

**Use Cases:**
- **Performance baseline**: Track logon performance trends
- **SLA monitoring**: Alert on logon duration > threshold
- **Bottleneck identification**: Correlate with phase breakdown metrics
- **Capacity planning**: Identify peak logon times and infrastructure needs

## Infrastructure Metrics

These metrics monitor the health and state of Citrix machines (VDAs) in the environment.

### Machine State Metrics

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `machines_total` | Total Machines | Gauge | `#` | Total machines in environment |
| `machines_registered` | Registered Machines | Gauge | `#` | Machines successfully registered with controller |
| `unregistered_vda_count` | Unregistered VDA Count | Gauge | `#` | VDA agents not communicating with controller |
| `machines_faulty` | Faulty Machines | Gauge | `#` | Machines with fault states |
| `machines_maintenance` | Machines in Maintenance | Gauge | `#` | Machines in maintenance mode |

**Tags:** `metric_type=infrastructure`, `frequency=2min`, `data_source=machines`

**Example Values:**
- `machines_total`: `259` machines
- `machines_registered`: `247` machines
- `unregistered_vda_count`: `12` machines
- `machines_faulty`: `5` machines
- `machines_maintenance`: `3` machines

**Calculation Logic:**

**Machine Registration States:**
```
0 = Unregistered - VDA not communicating with controller
1 = Registered   - VDA successfully registered and available
2 = AgentError   - VDA communication error
```

**Machine Fault States (`MachineFaultStateCode`):**
```
0 = Unknown           - Fault state unknown
1 = None              - Healthy machine (no faults)
2 = FailedToStart     - Last power-on operation failed
3 = StuckOnBoot       - Machine might not have booted properly
4 = Unregistered      - Machine unregistered from controller
5 = MaxCapacity       - Machine at maximum session capacity
6 = VMNotFound        - Virtual machine not found in hypervisor
```

**Metric Calculations:**
- `machines_total`: Count of all `Machines` entities
- `machines_registered`: Count where `RegistrationState = 1`
- `unregistered_vda_count`: Count where `RegistrationState = 0 or 2`
- `machines_faulty`: Count where `FaultState != 1` (not None/Healthy)
- `machines_maintenance`: Count where `InMaintenanceMode = true`

**Data Source:** `Machines` OData endpoint

**Use Cases:**
- **Infrastructure health**: Monitor VDA registration status
- **Capacity management**: Track available vs. faulty machines
- **Maintenance planning**: Identify machines needing attention
- **Alerting**: Critical alerts when `unregistered_vda_count` spikes

## Connection Failure Metrics

These metrics track and categorize connection failures to help identify infrastructure issues.

### Connection Failure Breakdown

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `total` | Total Connection Failures | Gauge | `#` | Total connection failures in last hour |
| `client_connection_failures` | Client Connection Failures | Gauge | `#` | Client-side connection issues |
| `configuration_errors` | Configuration Errors | Gauge | `#` | Configuration or setup errors |
| `machine_failures` | Machine Failures | Gauge | `#` | Machine or VDA failures |
| `capacity_unavailable` | Capacity Unavailable | Gauge | `#` | Capacity or resource issues |
| `licenses_unavailable` | Licenses Unavailable | Gauge | `#` | Licensing issues |
| `other_failures` | Other Failures | Gauge | `#` | Other or unknown issues |

**Tags:** `metric_type=connection_failures`, `frequency=2min`, `time_window=1h`

**Example Values:**
- `total`: `15` failures
- `client_connection_failures`: `8` failures
- `machine_failures`: `4` failures
- `capacity_unavailable`: `2` failures
- `configuration_errors`: `1` failure

**Calculation Method:**
1. Get `ConnectionFailureLogs` from last 1 hour
2. Get `ConnectionFailureCategories` for dynamic mapping
3. Map `ConnectionFailureEnumValue` → `Category` → Failure Type
4. Count failures per type

**Dynamic Category Mapping:**

The system uses a hybrid approach:
1. **Dynamic mapping** from `ConnectionFailureCategories` endpoint (adapts to environment)
2. **Static conversion** to consistent failure types (no internet required)

Example mapping (varies by environment):
```
Category 0 → Configuration errors
Category 1 → Client connection failures
Category 2 → Machine failures
Category 3 → Capacity unavailable
Category 4 → Licenses unavailable
Category 5 → Other failures
```

**Citrix Director Equivalents:**
- `total`: "Nbre total d'échecs de connexion"
- `client_connection_failures`: "Échecs de connexion client"
- `configuration_errors`: "Erreurs de configuration"
- `machine_failures`: "Machines défectueuses"
- `capacity_unavailable`: "Capacité non disponible"
- `licenses_unavailable`: "Licences non disponibles"
- `other_failures`: "Autres échecs"

**Data Sources:**
- `ConnectionFailureLogs` OData endpoint
- `ConnectionFailureCategories` OData endpoint

**Use Cases:**
- **Troubleshooting**: Identify most common failure types
- **Alerting**: Critical alerts on failure rate spikes
- **Capacity planning**: Track capacity-related failures
- **License management**: Monitor license availability issues

## Multi-Session Machine Fault Metrics

These metrics provide detailed fault state breakdown for multi-session machines (server VDAs).

### Multi-Session Fault Breakdown

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `machines_total` | Total Faulty Multi-Session Machines | Gauge | `#` | Total multi-session machines with faults |
| `boot_failure` | Boot Failure Machines | Gauge | `#` | Machines that failed to boot |
| `stuck_at_boot` | Machines Stuck at Boot | Gauge | `#` | Machines stuck during boot process |
| `unregistered` | Unregistered Machines | Gauge | `#` | Multi-session machines unregistered |
| `max_capacity` | Max Capacity Machines | Gauge | `#` | Machines at maximum session capacity |
| `vm_not_found` | VM Not Found | Gauge | `#` | Virtual machines not found in hypervisor |
| `unknown` | Unknown Fault State | Gauge | `#` | Machines with unknown fault states |

**Tags:** `metric_type=multi_session_faults`, `frequency=2min`, `data_source=machines`

**Example Values:**
- `machines_total`: `3` machines
- `unregistered`: `2` machines
- `boot_failure`: `1` machine

**Calculation Logic:**

**Filtering Criteria:**
- Machines with valid `MachineName` (excludes phantom machines)
- Machines with valid `DesktopGroupId` (excludes infrastructure machines)
- Note: `SessionSupport` field not reliable for multi/single-session distinction

**Fault State Mapping:**
```
FaultState = 0  → unknown
FaultState = 2  → boot_failure
FaultState = 3  → stuck_at_boot
FaultState = 4  → unregistered
FaultState = 5  → max_capacity
FaultState = 6  → vm_not_found

RegistrationState != 1  → unregistered (additional check)
```

**Citrix Director Equivalents:**
- `machines_total`: "Total des machines avec OS multi-session défectueuses"
- `boot_failure`: "Échec du démarrage"
- `stuck_at_boot`: "Bloquées au démarrage"
- `unregistered`: "Non enregistrées"
- `max_capacity`: "Charge maximale"
- `vm_not_found`: "Machine virtuelle introuvable"
- `unknown`: "Inconnue"

**Data Source:** `Machines` OData endpoint

**Use Cases:**
- **Infrastructure health**: Monitor multi-session VDA health
- **Capacity planning**: Track machines at max capacity
- **Troubleshooting**: Identify boot and registration issues
- **Alerting**: Critical alerts on unregistered or faulty machines

## Analytics Metrics

These metrics provide advanced analytics including Black Hole Machine detection, zombie sessions, and overall environment health scoring.

### Analytics Metrics List

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `black_hole_machines_count` | Black Hole Machines Count | Gauge | `#` | Number of Black Hole machines detected |
| `black_hole_machine_users_affected` | Users Affected per Black Hole | Gauge | `#` | Unique users affected by each Black Hole machine |
| `sessions_zombie` | Zombie Sessions | Gauge | `#` | Hidden/zombie sessions not visible in Director |
| `health_score` | Environment Health Score | Gauge | `0-100` | Overall environment health rating |

**Tags:**
- `metric_type=analytics`
- `black_hole_machine_users_affected` includes `machine=<machine_name>` tag

**Example Values:**
- `black_hole_machines_count`: `1` machine
- `black_hole_machine_users_affected{machine="SVR-XEN-001"}`: `5` users
- `sessions_zombie`: `15` sessions
- `health_score`: `95.5` score

**Calculation Logic:**

### Black Hole Machine Detection

**Official Citrix Definition:**
> "Black hole VDA failures are detected when **three or more unique users fail to connect** to the same multi-session VDA."

**Reference:** [Citrix Performance Analytics - Insights](https://docs.citrix.com/en-us/performance-analytics/insights.html)

**Detection Algorithm:**
1. Get `ConnectionFailureLogs` from last 24 hours
2. Group failures by `MachineId`
3. Count unique `UserName` values per machine
4. Machines with ≥ 3 unique user failures = Black Hole
5. Return count of Black Hole machines
6. Return per-machine user count with machine tag

**Time Window Behavior:**
- **Detection Frequency**: Every 2 minutes (probe interval)
- **Analysis Window**: Exactly 24 hours (rolling window)
- **Citrix Analytics Frequency**: Every 15 minutes (for comparison)

SenHub Agent provides **faster detection** (2 min vs. 15 min) while maintaining the same 24-hour analysis window.

**Configuration:**
- **24-hour window**: Fixed (Citrix standard)
- **3 unique users threshold**: Fixed (Citrix standard)
- No additional configuration required

### Zombie Session Detection

**Zombie sessions** are identified by the `Hidden` property in the Citrix API.

```go
// From Citrix Monitor API documentation
type Session struct {
    Hidden bool  // true = zombie session (not visible in Director)
    // other fields...
}
```

**Reference:** [Citrix Monitor API Deep Dive - Hidden Sessions](https://www.citrix.com/blogs/2022/09/07/diving-deep-into-the-monitor-api/)

**Calculation:**
1. Get all `Sessions` entities
2. Count sessions where `Hidden = true`

### Health Score Calculation

The health score provides a 0-100 rating of environment health based on connection failure rate.

**Formula:**
```
Health Score = 100 - (Failure Rate × 100)
Failure Rate = Failed Connections / Total Connection Attempts
```

**Interpretation:**
- **100**: Perfect health (no failures)
- **90-99**: Good health (minimal failures)
- **70-89**: Warning state (noticeable failures)
- **0-69**: Critical state (high failure rate)

**Use Cases:**
- **Executive dashboards**: Single health score for environment
- **Trend analysis**: Track health over time
- **SLA reporting**: Quantify environment reliability
- **Alerting**: Critical alerts when health score < 70

## Metric Tags

All Citrix metrics include standard tags for filtering and aggregation.

### Standard Tags

| Tag Name | Type | Description | Example |
|----------|------|-------------|---------|
| `hostname` | string | Agent hostname | `citrix-monitor-01` |
| `os` | string | Operating system | `linux`, `windows` |
| `arch` | string | CPU architecture | `amd64` |
| `probe_name` | string | Probe identifier | `citrix` |
| `metric_type` | string | Metric category | `sessions`, `logon`, `infrastructure` |
| `environment` | string | Citrix environment name | `PROD`, `TEST` |
| `frequency` | string | Collection frequency | `2min` |
| `data_source` | string | Data source endpoint | `session_count`, `machines` |
| `time_window` | string | Analysis time window | `2min`, `1h`, `24h` |

### Metric Type Categories

| Metric Type | Metrics Included | Purpose |
|------------|------------------|---------|
| `sessions` | Session counts (connected, disconnected, zombie, simultaneous) | Session state monitoring |
| `logon` | Logon performance (breakdown phases, averages) | Logon experience tracking |
| `infrastructure` | Machine states (registered, faulty, maintenance) | Infrastructure health |
| `connection_failures` | Failure breakdown by category | Troubleshooting connection issues |
| `multi_session_faults` | Multi-session machine fault states | Server VDA health monitoring |
| `analytics` | Black Hole detection, zombie sessions, health score | Advanced analytics |

### Tag Usage Examples

**Filter by metric type:**
```promql
citrix_sessions_connected{metric_type="sessions"}
citrix_logon_duration_total{metric_type="logon"}
```

**Filter by environment:**
```promql
citrix_sessions_connected{environment="PROD"}
```

**Filter by time window:**
```promql
citrix_logon_duration_avg_1h{time_window="1h"}
```

**Aggregate across all failure types:**
```promql
sum(citrix_connection_failures{metric_type="connection_failures"})
```

## Calculation Details

### OData Query Examples

**Active Sessions:**
```http
GET /Sessions?$filter=ConnectionState eq 5
```

**Disconnected Sessions:**
```http
GET /Sessions?$filter=ConnectionState eq 2
```

**Recent Connections (2 minutes):**
```http
GET /Connections?$filter=LogOnStartDate ge 2024-06-27T14:05:00Z and LogOnStartDate lt 2024-06-27T14:07:00Z
```

**Connection Failures (1 hour):**
```http
GET /ConnectionFailureLogs?$filter=FailureDate ge 2024-06-27T13:00:00Z
```

**Registered Machines:**
```http
GET /Machines?$filter=RegistrationState eq 1
```

**Multi-Session Faulty Machines:**
```http
GET /Machines?$filter=FaultState ne 1 and DesktopGroupId ne null and MachineName ne null
```

### API Response Field Mapping

The Citrix OData API returns different field names than documented:

| Expected Field | Actual API Field | Usage |
|----------------|------------------|-------|
| `MachineName` | `Name` | Machine display name |
| `MachineId` | `Id` | Machine unique identifier |
| `SessionSupport` | Not available | Cannot distinguish multi/single-session |
| `DesktopGroupId` | `DesktopGroupId` | Identifies real vs. phantom machines |

### Time Window Precision

Different metrics use different time windows for analysis:

| Time Window | Metrics | Alignment | Purpose |
|------------|---------|-----------|---------|
| **2 minutes** | Logon breakdown phases | Complete previous minutes | Detailed logon analysis |
| **1 hour** | Logon avg, connection failures | Exact 60-minute lookback | Performance trending |
| **24 hours** | Black Hole detection | Exact 24-hour lookback | Long-term failure patterns |

**2-Minute Calculation Example:**
```
Collection Time: 14:07:30
Analysis Window: 14:05:00 to 14:07:00 (complete previous 2 minutes)
```

**1-Hour Calculation Example:**
```
Collection Time: 14:07:30
Analysis Window: 13:07:30 to 14:07:30 (exact 60-minute lookback)
```

### Session Analysis Algorithms

**Simultaneous Users Calculation (SQL Equivalent):**
```sql
-- Count users with multiple sessions
SELECT COUNT(DISTINCT UserId)
FROM (
  SELECT UserId, COUNT(*) as SessionCount
  FROM Sessions
  WHERE ConnectionState = 5
  GROUP BY UserId
  HAVING COUNT(*) > 1
)
```

**Simultaneous Total Calculation (SQL Equivalent):**
```sql
-- Sum sessions from multi-session users
SELECT SUM(SessionCount)
FROM (
  SELECT UserId, COUNT(*) as SessionCount
  FROM Sessions
  WHERE ConnectionState = 5
  GROUP BY UserId
  HAVING COUNT(*) > 1
)
```

**Zombie Sessions Calculation (SQL Equivalent):**
```sql
-- Count disconnected sessions > 24h
SELECT COUNT(*)
FROM Sessions
WHERE ConnectionState = 2
  AND SessionStateChangeTime < (NOW() - INTERVAL '24 hours')
```

## Use Cases by Metric

### Performance Monitoring

| Goal | Primary Metrics | Alert Threshold |
|------|----------------|----------------|
| High session load | `sessions_connected` | > 80% of capacity |
| Slow logon | `logon_duration_avg_1h` | > 30 seconds |
| Logon bottleneck | `logon_gpo`, `logon_profile` | > 10 seconds per phase |
| Infrastructure health | `machines_faulty`, `unregistered_vda_count` | > 5% of total machines |

### User Experience

| Goal | Primary Metrics | Alert Threshold |
|------|----------------|----------------|
| Overall UX | `health_score` | < 90 |
| Connection issues | `total` (failures) | > 10 failures/hour |
| Session quality | `sessions_zombie / sessions_disconnected` | > 30% |
| Multi-device usage | `sessions_simultaneous_total / sessions_connected` | Track trend |

### Capacity Planning

| Goal | Primary Metrics | Alert Threshold |
|------|----------------|----------------|
| License utilization | `sessions_connected / total_licenses` | > 80% |
| Machine capacity | `max_capacity` (multi-session faults) | > 0 machines |
| Infrastructure growth | `machines_total`, `sessions_connected` | Track trend |
| Peak usage | `sessions_connected` (time series) | Track patterns |

### Troubleshooting

| Goal | Primary Metrics | Alert Threshold |
|------|----------------|----------------|
| Black Hole VDAs | `black_hole_machines_count` | > 0 machines |
| Registration issues | `unregistered_vda_count` | > 0 machines |
| Boot problems | `boot_failure`, `stuck_at_boot` | > 0 machines |
| License issues | `licenses_unavailable` | > 0 failures |

## Platform Comparison

### Citrix Director vs. SenHub Agent

| Concept | Citrix Director | SenHub Agent | Alignment |
|---------|----------------|--------------|-----------|
| Active Sessions | "Sessions Connected" | `sessions_connected` | ✅ ConnectionState = 5 |
| Disconnected Sessions | "Sessions Disconnected" | `sessions_disconnected` | ✅ ConnectionState = 2 |
| Logon Duration (1h) | "Average Logon Duration" | `logon_duration_avg_1h` | ✅ Same filtering |
| Connection Failures | "Nbre total d'échecs" | `total` | ✅ Same 1h window |
| Black Hole VDAs | Analytics (15 min) | `black_hole_machines_count` | ✅ Same logic (faster detection) |
| Faulty Machines | "Machines défectueuses" | `machines_faulty` | ✅ FaultState != 1 |

### Data Collection Comparison

| Aspect | Citrix Director | SenHub Agent |
|--------|----------------|--------------|
| **Collection Frequency** | Real-time (UI) | Configurable (default: 2 min) |
| **Black Hole Detection** | Every 15 minutes | Every 2 minutes (faster) |
| **Data Source** | OData API (same) | OData API + DDC REST API |
| **Historical Data** | Limited in UI | Full retention in time-series DB |
| **Filtering** | UI-based | OData filters (server-side) |
| **Alerting** | Via Citrix Cloud | Via monitoring system (PRTG, Prometheus) |

## Monitoring Best Practices

### Alert Configurations

**Critical Alerts:**
```yaml
# High session load
- alert: CitrixHighSessionLoad
  expr: citrix_sessions_connected / citrix_license_capacity > 0.8
  for: 5m
  severity: warning

# Slow logon performance
- alert: CitrixSlowLogon
  expr: citrix_logon_duration_avg_1h > 30
  for: 10m
  severity: warning

# Infrastructure issues
- alert: CitrixUnregisteredVDAs
  expr: citrix_unregistered_vda_count > 0
  for: 5m
  severity: critical

# Black Hole VDA detected
- alert: CitrixBlackHoleMachine
  expr: citrix_black_hole_machines_count > 0
  for: 15m
  severity: critical

# Poor environment health
- alert: CitrixPoorHealth
  expr: citrix_health_score < 70
  for: 15m
  severity: critical
```

**Warning Alerts:**
```yaml
# High zombie session ratio
- alert: CitrixHighZombieRatio
  expr: citrix_sessions_zombie / citrix_sessions_disconnected > 0.5
  for: 30m
  severity: warning

# Connection failures increasing
- alert: CitrixConnectionFailures
  expr: rate(citrix_total[1h]) > 10
  for: 15m
  severity: warning

# Machines at max capacity
- alert: CitrixMaxCapacity
  expr: citrix_max_capacity > 0
  for: 10m
  severity: warning
```

### Dashboard Panels

**Essential Citrix Panels:**

1. **Session Overview Dashboard**
   - Current Sessions (Connected, Disconnected)
   - Zombie Session Ratio
   - Multi-Device Usage
   - Session Trend (24h)

2. **Logon Performance Dashboard**
   - Average Logon Duration (1h)
   - Logon Phase Breakdown (stacked bar)
   - Sessions Opened per Hour
   - Logon Duration Trend

3. **Infrastructure Health Dashboard**
   - Total Machines / Registered / Faulty
   - Multi-Session Fault Breakdown
   - Unregistered VDA Trend
   - Maintenance Mode Machines

4. **Connection Failures Dashboard**
   - Total Failures (1h)
   - Failure Breakdown by Category
   - Black Hole Machines Count
   - Users Affected by Black Hole

5. **Environment Health Dashboard**
   - Health Score Gauge (0-100)
   - Health Score Trend (7d)
   - Top Failure Categories
   - Critical Alerts Summary

### Collection Intervals

| Monitoring Type | Interval | Reason |
|----------------|----------|--------|
| Real-time | 60s | Catch rapid session changes |
| Standard | 120s | Balance accuracy/API load |
| Long-term | 300s | Historical trending |

**Recommendation:** Use 120 seconds (2 minutes) for production to align with Director's 2-minute metrics window.

### Performance Optimization

**API Call Optimization:**

1. **Server-Side Filtering** - Use OData `$filter` to reduce data transfer
   ```
   ✅ GET /Sessions?$filter=ConnectionState eq 5
   ❌ GET /Sessions (then filter locally)
   ```

2. **Parallel Requests** - Query different endpoints simultaneously
   - Sessions, Connections, Machines in parallel
   - Reduces total collection time

3. **Cached Lookups** - Cache desktop group names and failure categories
   - Reduces redundant API calls
   - 5-minute cache TTL recommended

4. **Selective Processing** - Only process relevant machines
   - Multi-session fault metrics: filter by `DesktopGroupId` and `MachineName`
   - Excludes phantom and infrastructure machines

**Expected Metric Volume:**

Approximate metrics per collection cycle:
- **Session metrics**: 5 metrics
- **Logon metrics**: 10 metrics (when sessions active)
- **Infrastructure metrics**: 5 base + 7 fault state metrics
- **Failure metrics**: 7 metrics
- **Analytics metrics**: 3+ metrics (including per-machine Black Hole)

**Total**: ~30-35 metrics per collection cycle

## Advanced Analysis

### Session Health Analysis

**Zombie Session Ratio:**
```
Zombie Ratio % = (sessions_zombie / sessions_disconnected) × 100

Interpretation:
- < 20%: Excellent session cleanup
- 20-50%: Good session management
- 50-80%: Review session timeout policies
- > 80%: Critical - investigate immediately
```

**Multi-Device Usage Analysis:**
```
Multi-Device % = (sessions_simultaneous_total / sessions_connected) × 100

Typical Values:
- 10-20%: Normal multi-device usage
- 20-40%: High multi-device adoption
- > 40%: Investigate potential shared accounts
```

**Session Efficiency:**
```
Average Sessions per Multi-User = sessions_simultaneous_total / sessions_simultaneous_users

Example:
95 sessions / 47 users = ~2 sessions per multi-device user
```

### Logon Performance Analysis

**Phase Bottleneck Identification:**
```
Identify phases taking > 10 seconds:
- logon_gpo > 10s: Review Group Policy structure
- logon_profile > 10s: Check profile size and storage speed
- logon_scripts > 10s: Optimize logon script execution
- logon_hdx > 5s: Network connectivity issues
```

**Total Logon Trend Analysis:**
```
Compare current vs. baseline:
- Current: 18s
- Baseline: 15s
- Increase: +3s (+20%)

Action: Investigate recent infrastructure or policy changes
```

### Infrastructure Health Analysis

**VDA Registration Health:**
```
Registration % = (machines_registered / machines_total) × 100

Targets:
- > 95%: Healthy infrastructure
- 90-95%: Acceptable (monitor closely)
- < 90%: Critical (investigate unregistered VDAs)
```

**Fault State Distribution:**
```
Analyze multi-session fault breakdown:
- High boot_failure: Check hypervisor/storage
- High unregistered: Network or DDC connectivity
- High max_capacity: Capacity planning needed
- High vm_not_found: Provisioning or hypervisor issues
```

### Connection Failure Analysis

**Failure Rate Calculation:**
```
Failure Rate % = (total_failures / total_connection_attempts) × 100

Health Score Validation:
health_score = 100 - failure_rate

Example:
- Failures: 15/hour
- Attempts: 1000/hour
- Failure Rate: 1.5%
- Health Score: 98.5
```

**Failure Category Priorities:**
```
Priority 1 (Critical): licenses_unavailable, capacity_unavailable
Priority 2 (High): machine_failures, configuration_errors
Priority 3 (Medium): client_connection_failures
Priority 4 (Low): other_failures
```

### Black Hole Machine Analysis

**Black Hole Impact Assessment:**
```
Per-Machine User Impact:
black_hole_machine_users_affected{machine="SVR-XEN-001"} = 5 users

Total User Impact:
Sum of all black_hole_machine_users_affected metrics

Action Priority:
- > 5 users affected: Critical - immediate action
- 3-5 users: High - investigate within 1 hour
```

**Black Hole Root Cause Analysis:**
```
Correlate with other metrics:
1. Check machine_failures for same machine
2. Review boot_failure or unregistered state
3. Analyze connection failure logs for machine
4. Check infrastructure metrics (CPU, memory, disk)
```

## Troubleshooting

### Common Issues

**Issue: Zero Connected Sessions**

**Symptoms:**
- `sessions_connected = 0`
- Director shows active sessions

**Causes:**
- Using incorrect `ConnectionState` filter
- OData API connectivity issues
- Authentication failure

**Resolution:**
1. Verify OData query: `$filter=ConnectionState eq 5` (not 1)
2. Test API manually with Postman
3. Check authentication credentials
4. Enable debug logging: `--debug-modules probe.citrix`

---

**Issue: High Zombie Sessions**

**Symptoms:**
- `sessions_zombie ≈ sessions_disconnected`
- All disconnected sessions > 24h

**Causes:**
- Session timeout policies not configured
- Users not logging off properly
- Session cleanup jobs not running

**Resolution:**
1. Review Citrix session timeout policies
2. Configure automatic session cleanup (e.g., 8 hours)
3. Enable session linger timeout
4. Investigate why users don't log off

---

**Issue: Incorrect Logon Duration**

**Symptoms:**
- `logon_duration_avg_1h` doesn't match Director
- Significant discrepancy (e.g., 11s vs 18s)

**Causes:**
- Different calculation methods
- Reconnections not excluded
- Phase overlap not accounted for

**Resolution:**
1. Verify filtering: HDX-only, no reconnections, LogOnEndDate != null
2. Check if using `Session.LogOnDuration` vs. calculated durations
3. Compare phase breakdown metrics with Director
4. Enable debug logging to see calculation details

---

**Issue: Missing Black Hole Machines**

**Symptoms:**
- `black_hole_machines_count = 0`
- Director shows Black Hole alerts

**Causes:**
- 24-hour window not complete yet
- `MachineId` or `UserName` fields missing
- Threshold not met (< 3 unique users)

**Resolution:**
1. Verify `ConnectionFailureLogs` contains failure data
2. Check `MachineId` and `UserName` are populated
3. Wait for 24-hour window to populate after initial deployment
4. Confirm at least 3 unique users failed on same machine

---

**Issue: Incorrect Failure Categories**

**Symptoms:**
- Failure categories don't match Director
- All failures in "other_failures"

**Causes:**
- `ConnectionFailureCategories` endpoint inaccessible
- Incorrect category mapping for environment
- Static conversion doesn't match environment

**Resolution:**
1. Test `ConnectionFailureCategories` endpoint manually
2. Review dynamic category mapping in logs
3. Compare `ConnectionFailureEnumValue` to `Category` mapping
4. May need to adjust static conversion table for specific environment

---

**Issue: Multi-Session Machine Detection Issues**

**Symptoms:**
- `machines_total` (multi-session faults) = 0
- Known faulty multi-session machines not detected

**Causes:**
- `DesktopGroupId` field empty (excludes infrastructure)
- `MachineName` field empty (excludes phantoms)
- All machines have `FaultState = 1` (healthy)

**Resolution:**
1. Check `DesktopGroupId` is populated in API
2. Verify `MachineName` is not empty
3. Review both `RegistrationState` and `FaultState` fields
4. Test machine filtering logic with actual API data

---

**Issue: Windows Service Not Starting (Offline Mode)**

**Symptoms:**
- Port doesn't bind when running as Windows service
- Works in `run` mode, fails in `service` mode

**Causes:**
- Configuration file not found (wrong working directory)
- Insufficient permissions for service account
- Port already in use by another service

**Resolution:**
```powershell
# Reinstall with absolute config path
sc stop "SenHub Agent"
sc delete "SenHub Agent"
senhub-agent.exe install \
  --config-path "C:\Program Files\Senhub\Senhub Agent\agent.yaml"
sc start "SenHub Agent"

# Check logs
type "C:\ProgramData\SenHub\logs\senhubagent.log"
```

### Debug Logging

**Enable Detailed Logging:**
```bash
# Full Citrix probe debugging
./agent run --verbose --debug-modules probe.citrix

# Include cache and transformer debugging
./agent run --verbose --debug-modules probe.citrix,cache,transformer
```

**Debug Output Includes:**
- API calls and response data
- Metric calculations and filtering
- Time window calculations
- Category mappings and conversions
- Machine filtering logic
- Connection processing details

**Example Debug Output:**
```
DBG Citrix probe collecting metrics
DBG OData query: /Sessions?$filter=ConnectionState eq 5
DBG Retrieved 448 active sessions
DBG Calculating simultaneous sessions: 47 users, 95 total sessions
DBG Logon window: 2024-06-27T14:05:00Z to 2024-06-27T14:07:00Z
DBG Retrieved 12 connections in window
DBG Logon duration avg: 18.34s (12 connections)
```

**Log Analysis Tips:**
1. Check API response times (should be < 2s per endpoint)
2. Verify OData filters are correct
3. Confirm metric calculations match expectations
4. Look for API errors or authentication failures
5. Validate time window calculations

### API Testing

**Manual OData Testing (Postman):**

```http
# Test authentication and active sessions
GET https://director.domain.com/Citrix/Monitor/OData/v4/Data/Sessions?$filter=ConnectionState eq 5&$top=100
Authorization: Basic <base64(username:password)>

# Test connection failures
GET https://director.domain.com/Citrix/Monitor/OData/v4/Data/ConnectionFailureLogs?$filter=FailureDate ge 2024-06-27T13:00:00Z
Authorization: Basic <base64(username:password)>

# Test machines endpoint
GET https://director.domain.com/Citrix/Monitor/OData/v4/Data/Machines?$filter=RegistrationState eq 1
Authorization: Basic <base64(username:password)>
```

**DDC API Testing:**

```http
# Get authentication token
POST https://ddc.domain.com/cvad/manage/Sessions/$getCredentials
Content-Type: application/json
Authorization: Basic <base64(username:password)>

# Get site inventory
GET https://ddc.domain.com/cvad/manage/Machines?$filter=SiteId eq '<site_guid>'
Authorization: Bearer <token_from_auth>
```

## Configuration Examples

### Basic Citrix Configuration

```yaml
probes:
  - name: citrix
    params:
      base_url: "https://your-director.domain.com/Citrix/Monitor/OData/v4/Data"
      environment: "PROD"
      interval: 120  # 2 minutes
      auth:
        method: "ntlm"  # or "basic"
        username: "DOMAIN\\serviceaccount"
        password: "password"
      tls:
        verify_ssl: false  # for development only - use true in production
```

### Configuration with Site Filtering (via DDC)

```yaml
probes:
  - name: citrix
    params:
      base_url: "https://director.domain.com/Citrix/Monitor/OData/v4/Data"

      # DDC configuration for site-based filtering
      delivery_controller:
        url: "https://ddc-primary.domain.com"
        fallback_urls:
          - "https://ddc-backup.domain.com"
        site_filter: "PROD"  # Only monitor this site

      interval: 120
      auth:
        method: "ntlm"
        username: "DOMAIN\\serviceaccount"
        password: "password"
      tls:
        verify_ssl: true
```

**Site Filtering Mechanism:**
1. DDC interrogated for site "PROD" inventory
2. Returns list of machine DNS names (e.g., 259 machines)
3. Cache maintained for 5 minutes
4. OData queries filtered by machine DNS (client-side)
5. Requires `$expand=Machine` in Sessions queries

### Production Configuration Best Practices

```yaml
probes:
  - name: citrix
    params:
      base_url: "https://director.domain.com/Citrix/Monitor/OData/v4/Data"
      environment: "PROD"
      interval: 120  # 2 minutes recommended

      auth:
        method: "ntlm"  # Preferred for Windows environments
        username: "DOMAIN\\svc_api_sensor"
        password: "${CITRIX_API_PASSWORD}"  # Use environment variable

      tls:
        verify_ssl: true  # Always true in production
        min_tls_version: "1.2"

      # Optional: Site-based filtering
      delivery_controller:
        url: "https://ddc-01.domain.com"
        fallback_urls:
          - "https://ddc-02.domain.com"
          - "https://ddc-03.domain.com"
        site_filter: "PROD"
```

### High-Frequency Monitoring Configuration

```yaml
# For real-time monitoring (not recommended for production)
probes:
  - name: citrix
    params:
      base_url: "https://director.domain.com/Citrix/Monitor/OData/v4/Data"
      interval: 60  # 1 minute (higher API load)

      auth:
        method: "ntlm"
        username: "DOMAIN\\serviceaccount"
        password: "password"

      tls:
        verify_ssl: true
```

**Warning:** Intervals < 120s increase API load and may impact Director performance. Only use for troubleshooting or small environments.

## Related Documentation

### Official Citrix Documentation

1. **Citrix Performance Analytics - Insights**
   https://docs.citrix.com/en-us/performance-analytics/insights.html
   - Black Hole Machine definition and detection criteria
   - Analytics calculations and thresholds

2. **Citrix Monitor API Deep Dive**
   https://www.citrix.com/blogs/2022/09/07/diving-deep-into-the-monitor-api/
   - Zombie sessions (`Hidden` property)
   - Session properties and states
   - API best practices

3. **Citrix OData API Reference**
   https://developer-docs.citrix.com/projects/monitor-service-odata-api/en/latest/
   - Complete API endpoint documentation
   - Entity relationships and field definitions
   - OData query syntax

4. **Citrix Virtual Apps and Desktops Monitoring**
   https://docs.citrix.com/en-us/citrix-virtual-apps-desktops/monitor.html
   - Director interface explanations
   - Metric definitions and calculations
   - Monitoring best practices

### Internal Implementation References

- **Citrix Probe**: `/internal/agent/probes/citrix/citrixProbe.go`
- **Session States**: `/internal/agent/probes/citrix/client_interface.go`
- **Machine Fault States**: `/internal/agent/probes/citrix/client_interface.go`
- **Failure Categories**: `/internal/agent/probes/citrix/metrics_failures.go`
- **Logon Metrics**: `/internal/agent/probes/citrix/metrics_logon.go`
- **Infrastructure Metrics**: `/internal/agent/probes/citrix/metrics_infrastructure.go`
- **Analytics Metrics**: `/internal/agent/probes/citrix/metrics_sessions.go`, `/internal/agent/probes/citrix/metrics_health.go`
- **Site Inventory**: `/internal/agent/probes/citrix/site_inventory.go`
- **DDC Client**: `/internal/agent/probes/citrix/ddc_client.go`

### SenHub Agent Documentation

- **Configuration Guide**: `/docs/admin-guide/CONFIGURATION.md`
- **Probe Development**: `/docs/developer-guide/PROBE-DEVELOPMENT.md`
- **Logging Guide**: `/docs/admin-guide/LOGGING.md`
- **Troubleshooting**: `/docs/troubleshooting/README.md`

---

*Document Version: 3.0*
*Last Updated: January 2025*
*This document consolidates all Citrix metrics documentation following the standardized SenHub Agent metrics format.*
