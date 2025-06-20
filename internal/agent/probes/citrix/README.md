# Citrix Probe for SenHub Agent

The Citrix probe monitors Citrix Virtual Apps and Desktops environments via the Citrix Director/Monitor OData v4 API. It collects comprehensive metrics including session performance, connection failures, machine status, and infrastructure health.

## Features

- **Pre-calculated Metrics**: All metrics are calculated and cached, no raw data storage
- **Multiple Authentication**: NTLM, Basic, and Kerberos authentication support
- **Comprehensive Coverage**: Monitors sessions, machines, delivery groups, failures, and infrastructure
- **Performance Optimized**: 2-minute collection intervals with 1-hour rolling averages
- **Retry Logic**: Exponential backoff retry mechanism for reliability
- **High Availability**: Works with multiple Citrix controllers

## Configuration

### Basic Configuration

```yaml
citrix:
  base_url: "https://director.domain.com:443/Citrix/Monitor/OData/v4/Data"
  environment: "PROD"
  auth:
    method: "ntlm"
    username: "domain\\user"
    password: "password"
  tls:
    verify_ssl: false
  collection_interval: 120
  timeout: 30
  retry:
    max_attempts: 3
    backoff_factor: 2
```

### Production Configuration Example

```yaml
# Production Citrix monitoring configuration
probes:
  - name: citrix
    params:
      base_url: "https://citrix-director.company.com/Citrix/Monitor/OData/v4/Data"
      environment: "PROD"
      interval: 120  # 2 minutes
      
      # Authentication configuration
      auth:
        method: "ntlm"          # ntlm, basic, or kerberos
        username: "DOMAIN\\svc-monitoring"
        password: "${CITRIX_PASSWORD}"  # Use environment variable
      
      # TLS configuration
      tls:
        verify_ssl: true
        
      # Collection settings
      collection_interval: 120   # 2 minutes (same as interval)
      timeout: 30               # 30 seconds per request
      
      # Retry configuration
      retry:
        max_attempts: 3
        backoff_factor: 2.0
```

### Development/Testing Configuration

```yaml
# Development environment with relaxed security
probes:
  - name: citrix
    params:
      base_url: "https://citrix-dev.company.com/Citrix/Monitor/OData/v4/Data"
      environment: "DEV"
      interval: 300  # 5 minutes for dev
      
      auth:
        method: "basic"
        username: "admin"
        password: "devpassword"
      
      tls:
        verify_ssl: false  # Allow self-signed certificates in dev
      
      timeout: 60      # Longer timeout for development
      
      retry:
        max_attempts: 5
        backoff_factor: 1.5
```

## Configuration Parameters

### Required Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `base_url` | string | Citrix Director OData API endpoint URL |
| `auth.username` | string | Username for authentication |
| `auth.password` | string | Password for authentication |

### Optional Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `environment` | string | "PROD" | Environment identifier (PROD, DEV, QAL) |
| `interval` | int | 120 | Probe collection interval in seconds |
| `auth.method` | string | "ntlm" | Authentication method (ntlm, basic, kerberos) |
| `tls.verify_ssl` | bool | true | Enable SSL certificate verification |
| `collection_interval` | int | 120 | Internal collection interval in seconds |
| `timeout` | int | 30 | Request timeout in seconds |
| `retry.max_attempts` | int | 3 | Maximum retry attempts |
| `retry.backoff_factor` | float | 2.0 | Exponential backoff multiplier |

## Authentication Methods

### NTLM Authentication (Recommended)

```yaml
auth:
  method: "ntlm"
  username: "DOMAIN\\serviceaccount"
  password: "password"
```

NTLM authentication is the most common method for Citrix environments. It supports both NTLMv1 and NTLMv2.

### Basic Authentication

```yaml
auth:
  method: "basic"
  username: "admin"
  password: "password"
```

Basic authentication sends credentials in Base64 encoding. Only use over HTTPS.

### Kerberos Authentication

```yaml
auth:
  method: "kerberos"
  username: "serviceaccount@DOMAIN.COM"
  password: "password"
```

**Note**: Kerberos authentication is planned for future implementation.

## Collected Metrics

The Citrix probe collects the following pre-calculated metrics:

### 1. Connection Failures Metrics

- `connection_failures_total`: Total connection failures in the last 2 minutes
- `connection_failures_by_type`: Failures categorized by type
- `connection_failures_by_delivery_group`: Failures per delivery group

**Tags**: `environment`, `metric_type`, `failure_type`, `delivery_group_id`, `delivery_group_name`

### 2. Logon Performance Metrics

- `logon_count_current`: Number of logons in the last 2 minutes
- `logon_duration_average_ms`: Average logon duration (current period)
- `logon_duration_min_ms`: Minimum logon duration
- `logon_duration_max_ms`: Maximum logon duration
- `logon_duration_median_ms`: Median logon duration
- `logon_duration_p95_ms`: 95th percentile logon duration
- `logon_duration_1h_average_ms`: 1-hour rolling average

