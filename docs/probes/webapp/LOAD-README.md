# Load WebApp Probe

The Load WebApp probe monitors HTTP/HTTPS web application performance by measuring detailed timing metrics throughout the request lifecycle. It provides comprehensive insights into DNS resolution, TCP connection, TLS handshake, server response, and data transfer phases.

## Quick Start

### Basic Configuration

```yaml
probes:
  - name: load_webapp
    params:
      url: "https://www.example.com"
      timeout: 30  # Request timeout in seconds (default: 30)
```

### Multiple URL Monitoring

```yaml
probes:
  - name: production_webapp
    type: load_webapp
    params:
      url: "https://app.example.com"
      timeout: 30

  - name: staging_webapp
    type: load_webapp
    params:
      url: "https://staging.example.com"
      timeout: 45

  - name: api_endpoint
    type: load_webapp
    params:
      url: "https://api.example.com/health"
      timeout: 15
```

## Supported Protocols

- **HTTP**: Unencrypted web traffic (http://)
- **HTTPS**: TLS/SSL encrypted web traffic (https://)

## Key Metrics Summary

| Metric | Description | Use Case |
|--------|-------------|----------|
| `dnstime` | DNS resolution time (ms) | DNS server performance, caching effectiveness |
| `connecttime` | TCP connection establishment (ms) | Network latency, firewall delays |
| `tlstime` | TLS handshake duration (ms) | Certificate validation, encryption overhead |
| `ttfb` | Time to First Byte (ms) | Server processing time, backend performance |
| `total_time` | Complete request time (ms) | End-to-end performance, user experience |

For a complete metrics reference with timing breakdowns and analysis, see [LOAD-METRICS.md](./LOAD-METRICS.md).

## Configuration Parameters

| Parameter | Type | Required | Default | Range | Description |
|-----------|------|----------|---------|-------|-------------|
| `url` | string | Yes | - | - | Full HTTP/HTTPS URL to monitor |
| `timeout` | integer | No | `30` | 1-300 | Request timeout in seconds |

### URL Requirements

- **Must include protocol**: `http://` or `https://`
- **Must be valid URL**: Hostname and path properly formatted
- **Examples**:
  - ✅ `https://www.example.com`
  - ✅ `https://api.example.com/v1/status`
  - ✅ `http://internal-app.local/health`
  - ❌ `www.example.com` (missing protocol)
  - ❌ `ftp://files.example.com` (unsupported protocol)

### Example Configurations

**Fast API monitoring (short timeout):**
```yaml
probes:
  - name: api_health_check
    type: load_webapp
    params:
      url: "https://api.example.com/health"
      timeout: 5  # Quick timeout for health check
```

**Slow backend monitoring (long timeout):**
```yaml
probes:
  - name: legacy_app
    type: load_webapp
    params:
      url: "https://legacy.example.com/dashboard"
      timeout: 60  # Extended timeout for slow application
```

**CDN performance monitoring:**
```yaml
probes:
  - name: cdn_homepage
    type: load_webapp
    params:
      url: "https://www.example.com"
      timeout: 30
```

## Monitoring Tool Integration

### PRTG Network Monitor

Access Load WebApp metrics in PRTG JSON format:

```bash
# All Load WebApp metrics
curl http://localhost:8080/api/{agentkey}/prtg/metrics

# Configure PRTG HTTP Advanced Sensor:
# - URL: http://agent-host:8080/api/{agentkey}/prtg/metrics
# - Method: POST
# - Request body: {"probe": "load_webapp"}
```

**PRTG Channels Available:**
- DNS Resolution Time (ms)
- Connect Time (ms)
- TLS Handshake Time (ms)
- Time to First Byte (ms)
- Total Load Time (ms)

**PRTG Configuration Example:**
```json
{
  "prtg": {
    "result": [
      {
        "channel": "DNS Resolution Time",
        "value": 12.5,
        "unit": "TimeMilliseconds",
        "LimitMaxWarning": 100,
        "LimitMaxError": 500
      },
      {
        "channel": "Total Load Time",
        "value": 245.8,
        "unit": "TimeMilliseconds",
        "LimitMaxWarning": 3000,
        "LimitMaxError": 5000
      }
    ]
  }
}
```

### Nagios/Icinga

Access Load WebApp metrics in Nagios format:

```bash
# All Load WebApp metrics with performance data
curl http://localhost:8080/api/{agentkey}/nagios/metrics?probe=load_webapp

# Example output:
# OK - WebApp load monitoring active | dnstime=12.5ms ttfb=145.2ms total_time=245.8ms
```

**Nagios Performance Data:**
- `dnstime` - DNS resolution time with 100ms warning, 500ms critical
- `connecttime` - TCP connection time with 200ms warning, 1000ms critical
- `tlstime` - TLS handshake time with 500ms warning, 2000ms critical
- `ttfb` - Time to First Byte with 1000ms warning, 5000ms critical
- `total_time` - Total request time with 3000ms warning, 10000ms critical

### Grafana/Prometheus

Access metrics in Prometheus-compatible format:

```bash
# Prometheus format
curl http://localhost:8080/api/{agentkey}/prometheus/metrics

# Example output:
# load_webapp_dnstime{url="https://www.example.com"} 12.5
# load_webapp_connecttime{url="https://www.example.com"} 45.3
# load_webapp_tlstime{url="https://www.example.com"} 87.5
# load_webapp_ttfb{url="https://www.example.com"} 145.2
# load_webapp_total_time{url="https://www.example.com"} 245.8
```

**Grafana Dashboard Queries:**
```promql
# Total load time over time
load_webapp_total_time{url="https://www.example.com"}

# DNS resolution performance
load_webapp_dnstime{url=~".*"}

# Backend processing time (TTFB)
load_webapp_ttfb{url="https://api.example.com"}

# TLS overhead percentage
(load_webapp_tlstime / load_webapp_total_time) * 100
```

### Web Interface

View Load WebApp metrics in the built-in dashboard:

```
http://localhost:8080/web/{agentkey}/dashboard
```

Features:
- Real-time load time visualization
- Timing phase breakdown (DNS, Connect, TLS, TTFB, Transfer)
- Historical performance trends
- Multi-URL comparison

## Use Cases

### Performance Monitoring

Monitor web application performance to identify:
- **Slow DNS resolution** (DNS server issues, missing caching)
- **Network latency** (high connect times)
- **TLS overhead** (certificate chain validation delays)
- **Backend processing delays** (high TTFB)
- **Transfer bottlenecks** (large payloads, slow bandwidth)

### Bottleneck Detection

Identify performance bottlenecks by phase:

```
Total Time: 2450ms breakdown:
├─ DNS Time:      12ms (0.5%)  ← Normal
├─ Connect Time:  45ms (1.8%)  ← Normal
├─ TLS Time:     387ms (15.8%) ← HIGH (investigate certificate chain)
├─ TTFB:        1856ms (75.8%) ← CRITICAL (backend bottleneck)
└─ Transfer:     150ms (6.1%)  ← Normal
```

**Interpretation:**
- DNS/Connect time normal → Network OK
- High TLS time → Certificate validation issue
- Very high TTFB → Backend processing bottleneck
- Transfer time acceptable → Content size reasonable

### CDN Performance Analysis

Monitor Content Delivery Network effectiveness:

```yaml
probes:
  # Origin server (no CDN)
  - name: origin_server
    type: load_webapp
    params:
      url: "https://origin.example.com/page.html"

  # CDN endpoint
  - name: cdn_endpoint
    type: load_webapp
    params:
      url: "https://cdn.example.com/page.html"
```

**Compare metrics:**
- **DNS time** should be similar (both resolve quickly)
- **Connect time** should be lower for CDN (closer to users)
- **Total time** should be significantly lower for CDN
- **TTFB** should be minimal for CDN (cached content)

### API Health Monitoring

Monitor REST API endpoint performance:

```yaml
probes:
  - name: api_health
    type: load_webapp
    params:
      url: "https://api.example.com/v1/health"
      timeout: 10

  - name: api_users
    type: load_webapp
    params:
      url: "https://api.example.com/v1/users/profile"
      timeout: 15
```

Track API response times and detect degradation early.

### SSL/TLS Certificate Monitoring

Monitor certificate validation performance:

- **Normal TLS time**: 50-200ms
- **Slow TLS time**: 200-500ms (investigate certificate chain)
- **Very slow TLS time**: >500ms (OCSP stapling issues, revocation checks)

### Geographic Performance Testing

Deploy agents in different regions to compare performance:

```yaml
# US East agent
probes:
  - name: webapp_from_us_east
    type: load_webapp
    params:
      url: "https://www.example.com"

# EU West agent (separate deployment)
probes:
  - name: webapp_from_eu_west
    type: load_webapp
    params:
      url: "https://www.example.com"
```

Compare connect times and total times to optimize CDN configuration.

## Troubleshooting

### No Metrics Collected

**Check probe status:**
```bash
# View agent logs with Load WebApp probe debugging
./agent run --authentication-key YOUR_KEY --verbose --debug-modules probe.loadwebapp
```

**Verify probe is enabled:**
```bash
# Check configuration
cat agent-config.yaml | grep -A5 "name: load_webapp"
```

### DNS Resolution Failures

**Symptom:** Error: "DNS resolution failed" or high DNS times

**Causes:**
- Invalid hostname
- DNS server unreachable
- DNS timeout

**Solutions:**
1. Verify hostname resolves:
   ```bash
   nslookup www.example.com
   dig www.example.com
   ```

2. Check DNS server configuration:
   ```bash
   # Linux/macOS
   cat /etc/resolv.conf

   # Windows
   ipconfig /all
   ```

3. Test with alternative DNS:
   ```bash
   # Temporarily use Google DNS
   nslookup www.example.com 8.8.8.8
   ```

### Connection Timeouts

**Symptom:** Error: "request timed out" or connect time equals timeout

**Causes:**
- Firewall blocking connection
- Server unreachable
- Network routing issues
- Timeout too short for slow connections

**Solutions:**
1. Verify connectivity:
   ```bash
   # Test TCP connection
   telnet www.example.com 443
   nc -zv www.example.com 443

   # Test with curl
   curl -v -m 30 https://www.example.com
   ```

2. Check firewall rules:
   ```bash
   # Linux (iptables)
   sudo iptables -L -n

   # Windows
   netsh advfirewall show currentprofile
   ```

3. Increase timeout if needed:
   ```yaml
   params:
     url: "https://slow-server.example.com"
     timeout: 60  # Increase from default 30s
   ```

### SSL/TLS Certificate Errors

**Symptom:** Error: "certificate error" or "x509: certificate" errors

**Causes:**
- Expired certificate
- Self-signed certificate
- Untrusted CA
- Certificate hostname mismatch
- Certificate chain incomplete

**Solutions:**
1. Check certificate validity:
   ```bash
   # View certificate details
   openssl s_client -connect www.example.com:443 -showcerts

   # Check expiration
   echo | openssl s_client -connect www.example.com:443 2>/dev/null | openssl x509 -noout -dates
   ```

2. Verify certificate chain:
   ```bash
   # Test full certificate chain
   curl -v https://www.example.com
   ```

3. **For internal/self-signed certificates:**
   - Note: Current implementation enforces SSL verification (InsecureSkipVerify=false)
   - For production use, ensure valid certificates from trusted CA
   - For development/testing, consider using valid certificates (Let's Encrypt is free)

### High TTFB (Time to First Byte)

**Symptom:** TTFB > 1000ms consistently

**Causes:**
- Backend server overloaded
- Database query bottlenecks
- Slow application code
- Server-side caching disabled

**Solutions:**
1. Monitor backend server resources:
   - CPU usage (system probe)
   - Memory usage (system probe)
   - Disk I/O (system probe)

2. Analyze application logs for slow queries:
   ```bash
   # Check application logs
   tail -f /var/log/application.log | grep "slow"
   ```

3. Enable server-side caching:
   - Redis/Memcached for data caching
   - Varnish/Nginx for HTTP caching
   - CloudFlare/CDN for static content

4. Database optimization:
   - Add indexes for slow queries
   - Optimize query patterns
   - Enable query caching

### HTTP Status Code Errors

**Symptom:** Error: "unexpected status code: 404/500/503"

**Monitoring behavior:**
- Probe only succeeds on HTTP 2xx and 3xx status codes
- HTTP 4xx and 5xx trigger errors

**Solutions:**
1. Verify URL is correct:
   ```bash
   curl -I https://www.example.com/correct/path
   ```

2. Check server logs for errors:
   ```bash
   # Web server logs
   tail -f /var/log/nginx/error.log
   tail -f /var/log/apache2/error.log
   ```

3. Test endpoint manually:
   ```bash
   # Full request
   curl -v https://www.example.com
   ```

### Performance Degradation

**Symptom:** Metrics show increasing load times over days/weeks

**Analysis approach:**

1. **Compare timing phases:**
   ```
   Week 1 vs Week 4:
   DNS Time:     12ms →   15ms  (+25%)   ← Minor
   Connect Time: 45ms →   52ms  (+15%)   ← Minor
   TLS Time:     87ms →   95ms  (+9%)    ← Minor
   TTFB:        145ms →  458ms  (+216%)  ← MAJOR (investigate backend)
   Transfer:     56ms →   65ms  (+16%)   ← Minor
   ```

2. **Identify the bottleneck phase:**
   - DNS degradation → DNS server issues
   - Connect degradation → Network issues
   - TLS degradation → Certificate/OCSP issues
   - TTFB degradation → Backend performance (most common)
   - Transfer degradation → Bandwidth or content size increase

3. **Correlate with other metrics:**
   - Check CPU probe for server load
   - Check memory probe for memory leaks
   - Check disk probe for I/O bottlenecks
   - Check network probe for bandwidth saturation

## Performance Considerations

### Collection Overhead

The Load WebApp probe overhead:
- **Network**: Full HTTP request per collection (~KB to MB depending on response size)
- **CPU**: Minimal (HTTP client + timing tracking ~5-10ms)
- **Memory**: ~2-5 MB per active request

### Recommended Intervals

| Use Case | Interval | Reason |
|----------|----------|--------|
| Critical API monitoring | 30s | Detect issues quickly |
| Standard web monitoring | 60s | Balance accuracy and load |
| Long-term trending | 300s | Reduce network traffic |

**Important:** Frequent polling can impact target server:
- Generates real traffic to monitored URLs
- Consumes server resources
- May trigger rate limiting
- Consider using `/health` or lightweight endpoints

### Response Body Handling

The probe downloads the complete response body to accurately measure transfer time:
- **Small responses** (< 100KB): Negligible impact
- **Large responses** (> 1MB): Consider impact on agent bandwidth
- **Very large responses** (> 10MB): May want to use dedicated endpoints

**Best practice:** Monitor lightweight endpoints or specific health check URLs rather than full pages with large assets.

## Advanced Configuration

### Multi-Environment Monitoring

Monitor multiple environments with consistent configuration:

```yaml
probes:
  - name: production_app
    type: load_webapp
    params:
      url: "https://app.example.com"
      timeout: 30

  - name: staging_app
    type: load_webapp
    params:
      url: "https://staging.example.com"
      timeout: 30

  - name: development_app
    type: load_webapp
    params:
      url: "https://dev.example.com"
      timeout: 30
```

Compare performance across environments to detect configuration issues.

### API Endpoint Testing

Monitor critical API endpoints:

```yaml
probes:
  - name: auth_api
    type: load_webapp
    params:
      url: "https://api.example.com/v1/auth/health"
      timeout: 10

  - name: users_api
    type: load_webapp
    params:
      url: "https://api.example.com/v1/users/health"
      timeout: 10

  - name: payments_api
    type: load_webapp
    params:
      url: "https://api.example.com/v1/payments/health"
      timeout: 15
```

Track individual microservice performance independently.

### Integration with Other Probes

Combine Load WebApp probe with system probes for comprehensive monitoring:

```yaml
probes:
  # Application performance
  - name: webapp_frontend
    type: load_webapp
    params:
      url: "https://www.example.com"

  # Server health
  - name: cpu
    params:
      interval: 30

  - name: memory
    params:
      interval: 30

  - name: network
    params:
      interval: 60
```

Correlate application response times with server resource usage.

## Security Considerations

### TLS Configuration

Current implementation:
- **TLS verification**: Enabled (InsecureSkipVerify=false)
- **Minimum TLS version**: TLS 1.2
- **Certificate validation**: Full chain validation required
- **Connection reuse**: Disabled (DisableKeepAlives=true) for consistent measurements

### Best Practices

1. **Use HTTPS**: Always prefer HTTPS over HTTP for production monitoring
2. **Valid certificates**: Ensure monitored endpoints have valid, trusted certificates
3. **Secure URLs**: Avoid embedding sensitive data in monitored URLs
4. **Authentication**: Use dedicated health check endpoints that don't require authentication
5. **Rate limiting**: Be mindful of target server rate limits

## Authentication

The Load WebApp probe:
- **Requires no authentication** for the probe configuration itself
- **Does not support** HTTP Basic Auth, Bearer tokens, or custom headers (current implementation)
- **Monitors public endpoints** or endpoints accessible without authentication
- **For authenticated endpoints**: Consider using dedicated health check endpoints

**Future enhancement consideration:** HTTP header support for authenticated API monitoring.

## Requirements

### Network
- Outbound HTTP/HTTPS access to monitored URLs
- DNS resolution capability
- Firewall rules allowing connections to target hosts

### Agent
- HTTP strategy enabled for metric access
- Sufficient network bandwidth for full response downloads
- Proper timeout configuration for slow endpoints

## Alert Threshold Recommendations

### DNS Time Thresholds

| Level | Threshold | Description |
|-------|-----------|-------------|
| Normal | < 50ms | Healthy DNS performance |
| Warning | 50-200ms | Slow DNS, check DNS server |
| Critical | > 200ms | DNS issues, investigate immediately |

### Connect Time Thresholds

| Level | Threshold | Description |
|-------|-----------|-------------|
| Normal | < 100ms | Good network latency |
| Warning | 100-500ms | High latency, check network |
| Critical | > 500ms | Network issues or distant server |

### TLS Time Thresholds

| Level | Threshold | Description |
|-------|-----------|-------------|
| Normal | < 200ms | Normal TLS handshake |
| Warning | 200-500ms | Slow handshake, check cert chain |
| Critical | > 500ms | Certificate issues, OCSP problems |

### TTFB Thresholds

| Level | Threshold | Description |
|-------|-----------|-------------|
| Normal | < 500ms | Fast backend processing |
| Warning | 500-2000ms | Slow backend, investigate |
| Critical | > 2000ms | Backend bottleneck, urgent action |

### Total Time Thresholds

| Level | Threshold | Description |
|-------|-----------|-------------|
| Normal | < 1000ms | Excellent user experience |
| Warning | 1000-3000ms | Acceptable but monitor |
| Critical | > 3000ms | Poor user experience |

**Note:** Thresholds should be adjusted based on application requirements and user expectations.

## Support

For issues or questions:

1. **Enable debug logging:**
   ```bash
   ./agent run --authentication-key YOUR_KEY --verbose --debug-modules probe.loadwebapp
   ```

2. **Check probe health:**
   ```bash
   curl http://localhost:8080/api/{agentkey}/debug/health
   ```

3. **Test URL manually:**
   ```bash
   # Test connectivity
   curl -v -m 30 https://www.example.com

   # Measure timing with curl
   curl -w "@curl-format.txt" -o /dev/null -s https://www.example.com
   ```

   **curl-format.txt:**
   ```
   time_namelookup:  %{time_namelookup}s\n
   time_connect:     %{time_connect}s\n
   time_appconnect:  %{time_appconnect}s\n
   time_starttransfer: %{time_starttransfer}s\n
   time_total:       %{time_total}s\n
   ```

4. **Review documentation:**
   - [Complete Metrics Reference](./LOAD-METRICS.md)
   - [Troubleshooting Guide](../../troubleshooting/)
   - [Agent Configuration Guide](../../../configuration/)

5. **Report issues:**
   - Include complete URL being monitored
   - Include relevant log excerpts with debug logging enabled
   - Include configuration YAML
   - Include curl test results showing the issue
