# Load WebApp Metrics Complete Reference

This document provides a comprehensive reference for all metrics collected by the SenHub Agent Load WebApp probe, including detailed timing phase breakdowns and performance analysis guidelines.

## Table of Contents

- [Introduction](#introduction)
- [Timing Phases Overview](#timing-phases-overview)
- [Metrics Reference](#metrics-reference)
- [Metric Tags](#metric-tags)
- [Timing Breakdown Analysis](#timing-breakdown-analysis)
- [Performance Baselines](#performance-baselines)
- [Use Cases by Metric](#use-cases-by-metric)
- [Alert Configurations](#alert-configurations)
- [Troubleshooting by Phase](#troubleshooting-by-phase)

## Introduction

The Load WebApp probe measures HTTP/HTTPS request performance using Go's `httptrace` package, which provides detailed timing information for each phase of the HTTP request lifecycle. All measurements are captured in milliseconds (ms) with microsecond precision.

### Measurement Process

```
User initiates request
         │
         ├─> DNS Resolution          (dnstime)
         │   └─ Resolve hostname to IP address
         │
         ├─> TCP Connection           (connecttime)
         │   └─ Establish TCP socket connection
         │
         ├─> TLS Handshake           (tlstime)
         │   └─ Negotiate encryption (HTTPS only)
         │
         ├─> Server Processing        (ttfb)
         │   ├─ Send HTTP request
         │   ├─ Server processes request
         │   └─ First response byte received
         │
         ├─> Data Transfer           (calculated)
         │   └─ Download response body
         │
         └─> Complete                (total_time)
```

### Collection Method

- **HTTP Library**: Go `net/http` with `httptrace`
- **Request Method**: GET
- **Body Handling**: Response body fully downloaded to measure complete transfer time
- **Timeout**: Configurable (default: 30 seconds)
- **Connection Reuse**: Disabled for consistent measurements

## Timing Phases Overview

### Complete Request Timeline

```
Start                                                                        End
  │                                                                          │
  ├──────────┬─────────┬──────────┬─────────────────────┬─────────────────┤
  │          │         │          │                     │                 │
  DNS    Connect     TLS      Request     TTFB      Transfer         Complete
  12ms      45ms      87ms       0ms     145ms         56ms

  │◄────────────────────────────────────────────────────────────────────►│
                          Total Time: 245ms
```

### Phase Dependencies

```
HTTP Request:
└─ DNS Resolution (dnstime)
   └─ TCP Connection (connecttime)
      └─ Time to First Byte (ttfb)
         └─ Data Transfer (total_time - ttfb)
            └─ Total Time (total_time)

HTTPS Request:
└─ DNS Resolution (dnstime)
   └─ TCP Connection (connecttime)
      └─ TLS Handshake (tlstime)
         └─ Time to First Byte (ttfb)
            └─ Data Transfer (total_time - ttfb)
               └─ Total Time (total_time)
```

## Metrics Reference

### DNS Resolution Time

| Attribute | Value |
|-----------|-------|
| **Metric Name** | `dnstime` |
| **Display Name** | DNS Resolution Time |
| **Type** | Gauge |
| **Unit** | Milliseconds (ms) |
| **PRTG Channel** | `load_dns_time` |

**Description:**
Time taken to resolve the hostname to an IP address via DNS query.

**Measurement:**
Time between DNS query start and DNS query completion (first IP address returned).

**Tags:**
- `url`: The monitored URL (e.g., `https://www.example.com`)
- `probe_name`: `load_webapp`

**Typical Values:**

| Scenario | Value | Interpretation |
|----------|-------|----------------|
| DNS cached (local) | 0-5ms | Excellent - resolved from local cache |
| DNS cached (resolver) | 5-20ms | Good - resolved from DNS resolver cache |
| DNS query required | 20-100ms | Normal - fresh DNS query to authoritative server |
| Slow DNS server | 100-500ms | Warning - DNS server overloaded or distant |
| DNS issues | > 500ms | Critical - DNS timeout or failures |

**Example Values:**
```yaml
dnstime: 12.5  # 12.5ms - normal DNS resolution
```

**Use Cases:**
- **DNS server performance monitoring** - Track DNS resolver health
- **Cache effectiveness** - Low values indicate good caching
- **Geographic analysis** - Compare DNS times across regions
- **DNS provider comparison** - Evaluate different DNS services

**Troubleshooting High DNS Time:**
```bash
# Test DNS resolution
nslookup www.example.com
dig www.example.com

# Check DNS server response time
dig @8.8.8.8 www.example.com +stats

# Compare with alternative DNS
dig @1.1.1.1 www.example.com  # Cloudflare DNS
dig @8.8.8.8 www.example.com  # Google DNS
```

**Related Metrics:**
- `connecttime` - Often follows DNS resolution pattern
- `total_time` - DNS is first phase of total time

---

### TCP Connection Time

| Attribute | Value |
|-----------|-------|
| **Metric Name** | `connecttime` |
| **Display Name** | Connect Time |
| **Type** | Gauge |
| **Unit** | Milliseconds (ms) |
| **PRTG Channel** | `load_connect_time` |

**Description:**
Time taken to establish a TCP connection to the target server after DNS resolution.

**Measurement:**
Time between TCP connection start (after DNS) and TCP connection established (3-way handshake complete).

**Tags:**
- `url`: The monitored URL
- `probe_name`: `load_webapp`

**Typical Values:**

| Scenario | Value | Interpretation |
|----------|-------|----------------|
| Same datacenter | 1-10ms | Excellent - low latency |
| Same region | 10-50ms | Good - acceptable latency |
| Different region (same continent) | 50-150ms | Normal - geographic distance |
| Different continent | 150-300ms | Expected - long distance |
| High latency | > 300ms | Warning - network issues or very distant server |

**Example Values:**
```yaml
connecttime: 45.3  # 45.3ms - good regional latency
```

**Use Cases:**
- **Network latency monitoring** - Track connection establishment time
- **CDN effectiveness** - Compare origin vs CDN connection times
- **Geographic performance** - Measure latency from different locations
- **Firewall impact** - Detect firewall inspection delays

**Network Distance Reference:**

| Route | Expected Latency | Connect Time |
|-------|-----------------|--------------|
| Local network | < 1ms | 1-5ms |
| Same city | 1-5ms | 5-20ms |
| Same country | 5-30ms | 20-80ms |
| Same continent | 30-100ms | 80-200ms |
| Cross-continent | 100-300ms | 200-400ms |

**Troubleshooting High Connect Time:**
```bash
# Test latency
ping www.example.com

# Trace route
traceroute www.example.com  # Linux/macOS
tracert www.example.com     # Windows

# TCP connection test
time nc -zv www.example.com 443

# MTR (better traceroute)
mtr www.example.com
```

**Related Metrics:**
- `dnstime` - Must complete before connection
- `tlstime` - Follows connection establishment
- `total_time` - Connection is part of total time

---

### TLS Handshake Time

| Attribute | Value |
|-----------|-------|
| **Metric Name** | `tlstime` |
| **Display Name** | TLS Handshake Time |
| **Type** | Gauge |
| **Unit** | Milliseconds (ms) |
| **PRTG Channel** | `load_tls_time` |

**Description:**
Time taken to complete the TLS/SSL handshake and establish encrypted communication (HTTPS only).

**Measurement:**
Time between TLS handshake start (after TCP connection) and TLS handshake complete (encrypted connection ready).

**Tags:**
- `url`: The monitored URL
- `probe_name`: `load_webapp`

**Typical Values:**

| Scenario | Value | Interpretation |
|----------|-------|----------------|
| Cached session | 0ms | Excellent - TLS session resumption used |
| Modern server (TLS 1.3) | 20-100ms | Good - efficient handshake |
| Standard server (TLS 1.2) | 100-300ms | Normal - full handshake |
| Complex certificate chain | 300-800ms | Warning - long certificate validation |
| Certificate issues | > 800ms | Critical - OCSP/CRL checks failing |

**HTTP vs HTTPS:**
- **HTTP**: `tlstime` = 0 (no TLS handshake)
- **HTTPS**: `tlstime` > 0 (TLS required)

**Example Values:**
```yaml
tlstime: 87.5   # 87.5ms - good TLS 1.3 handshake
tlstime: 0      # 0ms - HTTP connection (no TLS)
```

**Use Cases:**
- **SSL/TLS performance monitoring** - Track encryption overhead
- **Certificate validation issues** - Detect OCSP/CRL problems
- **TLS version comparison** - Compare TLS 1.2 vs 1.3 performance
- **Certificate chain optimization** - Identify long validation chains

**TLS Handshake Process:**
```
Client                                   Server
  │                                        │
  ├──────► ClientHello ──────────────────►│
  │                                        │
  │◄─────── ServerHello + Certificate ────┤
  │         + ServerKeyExchange            │
  │         + ServerHelloDone              │
  │                                        │
  ├──────► ClientKeyExchange ────────────►│
  │        ChangeCipherSpec                │
  │        Finished                        │
  │                                        │
  │◄─────── ChangeCipherSpec ──────────────┤
  │         Finished                       │
  │                                        │
  └──────── Encrypted Connection ──────────┘

  TLS 1.2: ~2 round trips (~200ms with 100ms latency)
  TLS 1.3: ~1 round trip  (~100ms with 100ms latency)
```

**Troubleshooting High TLS Time:**
```bash
# Check certificate chain
openssl s_client -connect www.example.com:443 -showcerts

# Test TLS handshake speed
openssl s_time -connect www.example.com:443 -www /

# Check OCSP stapling
openssl s_client -connect www.example.com:443 -status

# Test specific TLS version
openssl s_client -connect www.example.com:443 -tls1_3
openssl s_client -connect www.example.com:443 -tls1_2

# Check certificate validity
echo | openssl s_client -connect www.example.com:443 2>/dev/null | openssl x509 -noout -dates -issuer -subject
```

**Common High TLS Time Causes:**
1. **Long certificate chain** - Multiple intermediate certificates
2. **OCSP responder slow** - Certificate revocation check delay
3. **No OCSP stapling** - Server doesn't include OCSP response
4. **Old TLS version** - TLS 1.0/1.1 slower than 1.2/1.3
5. **Certificate validation failures** - Retry attempts

**Related Metrics:**
- `connecttime` - Must complete before TLS handshake
- `ttfb` - Follows TLS establishment
- `total_time` - TLS is part of total time

---

### Time to First Byte (TTFB)

| Attribute | Value |
|-----------|-------|
| **Metric Name** | `ttfb` |
| **Display Name** | Time to First Byte |
| **Type** | Gauge |
| **Unit** | Milliseconds (ms) |
| **PRTG Channel** | `load_time_to_first_byte` |

**Description:**
Time from sending the HTTP request to receiving the first byte of the response. This represents server processing time and backend performance.

**Measurement:**
Time between HTTP request sent (after TLS/connection established) and first response byte received.

**Tags:**
- `url`: The monitored URL
- `probe_name`: `load_webapp`

**Typical Values:**

| Scenario | Value | Interpretation |
|----------|-------|----------------|
| Static cached content | 10-50ms | Excellent - served from cache |
| Dynamic content (fast) | 50-200ms | Good - efficient backend processing |
| Dynamic content (normal) | 200-1000ms | Acceptable - typical database queries |
| Complex page generation | 1000-3000ms | Warning - slow backend processing |
| Backend bottleneck | > 3000ms | Critical - severe performance issues |

**Example Values:**
```yaml
ttfb: 145.2  # 145.2ms - good dynamic content performance
```

**Use Cases:**
- **Backend performance monitoring** - Primary indicator of server processing time
- **Database bottleneck detection** - Slow queries increase TTFB
- **Cache effectiveness** - Low TTFB indicates good caching
- **Application optimization** - Track code performance improvements

**TTFB Components:**

```
TTFB includes:
├─ Network time (request transmission)      ~10-50ms
├─ Server queue time (load balancer)        ~0-100ms
├─ Application processing                   ~50-1000ms
│  ├─ Code execution
│  ├─ Database queries
│  ├─ External API calls
│  └─ Business logic
└─ Response generation (first byte)         ~1-10ms

Total TTFB = Sum of all components
```

**TTFB Analysis by Value:**

| TTFB Range | Likely Cause | Action |
|------------|--------------|--------|
| < 100ms | Cached content or very fast backend | Excellent - maintain |
| 100-500ms | Efficient dynamic generation | Good - monitor trends |
| 500-1000ms | Database queries or API calls | Acceptable - consider optimization |
| 1000-2000ms | Slow database or complex processing | Warning - investigate bottlenecks |
| 2000-5000ms | Severe backend bottleneck | Critical - immediate optimization needed |
| > 5000ms | Critical performance issue | Urgent - backend likely overloaded |

**Troubleshooting High TTFB:**

1. **Check server resources:**
   ```bash
   # CPU usage
   top
   htop

   # Memory usage
   free -h
   vmstat 1

   # Disk I/O
   iostat -x 1
   iotop
   ```

2. **Application profiling:**
   ```bash
   # Application logs
   tail -f /var/log/application.log

   # Slow query log (MySQL example)
   tail -f /var/log/mysql/slow-query.log

   # Enable application profiling
   # (Framework-specific: New Relic, Datadog, etc.)
   ```

3. **Database optimization:**
   ```sql
   -- Find slow queries (MySQL)
   SHOW PROCESSLIST;

   -- Query analysis
   EXPLAIN SELECT * FROM users WHERE email = 'test@example.com';

   -- Missing indexes
   SHOW INDEX FROM users;
   ```

4. **Web server configuration:**
   ```bash
   # Check worker processes (Nginx)
   ps aux | grep nginx

   # Check connections (Apache)
   apache2ctl status

   # Monitor backend pool (PHP-FPM)
   systemctl status php-fpm
   ```

**Common High TTFB Causes:**

| Cause | Indicators | Solution |
|-------|-----------|----------|
| Database bottleneck | Slow query logs, high DB CPU | Add indexes, optimize queries |
| Server overload | High CPU/memory usage | Scale horizontally/vertically |
| Missing caching | Consistent high TTFB | Implement Redis/Memcached |
| External API calls | TTFB matches external service | Cache API responses, async processing |
| Application code | Profiler shows slow functions | Code optimization, refactoring |
| DNS/Network issues | High DNS/connect time too | Separate network from backend issues |

**Related Metrics:**
- `dnstime`, `connecttime`, `tlstime` - Must complete before TTFB measurement
- `total_time` - TTFB is major component
- CPU/Memory probes - Correlate with server resource usage

---

### Total Load Time

| Attribute | Value |
|-----------|-------|
| **Metric Name** | `total_time` |
| **Display Name** | Total Load Time |
| **Type** | Gauge |
| **Unit** | Milliseconds (ms) |
| **PRTG Channel** | `load_total_time` |

**Description:**
Complete end-to-end time for the HTTP/HTTPS request, from DNS resolution start to complete response body download.

**Measurement:**
Time from DNS query start to response body fully downloaded.

**Calculation:**
```
total_time = dnstime + connecttime + tlstime + ttfb + transfer_time

Where:
  transfer_time = total_time - (dnstime + connecttime + tlstime + ttfb)
```

**Tags:**
- `url`: The monitored URL
- `probe_name`: `load_webapp`

**Typical Values:**

| Scenario | Value | Interpretation |
|----------|-------|----------------|
| Fast static page | 100-500ms | Excellent - great user experience |
| Normal page load | 500-1500ms | Good - acceptable performance |
| Complex page | 1500-3000ms | Acceptable - monitor for degradation |
| Slow page load | 3000-5000ms | Warning - poor user experience |
| Very slow load | > 5000ms | Critical - unacceptable performance |

**Example Values:**
```yaml
total_time: 245.8  # 245.8ms - excellent overall performance
```

**Use Cases:**
- **User experience monitoring** - Direct indicator of page load speed
- **SLA compliance** - Track against performance SLA targets
- **Overall performance trending** - Long-term performance analysis
- **Comparative analysis** - Compare different URLs/environments

**Total Time Breakdown Example:**

```
Total Time: 2,450ms

Breakdown:
├─ DNS Resolution:     12ms (  0.5%)  │██
├─ TCP Connection:     45ms (  1.8%)  │███
├─ TLS Handshake:     387ms ( 15.8%)  │████████████████
├─ Time to First Byte: 1856ms ( 75.8%)  │███████████████████████████████████████████████████████
└─ Data Transfer:     150ms (  6.1%)  │██████

Analysis:
✓ DNS/Connect normal (< 100ms combined)
⚠ TLS high (> 300ms) - investigate certificate chain
✗ TTFB critical (> 1500ms) - backend bottleneck detected
✓ Transfer acceptable (< 200ms)

Primary Issue: Backend processing (TTFB)
Secondary Issue: TLS handshake optimization needed
```

**Performance Categories:**

| Total Time | User Perception | Rating | Action |
|-----------|----------------|--------|--------|
| < 500ms | Instant | ★★★★★ Excellent | Maintain current performance |
| 500-1000ms | Fast | ★★★★☆ Good | Monitor for degradation |
| 1000-2000ms | Acceptable | ★★★☆☆ Fair | Consider optimization |
| 2000-3000ms | Slow | ★★☆☆☆ Poor | Optimize critical paths |
| > 3000ms | Very Slow | ★☆☆☆☆ Unacceptable | Immediate action required |

**Web Performance Industry Standards:**

| Standard | Threshold | Reference |
|----------|-----------|-----------|
| Google PageSpeed | < 2000ms | Acceptable mobile experience |
| AWS CloudFront | < 1000ms | Optimal CDN delivery |
| Akamai Recommendation | < 500ms | E-commerce conversion optimization |
| User Abandonment Risk | > 3000ms | 40% of users abandon page |

**Troubleshooting Total Time:**

1. **Identify the bottleneck phase:**
   ```
   Step 1: Calculate each phase percentage
   Step 2: Identify phase > 50% of total time
   Step 3: Focus optimization on that phase
   ```

2. **Phase-by-phase optimization priority:**

   | Phase % | Priority | Action |
   |---------|----------|--------|
   | > 50% | High | Immediate optimization target |
   | 25-50% | Medium | Consider optimization |
   | < 25% | Low | Acceptable contribution |

3. **Common patterns:**

   **Pattern A: Backend bottleneck**
   ```
   DNS:     1% │█
   Connect: 2% │██
   TLS:     5% │█████
   TTFB:   85% │█████████████████████████████████████████████████████████
   Transfer: 7% │███████

   Action: Optimize backend processing (database, caching, code)
   ```

   **Pattern B: Network latency**
   ```
   DNS:     5% │█████
   Connect: 40% │████████████████████████████████████████
   TLS:    35% │███████████████████████████████████
   TTFB:   15% │███████████████
   Transfer: 5% │█████

   Action: Use CDN, optimize geographic distribution
   ```

   **Pattern C: Large content transfer**
   ```
   DNS:     1% │█
   Connect: 3% │███
   TLS:     2% │██
   TTFB:    4% │████
   Transfer: 90% │██████████████████████████████████████████████████████████

   Action: Compress content, optimize images, lazy loading
   ```

**Total Time Optimization Strategies:**

| Phase | Optimization Technique | Expected Improvement |
|-------|----------------------|---------------------|
| DNS | DNS prefetching, shorter TTLs, reliable DNS | 10-50ms |
| Connect | CDN usage, connection keep-alive | 50-200ms |
| TLS | TLS 1.3, OCSP stapling, session resumption | 50-300ms |
| TTFB | Database optimization, caching, code profiling | 100-2000ms |
| Transfer | Compression, minification, lazy loading | 50-500ms |

**Related Metrics:**
- All other metrics (components of total_time)
- System probe metrics (CPU, memory, disk) - Correlate server resources
- Network probe metrics - Correlate bandwidth usage

---

## Metric Tags

All Load WebApp metrics include standard tags for filtering, grouping, and multi-instance tracking.

### Standard Tags

| Tag Name | Type | Required | Description | Example |
|----------|------|----------|-------------|---------|
| `url` | string | Yes | The monitored URL | `https://www.example.com` |
| `probe_name` | string | Yes | Probe identifier | `load_webapp` |
| `hostname` | string | Yes | Agent hostname | `monitoring-agent-01` |
| `os` | string | Yes | Operating system | `linux`, `windows`, `darwin` |
| `arch` | string | Yes | CPU architecture | `amd64`, `arm64` |

### Multi-Instance Tag Usage

The `url` tag enables monitoring multiple URLs with a single probe configuration:

**Single URL monitoring:**
```yaml
# probes.d/10-load_webapp.yaml — each file under probes.d/ is a YAML array of probes
- name: webapp_monitor
  type: load_webapp
  params:
    url: "https://www.example.com"
```

**Metrics generated:**
```
load_webapp_dnstime{url="https://www.example.com"} 12.5
load_webapp_connecttime{url="https://www.example.com"} 45.3
load_webapp_total_time{url="https://www.example.com"} 245.8
```

**Multiple URL monitoring:**
```yaml
# probes.d/10-load_webapp.yaml
- name: production_frontend
  type: load_webapp
  params:
    url: "https://app.example.com"

- name: production_api
  type: load_webapp
  params:
    url: "https://api.example.com/health"

- name: staging_frontend
  type: load_webapp
  params:
    url: "https://staging.example.com"
```

**Query examples:**

```promql
# All load_webapp metrics
load_webapp_total_time

# Specific URL
load_webapp_total_time{url="https://app.example.com"}

# All production URLs (regex)
load_webapp_total_time{url=~"https://.*\\.example\\.com.*"}

# Compare environments
load_webapp_total_time{url=~"https://(app|staging)\\.example\\.com"}
```

### Tag Filtering Examples

**PRTG Filter:**
```json
{
  "probe": "load_webapp",
  "filter": {
    "url": "https://www.example.com"
  }
}
```

**Nagios Filter:**
```bash
curl "http://localhost:8080/api/{key}/nagios/metrics?probe=load_webapp&url=https://www.example.com"
```

**Prometheus Query:**
```promql
# Average TTFB across all monitored URLs
avg(load_webapp_ttfb)

# Maximum total time
max(load_webapp_total_time)

# Group by URL
avg(load_webapp_total_time) by (url)

# Filter by agent hostname
load_webapp_total_time{hostname="agent-eu-west"}
```

## Timing Breakdown Analysis

### Calculating Transfer Time

Transfer time is not explicitly collected but can be calculated:

```
transfer_time = total_time - (dnstime + connecttime + tlstime + ttfb)
```

**Example:**
```
Given metrics:
  dnstime:     12ms
  connecttime: 45ms
  tlstime:     87ms
  ttfb:       145ms
  total_time: 345ms

Transfer time calculation:
  transfer_time = 345 - (12 + 45 + 87 + 145)
  transfer_time = 345 - 289
  transfer_time = 56ms
```

### Percentage Breakdown

Calculate each phase as percentage of total time:

```promql
# DNS percentage
(load_webapp_dnstime / load_webapp_total_time) * 100

# Connect percentage
(load_webapp_connecttime / load_webapp_total_time) * 100

# TLS percentage
(load_webapp_tlstime / load_webapp_total_time) * 100

# TTFB percentage
(load_webapp_ttfb / load_webapp_total_time) * 100

# Transfer percentage
((load_webapp_total_time - (load_webapp_dnstime + load_webapp_connecttime + load_webapp_tlstime + load_webapp_ttfb)) / load_webapp_total_time) * 100
```

### Grafana Dashboard Queries

**Stacked area chart (timing breakdown):**
```promql
# Create separate queries for each phase
Query A: load_webapp_dnstime{url="$url"}
Query B: load_webapp_connecttime{url="$url"}
Query C: load_webapp_tlstime{url="$url"}
Query D: load_webapp_ttfb{url="$url"}
Query E: load_webapp_total_time{url="$url"} - (load_webapp_dnstime{url="$url"} + load_webapp_connecttime{url="$url"} + load_webapp_tlstime{url="$url"} + load_webapp_ttfb{url="$url"})

# Legend format
{{phase}}: DNS, Connect, TLS, TTFB, Transfer
```

**Total time comparison:**
```promql
# Compare multiple URLs
load_webapp_total_time{url=~"$urls"}

# Average total time
avg(load_webapp_total_time)

# 95th percentile
quantile(0.95, load_webapp_total_time)
```

**TTFB alerting:**
```promql
# Alert if TTFB > 2000ms for 5 minutes
load_webapp_ttfb > 2000

# Alert if TTFB increased by 50% compared to 1h ago
(load_webapp_ttfb - load_webapp_ttfb offset 1h) / load_webapp_ttfb offset 1h > 0.5
```

## Performance Baselines

### Ideal Performance Profile

| Phase | Target | Excellent | Good | Acceptable | Poor |
|-------|--------|-----------|------|------------|------|
| DNS | < 10ms | < 20ms | < 50ms | < 100ms | > 100ms |
| Connect | < 50ms | < 100ms | < 200ms | < 500ms | > 500ms |
| TLS | < 100ms | < 200ms | < 300ms | < 500ms | > 500ms |
| TTFB | < 200ms | < 500ms | < 1000ms | < 2000ms | > 2000ms |
| Transfer | < 100ms | < 200ms | < 500ms | < 1000ms | > 1000ms |
| **Total** | **< 500ms** | **< 1000ms** | **< 2000ms** | **< 3000ms** | **> 3000ms** |

### Application Type Baselines

**Static Content (HTML, CSS, JS, Images):**
```
DNS:        10ms    (1%)
Connect:    50ms    (5%)
TLS:       100ms   (10%)
TTFB:       50ms    (5%)  ← Low (cached/CDN)
Transfer:  800ms   (79%)  ← High (large files)
Total:    1010ms
```

**Dynamic API (JSON responses):**
```
DNS:        15ms    (3%)
Connect:    45ms   (10%)
TLS:        85ms   (19%)
TTFB:      250ms   (57%)  ← Moderate (database query)
Transfer:   50ms   (11%)  ← Low (small JSON)
Total:     445ms
```

**Heavy Backend Application:**
```
DNS:        12ms    (0.5%)
Connect:    48ms    (2%)
TLS:        92ms    (4%)
TTFB:     2100ms   (92%)  ← High (complex processing)
Transfer:   38ms    (1.5%)
Total:    2290ms
```

**CDN-Cached Content:**
```
DNS:        18ms    (12%)
Connect:    22ms    (15%)
TLS:        65ms    (44%)
TTFB:       25ms    (17%)  ← Very low (edge cache)
Transfer:   18ms    (12%)
Total:     148ms
```

### Geographic Performance Baselines

**Same Region (< 500km):**
- Connect: 10-30ms
- Total: 200-800ms

**Same Continent (500-3000km):**
- Connect: 30-80ms
- Total: 500-1500ms

**Cross-Continent (3000-10000km):**
- Connect: 100-250ms
- Total: 1000-3000ms

**Cross-Globe (> 10000km):**
- Connect: 250-400ms
- Total: 2000-5000ms

## Use Cases by Metric

### Performance Monitoring

| Goal | Primary Metrics | Secondary Metrics | Alert Threshold |
|------|----------------|-------------------|----------------|
| Overall performance | `total_time` | All phases | > 3000ms |
| Backend health | `ttfb` | `total_time` | > 2000ms |
| Network health | `connecttime`, `dnstime` | - | > 500ms |
| TLS configuration | `tlstime` | - | > 500ms |
| User experience | `total_time` | `ttfb` | > 2000ms |

### Bottleneck Detection

| Bottleneck Type | Indicator Metric | Diagnostic Steps |
|----------------|------------------|------------------|
| DNS issues | `dnstime > 100ms` | 1. Check DNS server<br>2. Test resolution speed<br>3. Consider DNS caching |
| Network latency | `connecttime > 200ms` | 1. Ping test<br>2. Traceroute<br>3. Consider CDN |
| TLS problems | `tlstime > 500ms` | 1. Check certificate chain<br>2. OCSP stapling<br>3. TLS version |
| Backend slow | `ttfb > 1000ms` | 1. Check server CPU/memory<br>2. Database queries<br>3. Application profiling |
| Large content | `transfer_time > 1000ms` | 1. Check response size<br>2. Enable compression<br>3. Optimize images |

### SLA Monitoring

**Example SLA: 95% of requests < 2000ms**

```promql
# Calculate SLA compliance
(
  count(load_webapp_total_time{url="https://api.example.com"} < 2000)
  /
  count(load_webapp_total_time{url="https://api.example.com"})
) * 100

# Alert if SLA violated
(
  count(load_webapp_total_time < 2000) / count(load_webapp_total_time)
) < 0.95
```

### Capacity Planning

Track trends over time to predict when scaling is needed:

```promql
# Average total time trend (30-day moving average)
avg_over_time(load_webapp_total_time[30d])

# TTFB growth rate (week over week)
(
  avg_over_time(load_webapp_ttfb[7d])
  -
  avg_over_time(load_webapp_ttfb[7d] offset 7d)
)
```

## Alert Configurations

### Basic Alerts

**High Total Time Alert:**
```yaml
alert: WebAppHighLoadTime
expr: load_webapp_total_time > 3000
for: 5m
labels:
  severity: warning
annotations:
  summary: "Web application load time high"
  description: "{{ $labels.url }} load time is {{ $value }}ms (threshold: 3000ms)"
```

**Backend Performance Alert:**
```yaml
alert: WebAppHighTTFB
expr: load_webapp_ttfb > 2000
for: 5m
labels:
  severity: warning
annotations:
  summary: "High Time to First Byte detected"
  description: "{{ $labels.url }} TTFB is {{ $value }}ms (threshold: 2000ms)"
```

**DNS Performance Alert:**
```yaml
alert: WebAppSlowDNS
expr: load_webapp_dnstime > 200
for: 10m
labels:
  severity: warning
annotations:
  summary: "Slow DNS resolution detected"
  description: "{{ $labels.url }} DNS resolution taking {{ $value }}ms (threshold: 200ms)"
```

**TLS Issues Alert:**
```yaml
alert: WebAppSlowTLS
expr: load_webapp_tlstime > 500
for: 5m
labels:
  severity: warning
annotations:
  summary: "Slow TLS handshake detected"
  description: "{{ $labels.url }} TLS handshake taking {{ $value }}ms (threshold: 500ms)"
```

### Advanced Alerts

**Performance Degradation Alert:**
```yaml
alert: WebAppPerformanceDegradation
expr: |
  (
    avg_over_time(load_webapp_total_time[5m])
    -
    avg_over_time(load_webapp_total_time[5m] offset 1h)
  ) / avg_over_time(load_webapp_total_time[5m] offset 1h) > 0.5
for: 10m
labels:
  severity: warning
annotations:
  summary: "Web application performance degraded"
  description: "{{ $labels.url }} performance degraded by >50% compared to 1h ago"
```

**SLA Violation Alert:**
```yaml
alert: WebAppSLAViolation
expr: |
  (
    count_over_time((load_webapp_total_time < 2000)[1h:])
    /
    count_over_time(load_webapp_total_time[1h:])
  ) < 0.95
for: 5m
labels:
  severity: critical
annotations:
  summary: "SLA threshold violated"
  description: "{{ $labels.url }} failed to meet 95% < 2000ms SLA"
```

**Multi-Phase Failure Alert:**
```yaml
alert: WebAppMultiPhaseSlowdown
expr: |
  (
    (load_webapp_dnstime > 200) +
    (load_webapp_connecttime > 500) +
    (load_webapp_ttfb > 2000)
  ) >= 2
for: 5m
labels:
  severity: critical
annotations:
  summary: "Multiple performance issues detected"
  description: "{{ $labels.url }} experiencing slowdowns in multiple phases"
```

## Troubleshooting by Phase

### Phase 1: DNS Resolution Issues

**Symptoms:**
- `dnstime > 200ms` consistently
- Intermittent DNS failures

**Diagnostic commands:**
```bash
# Test DNS resolution
nslookup www.example.com

# Detailed DNS query
dig www.example.com +stats

# Test with different DNS servers
dig @8.8.8.8 www.example.com    # Google DNS
dig @1.1.1.1 www.example.com    # Cloudflare DNS
dig @208.67.222.222 www.example.com  # OpenDNS

# Check DNS cache
systemd-resolve --statistics    # Linux (systemd)
dscacheutil -statistics         # macOS
ipconfig /displaydns           # Windows
```

**Common fixes:**
1. Use faster DNS servers (Google, Cloudflare, OpenDNS)
2. Implement DNS caching on agent host
3. Reduce DNS TTL if stale records are an issue
4. Use DNS prefetching in application

---

### Phase 2: TCP Connection Issues

**Symptoms:**
- `connecttime > 500ms` consistently
- Intermittent connection failures

**Diagnostic commands:**
```bash
# Test connectivity
ping www.example.com

# TCP connection test
nc -zv www.example.com 443
telnet www.example.com 443

# Traceroute
traceroute www.example.com      # Linux/macOS
tracert www.example.com         # Windows

# MTR (better traceroute)
mtr --report www.example.com

# Check for packet loss
ping -c 100 www.example.com
```

**Common fixes:**
1. Use CDN to reduce geographic distance
2. Check for firewall/network filtering
3. Verify network routing is optimal
4. Consider using connection keep-alive

---

### Phase 3: TLS Handshake Issues

**Symptoms:**
- `tlstime > 500ms` consistently
- Certificate validation errors

**Diagnostic commands:**
```bash
# Full TLS handshake test
openssl s_client -connect www.example.com:443 -showcerts

# Test specific TLS versions
openssl s_client -connect www.example.com:443 -tls1_3
openssl s_client -connect www.example.com:443 -tls1_2

# Check OCSP stapling
openssl s_client -connect www.example.com:443 -status -tlsextdebug

# Certificate chain validation
openssl s_client -connect www.example.com:443 | openssl x509 -text

# Check certificate expiry
echo | openssl s_client -connect www.example.com:443 2>/dev/null | openssl x509 -noout -dates
```

**Common fixes:**
1. Enable OCSP stapling on server
2. Use TLS 1.3 instead of 1.2
3. Optimize certificate chain (minimize intermediates)
4. Implement TLS session resumption
5. Ensure certificate is not expired or revoked

---

### Phase 4: Time to First Byte Issues

**Symptoms:**
- `ttfb > 2000ms` consistently
- Backend processing delays

**Diagnostic commands:**
```bash
# Monitor server resources
top                             # CPU usage
htop                            # Better top
free -h                         # Memory usage
iostat -x 1                     # Disk I/O
vmstat 1                        # Virtual memory

# Application logs
tail -f /var/log/application.log

# Web server logs
tail -f /var/log/nginx/access.log
tail -f /var/log/nginx/error.log

# Database slow query log
tail -f /var/log/mysql/slow-query.log
tail -f /var/log/postgresql/postgresql.log

# Process monitoring
ps aux | grep application
lsof -p <PID>                   # Open files by process
```

**Common fixes:**
1. Enable caching (Redis, Memcached, Varnish)
2. Optimize database queries (add indexes)
3. Scale backend horizontally/vertically
4. Implement application-level caching
5. Use CDN for static content
6. Profile and optimize application code
7. Consider async processing for heavy operations

---

### Phase 5: Transfer Time Issues

**Symptoms:**
- `transfer_time > 1000ms` (calculated)
- Large response bodies

**Diagnostic commands:**
```bash
# Check response size
curl -w "Size: %{size_download} bytes\n" -o /dev/null -s https://www.example.com

# Check compression
curl -H "Accept-Encoding: gzip" -I https://www.example.com

# Detailed timing
curl -w "@curl-timing.txt" -o /dev/null -s https://www.example.com

# Download speed test
curl -o /dev/null https://www.example.com
```

**Common fixes:**
1. Enable gzip/brotli compression
2. Optimize images (WebP, compression)
3. Minify CSS/JavaScript
4. Implement lazy loading
5. Use CDN for asset delivery
6. Reduce response payload size
7. Split large responses (pagination)

---

## Related Documentation

- [Load WebApp Probe README](./README.md) - Configuration and quick start
- [Web Application Monitoring Guide](../../guides/web-monitoring.md) - Comprehensive web monitoring
- [Performance Optimization](../../guides/performance-optimization.md) - Optimization strategies
- [Troubleshooting Guide](../../troubleshooting/) - Common issues and solutions
- [Alert Configuration Guide](../../guides/alerting.md) - Setting up alerts and notifications