**Tags**: `environment`, `metric_type`, `statistic`, `period`, `delivery_group_id`, `delivery_group_name`

### 3. Session Metrics

- `total_sessions`: Total active sessions
- `sessions_by_state`: Sessions by state (connected, disconnected, active)
- `sessions_by_delivery_group_total`: Total sessions per delivery group
- `sessions_by_delivery_group_connected`: Connected sessions per delivery group

**Tags**: `environment`, `metric_type`, `session_state`, `delivery_group_id`, `delivery_group_name`

### 4. Machine Metrics

- `total_machines`: Total machines in the environment
- `machines_by_registration_state`: Machines by registration state
- `machines_by_fault_state`: Machines by health state
- `machines_by_delivery_group`: Machine counts per delivery group
- `machines_by_controller`: Machine counts per controller

**Tags**: `environment`, `metric_type`, `registration_state`, `fault_state`, `delivery_group_id`, `delivery_group_name`, `controller_dns`

### 5. Infrastructure Metrics

- `controller_online_status`: Controller availability (1=online, 0=offline)
- `controller_database_status`: Database connection status per controller
- `controller_machines_registered`: Number of machines registered to controller

**Tags**: `environment`, `metric_type`, `controller_dns`, `database_type`, `connection_status`

## Error Handling

The probe implements comprehensive error handling:

- **Timeouts**: Configurable request timeouts with retry logic
- **Authentication Errors**: Logged and alerted, no retry to avoid account lockout
- **Network Errors**: Automatic retry with exponential backoff
- **Partial Data**: Continues processing with available data
- **Invalid Certificates**: Optional SSL verification bypass

## Performance Considerations

- **Collection Frequency**: 2-minute intervals balance data freshness with system load
- **Parallel Queries**: Multiple API endpoints queried concurrently using goroutines
- **Memory Efficient**: Processes data in streams, minimal memory footprint
- **Cache Friendly**: Pre-calculated metrics stored in shared HTTP cache with appropriate TTL

## Monitoring the Probe

The probe provides self-monitoring metrics:

```yaml
# Probe health metrics (automatically generated)
- citrix_probe_collection_duration_ms
- citrix_probe_collection_errors_total
- citrix_probe_last_collection_timestamp
- citrix_probe_cache_entries_total
```

## Troubleshooting

### Common Issues

**Authentication Failures**
```
Error: HTTP 401: Unauthorized
```
- Verify username/password are correct
- Check domain format: use "DOMAIN\\user" for NTLM
- Ensure service account has read permissions to Citrix Monitor data

**Connection Timeouts**
```
Error: context deadline exceeded
```
- Increase `timeout` parameter
- Check network connectivity to Citrix Director
- Verify base_url is accessible

**SSL Certificate Errors**
```
Error: x509: certificate signed by unknown authority
```
- Set `tls.verify_ssl: false` for self-signed certificates
- Or install proper CA certificates

### Debug Mode

Enable debug logging for detailed troubleshooting:

```bash
./agent run --authentication-key YOUR_KEY --debug-modules probe.citrix
```

### Validation

Test your configuration:

```bash
# Test connectivity
curl -k -u "DOMAIN\\user:password" "https://director.domain.com/Citrix/Monitor/OData/v4/Data/Sessions"

# Validate OData endpoint
curl -k "https://director.domain.com/Citrix/Monitor/OData/v4/Data/\$metadata"
```

## Integration Examples

### PRTG Network Monitor

The probe automatically provides PRTG-compatible JSON format:

```bash
curl "http://agent:8080/api/{agentkey}/prtg/metrics"
```

### Grafana Dashboards

Use the HTTP strategy to expose metrics for Grafana:

```bash
curl "http://agent:8080/api/{agentkey}/senhub/metrics"
```

### Custom Monitoring

Access raw metrics via the probe's data points:

```go
// In your monitoring application
dataPoints, err := probe.Collect()
for _, dp := range dataPoints {
    fmt.Printf("Metric: %s, Value: %f, Tags: %v\n", 
        dp.Name, dp.Value, dp.Tags)
}
```

## Security Best Practices

1. **Service Account**: Use dedicated service account with minimal permissions
2. **Password Security**: Store passwords in environment variables or secure vaults
3. **SSL/TLS**: Always use HTTPS in production environments
4. **Network Security**: Restrict network access to Citrix Director
5. **Monitoring**: Monitor authentication failures and probe errors

## Version Compatibility

- **Citrix Virtual Apps and Desktops**: 7.x and later
- **Citrix Cloud**: Supported
- **OData API**: v4 (required)
- **Authentication**: NTLM v1/v2, Basic Auth
- **TLS**: 1.2 and 1.3

For the latest compatibility information, consult the Citrix documentation.