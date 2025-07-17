# Citrix Black Hole Machine Detection

## Overview

Black Hole Machines are Citrix VDAs (Virtual Desktop Agents) that repeatedly fail user connection attempts. These machines appear available in the Citrix environment but consistently prevent users from successfully connecting, creating a "black hole" effect where connection attempts disappear without success.

## Detection Algorithm

The SenHub Agent Citrix probe identifies Black Hole Machines using the following algorithm:

1. **Connection Failure Analysis**: Queries the Citrix OData API for `ConnectionFailureLogs` over a configurable time period (default: 7 days)
2. **Machine Grouping**: Groups all connection failures by `MachineId` (or `MachineName` as fallback)
3. **Threshold Detection**: Identifies machines with failure counts meeting or exceeding the configured threshold (default: 4 failures)
4. **Metric Generation**: Creates metrics for monitoring and alerting

## Configuration

Add the following configuration to your Citrix probe settings:

```yaml
probes:
  - name: citrix
    params:
      base_url: "https://your-citrix-director/Citrix/Monitor"
      auth:
        method: ntlm
        username: "DOMAIN\\username"
        password: "password"
      black_hole_detection:
        days_back: 7          # Number of days to look back for failures (default: 7)
        failure_threshold: 4  # Minimum failures to classify as black hole (default: 4)
```

## Metrics Generated

### 1. Black Hole Machine Count
- **Name**: `black_hole_machines_count`
- **Type**: Gauge
- **Description**: Total number of machines identified as black holes
- **Tags**: 
  - `metric_type`: "failures"

### 2. Individual Machine Failures
- **Name**: `black_hole_machine_failures`
- **Type**: Gauge
- **Description**: Number of failures for each black hole machine
- **Tags**:
  - `metric_type`: "failures"
  - `machine`: Machine name or ID

## Example Output

```json
[
  {
    "name": "black_hole_machines_count",
    "value": 3,
    "timestamp": "2024-01-15T10:30:00Z",
    "tags": [
      {"key": "metric_type", "value": "failures"}
    ]
  },
  {
    "name": "black_hole_machine_failures",
    "value": 12,
    "timestamp": "2024-01-15T10:30:00Z",
    "tags": [
      {"key": "metric_type", "value": "failures"},
      {"key": "machine", "value": "VDA-001"}
    ]
  },
  {
    "name": "black_hole_machine_failures",
    "value": 8,
    "timestamp": "2024-01-15T10:30:00Z",
    "tags": [
      {"key": "metric_type", "value": "failures"},
      {"key": "machine", "value": "VDA-002"}
    ]
  }
]
```

## Monitoring Best Practices

1. **Alert Thresholds**: Set alerts when `black_hole_machines_count` > 0
2. **Regular Review**: Review machines with high failure counts weekly
3. **Remediation**: Common fixes for black hole machines:
   - Restart the VDA service
   - Re-register the VDA with the Delivery Controller
   - Check for Windows Updates or pending reboots
   - Verify network connectivity
   - Review Event Viewer for VDA-related errors

## Troubleshooting

### No Black Hole Machines Detected
- Verify the probe has access to `ConnectionFailureLogs` endpoint
- Check if the time window (`days_back`) is appropriate
- Ensure connection failures are being logged in Citrix Director

### Too Many False Positives
- Increase the `failure_threshold` value
- Reduce the `days_back` window to focus on recent issues
- Filter out known problematic machines from alerts

### Performance Considerations
- The detection runs with each probe collection interval
- Large environments may have many connection failure logs
- Consider increasing the probe interval if performance is impacted

## Integration with Monitoring Systems

### PRTG
Create a custom sensor using the HTTP API endpoint:
```
/api/{agentkey}/prtg/metrics
```

Look for the `black_hole_machines_count` channel to monitor the total count.

### Nagios/Icinga
Use the check endpoint to monitor black hole machines:
```
/api/{agentkey}/nagios/check?metric=black_hole_machines_count&warning=1&critical=3
```

## Technical Details

The implementation is based on the Citrix OData API's `ConnectionFailureLogs` entity, which provides:
- `MachineId`: Unique identifier for the machine
- `MachineName`: Human-readable machine name
- `FailureDate`: When the connection failure occurred
- `UserName`: User who experienced the failure
- `ConnectionFailureEnumValue`: Type of failure

The algorithm efficiently groups failures in-memory without requiring additional API calls, making it suitable for large-scale deployments.