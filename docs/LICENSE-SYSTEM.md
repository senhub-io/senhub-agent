# SenHub Agent - License System Documentation

## Overview

The SenHub Agent uses a **JWT-based license system with RSA signatures** to control access to paid probes. This document explains the complete architecture, security model, and workflows for both development and production.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        License System                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Sensor Factory (Production)          SenHub Agent              │
│  ┌─────────────────────┐             ┌──────────────────┐      │
│  │ Private Key (Vault) │             │ Public Key       │      │
│  │  (4096-bit RSA)     │             │ (Embedded)       │      │
│  └──────────┬──────────┘             └────────┬─────────┘      │
│             │                                  │                │
│             │ Sign JWT                         │ Verify JWT    │
│             ▼                                  ▼                │
│  ┌─────────────────────┐             ┌──────────────────┐      │
│  │  Generate License   │────────────▶│ Validate License │      │
│  │  (JWT Token)        │  Customer   │ (Agent Startup)  │      │
│  └─────────────────────┘  Activates  └──────────────────┘      │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## License Tiers

Source of truth for the lists below:
- Free tier — `freeTierProbes` map in `internal/agent/services/license/license.go`
- Pro tier — `paidProbes` map in `internal/agent/services/license/probe_catalog.go` + the `authorized_probes` array in customer-specific JWTs
- Enterprise tier — wildcard `"*"` in `authorized_probes`

A structural test in `internal/agent/probes/registry_invariant_test.go` enforces that every probe registered for boot must be in one of these lists. CI fails if a future probe is added to the registry without claiming a free-tier seat or a paid-catalogue entry.

Only JWT licences are supported. The previous compact-licence format (a short HMAC-signed token) was retired with the open-source flip because its shared HMAC secret could not survive a public source tree.

### Free Tier (No License Required)
Host-local observability — probes that watch the machine the agent runs on, not a remote system:

- **apache** - Apache HTTP Server monitoring via mod_status (requests, workers, connections, traffic, uptime)
- **cpu** - CPU utilization monitoring
- **memory** - Memory usage monitoring
- **logicaldisk** - Disk space and I/O monitoring
- **network** - Network interface statistics
- **linux_logs** - Local systemd journal log shipping (Linux only)
- **windows_eventlog** - Local Windows Event Log shipping (Windows only) — the host-local OS log rail counterpart to linux_logs
- **filetail** - Generic flat-file log tailing (regex/JSON/logfmt parsing, rotation-aware), cross-platform — feeds VictoriaLogs alongside linux_logs/windows_eventlog
- **otlp_receiver** - Embedded OTLP gRPC/HTTP receiver; the agent acts as an edge collector ingesting OTLP metric streams from other instrumented sources (universal collection wedge)
- **prometheus_scrape** - Pull-side twin of otlp_receiver: scrapes Prometheus /metrics endpoints (exporters, appliances) into the same pipeline (universal collection wedge)
- **exec** - Custom checks: runs operator-supplied Nagios plugins or JSON-emitting scripts on interval (custom-sensor long tail)
- **syslog** - Syslog server (UDP/TCP) receiving RFC3164/RFC5424 messages as OTLP logs (moved from Pro in 0.2.2, #298 — completes the universal log-collection set)
- **snmp_trap** - SNMP v2c/v3 trap receiver (UDP, default :162) — push counterpart of snmp_poll, emits traps as OTel logs
- **icmp_check** - Multi-target ICMP ping (RTT min/avg/max/stddev, packet loss, reachability) — free active check, the PRTG-migration wedge sensor
- **http_check** - Multi-target HTTP(S) check: status, latency phases (DNS/connect/TLS/TTFB), response size, content match, TLS certificate expiry
- **tcp_dial** - Raw TCP connect latency to host:port targets (VIPs, brokers, AD, fileservers)
- **dns_latency** - DNS resolution latency per name, optionally per explicit resolver
- **snmp_poll** - Generic SNMP polling. The deliberate exception to "remote = paid": it is the open-core wedge to replace PRTG's free SNMP polling. Deep vendor-specific SNMP (device profiles, discovery, vendor MIBs) remains paid.
- **nginx** - Nginx stub_status scraping: active connections, request throughput, and connection state (reading/writing/waiting); web-server health on the monitored host.
- **haproxy** - HAProxy monitoring via the stats CSV HTTP endpoint: sessions, throughput, and error counters per frontend/backend/server component
- **varnish** - Varnish Cache observability via varnishstat (cache hit/miss, backend connections, thread lifecycle, session counts, memory allocation); runs on the local machine.
- **phpfpm** - PHP-FPM pool monitoring via the status-page JSON endpoint (pool stats, process counts, queue depth, uptime, slow requests)
- **wildfly** - WildFly / JBoss monitoring via the HTTP Management API (JVM heap, Undertow web-container counters, JTA transactions, JDBC datasource pool metrics)
- **kafka** — Kafka broker/topic/consumer-group monitoring via Admin API (12 metrics: broker count, topic/partition metadata, current/oldest offsets, ISR replicas, consumer group lag per partition and lag_sum per topic). Mirrors the OTel Collector kafkametricsreceiver metric set.
- **rabbitmq** - RabbitMQ broker health and queue depth via the built-in HTTP Management API (queue depth, message counters, per-node resource metrics)
- **nats** - NATS Server monitoring via the HTTP management API (/varz connections/subscriptions/messages/bytes/slow-consumers, /routez cluster routes, /jsz JetStream streams/consumers/messages/bytes). No external dependencies.
- **pulsar** - Apache Pulsar broker monitoring via Admin REST API and the Prometheus /metrics endpoint (broker-level health, throughput, storage, backlog — generic message-bus observability, not a paid vendor integration)
- **activemq** - Apache ActiveMQ broker monitoring via Jolokia HTTP REST: broker-level resource usage (memory, store, temp) and per-destination (queue/topic) message throughput counters.
- **consul** - Consul agent health and cluster metrics (catalog services, serf members, raft commit time, RPC/DNS counters, health-check states, leader status). Service-mesh health monitoring is universal infrastructure on the same footing as SNMP polling.
- **zookeeper** - Apache ZooKeeper health monitoring via the mntr four-letter command (latency, connections, znodes, file descriptors, ensemble state). Generic coordination-service observability, part of the open-core universal collection wedge.
- **envoy** - Envoy proxy monitoring: scrapes the local admin interface (/stats?format=prometheus) for server health, listener connections, and per-cluster upstream metrics. Same posture as prometheus_scrape — universal collection of an open-source standard, not a paid vendor integration.
- **ceph** - Ceph cluster monitoring via the REST Management API v1 (health, OSD counts, monitor quorum, per-pool stats). Open-source storage; same universal-collection rationale as snmp_poll and prometheus_scrape.
- **jenkins** - Jenkins CI controller health over its open HTTP REST API (stdlib, no vendor SDK): job status counts, per-job last-build duration/number, node/executor counts and build-queue depth — universal collection over a standard API, same wedge as the other free active checks
- **mysql** - MySQL / MariaDB server monitoring (OTel-first, mysql.* semconv, metric parity with otelcol-contrib mysqlreceiver). The standard open-core database wedge: PRTG/Nagios estates that already monitor MySQL get a drop-in replacement at zero cost.
- **postgresql** - PostgreSQL server monitoring (OTel-first, postgresql.* semconv aligned with otelcol-contrib postgresqlreceiver). FREE open-core database probe — the universal RDBMS wedge alongside snmp_poll.
- **cassandra** - Apache Cassandra monitoring via Jolokia HTTP REST. Operators self-host Cassandra and should not need a paid licence to observe their own cluster: connections, request latency/errors (read/write), compaction, storage load, JVM heap, GC.
- **couchdb** - CouchDB node monitoring (HTTP stats, method/status breakdowns, database reads/writes, I/O bytes). Open-core database observability wedge.
- **clickhouse** - ClickHouse server monitoring via the standard /metrics Prometheus endpoint (ClickHouse 20.1+). Follows the universal-collection posture of prometheus_scrape: the /metrics protocol is an open standard, not a proprietary vendor integration.
- **elasticsearch** - Elasticsearch / OpenSearch cluster and node monitoring via the REST JSON API (cluster health, JVM, indexing, search, thread pools)
- **opensearch** - OpenSearch cluster and node metrics via the REST JSON API (same surface as Elasticsearch). Universal observability for a database that ships with its own free distribution.
- **solr** - Apache Solr monitoring via the native metrics API: JVM heap/threads, node-level request/error/cache counters, and per-core document count and index size
- **influxdb** - InfluxDB 2.x availability and performance monitoring via the standard /health, /metrics (Prometheus text), and /api/v2/buckets endpoints. No vendor API key required beyond an optional read token.
- **memcached** - Memcached server stats via the TCP text protocol (connections, items, memory, hit/miss ratio, commands, evictions, CPU)
- **hyperv** - Hyper-V VM monitoring via WMI (Windows Server only). Reports per-VM CPU, memory and state for virtual machines co-hosted with the agent. Host-local virtualization observability, same tier rationale as cpu/memory/network.
- **proxmox** - Proxmox VE cluster monitoring via REST API: nodes (CPU, memory, status), virtual machines (QEMU) and LXC containers (CPU, memory, I/O, network, status), and storage pools. Proxmox VE is open-source hypervisor infrastructure; basic observability is equivalent to the host-monitoring role and belongs in the open-core wedge.
- **chrony** - NTP synchronisation health via chronyc: time offset, frequency offset, skew, root delay/dispersion, stratum, leap status (host-local, Linux/macOS)
- **smart** - S.M.A.R.T. Disk Health monitoring via smartctl (smartmontools, operator-installed prerequisite). Reads SATA/SAS and NVMe drive health from the local machine — host-local observability on the same footing as cpu/memory/logicaldisk.
- **ipmi** - IPMI / BMC hardware sensors (temperatures, fans, voltages, power supply status) via ipmitool. Host-local: the agent reads the machine's own baseboard management controller, not a remote system. Linux only (requires the OpenIPMI kernel driver).
- **nvidia** - NVIDIA GPU monitoring via nvidia-smi (utilization, memory, temperature, power, encoder/decoder, fan speed). Host-local: the GPU is part of the machine, like cpu/memory/logicaldisk.
- **oracle** - Oracle Database monitoring via go-ora (pure Go, no OCI client). Parity with the community oracledb_exporter: up, sessions, SGA/PGA, buffer cache hit ratio, tablespace usage, wait classes, enqueue deadlocks. The generic instance health/capacity surface is free collection; deep Oracle (RAC, Data Guard, ASM, per-SQL) stays paid.
- **process** - Local process table monitoring (CPU, memory, threads, file descriptors, uptime per process). Host-local: the probe reads from the local OS, not a remote system. Equivalent of PRTG's "Top Processes" sensor at zero cost.
- **unifi** - Ubiquiti UniFi Controller monitoring (stdlib REST, cookie session auth): device inventory, per-device CPU/memory, AP client counts and satisfaction scores, WAN throughput, connected-client counts
- **winservices** - Windows Service Control Manager enumeration: running/stopped state and SCM status code per service. Host-local (queries the machine the agent runs on); the Windows counterpart of linux_logs / windows_eventlog in the free host-observability tier.
- **systemd** - Systemd unit supervision (Linux only): active/sub/load state gauges + restart counter per unit. Watches the machine the agent runs on via D-Bus.

### Pro Tier (License Required)
Specific probes authorized by entries in the customer JWT `authorized_probes` array:

- **redfish** - BMC/iDRAC/iLO hardware monitoring
- **citrix** - Citrix Virtual Apps and Desktops monitoring
- **netscaler** - Citrix NetScaler ADC monitoring (load balancers, SSL, HA)
- **veeam** - Veeam Backup & Replication monitoring
- **postgresql** - PostgreSQL server monitoring (OTel-first, postgresql.* semconv)
- **mysql** - MySQL server monitoring (OTel-first, mysql.* semconv)
- **ibmi** - IBM i / Power Systems monitoring (JT400 JDBC bridge, senhub.ibmi.* semconv) — **Linux-only** agent runtime
- **event** - Custom HTTP event ingestion
- **ping_gateway** - Gateway connectivity monitoring
- **ping_webapp** - Web application availability
- **load_webapp** - Web application performance phase timing
- **wifi_signal_strength** - WiFi signal quality

### Enterprise Tier (License Required)
- **All probes** (wildcard `"*"` authorization in the JWT — also matches any probe added in future releases without requiring a JWT reissue)

## Security Model

### RSA Signature Verification

1. **Sensor Factory** signs licenses with **private key** (4096-bit RSA)
2. **Agent** validates licenses with **public key** (embedded in binary)
3. **Tampering detection**: Any modification invalidates the JWT signature

### Key Management

**Private Key (Production)**:
- Generated ONCE in secure environment
- Stored in secure vault (HashiCorp Vault, AWS Secrets Manager, etc.)
- NEVER committed to version control
- NEVER stored on disk (except during initial generation)
- Accessible ONLY by Sensor Factory backend

**Public Key**:
- Embedded in agent binary (`internal/agent/services/license/public_key.go`)
- Distributed with every agent installation
- Used to verify JWT signatures

## JWT License Format

### Claims Structure

```json
{
  "tier": "pro|enterprise",
  "authorized_probes": ["redfish", "citrix", "netscaler"] or ["*"],
  "exp": 1794241033,  // Expiration timestamp (Unix epoch)
  "iat": 1762705033,  // Issued at timestamp (Unix epoch)
  "iss": "SenHub",    // Issuer
  "sub": "customer-id" // Subject (customer identifier)
}
```

### Example JWT Token

```
eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdXRob3JpemVkX3Byb2JlcyI6WyJyZWRmaXNoIiwiY2l0cml4Il0sImV4cCI6MTc5NDI0MTAzMywiaWF0IjoxNzYyNzA1MDMzLCJpc3MiOiJTZW5IdWIiLCJzdWIiOiJ0ZXN0LWN1c3RvbWVyLXBybyIsInRpZXIiOiJwcm8ifQ.Gr-i74OG2WmiMn8DTjf5SiUmhm-DmGcDGVs_4EDNWror5riEUYYLZZGTDume8ejJYaQaRfXDhcOQPYHMg5YL64af0EeNiq8UFTMZi09N9ohU2NHMHT6GNRx_60r7klTXuVaT752jQfTfZqDgjnlpMoQaeovXHYMLq92Bn_KSHaqiJMJa3Nm4Vm0BaP86HkQBMA6UENda8_ErRWoVj1-LlT_6oRr5S8-yG6uJFD9AGLAc4ncEijBDRheJ8b4H4iEnS390Gfgyng7dvxb3P8_F_NLIUeawsjYdnDJoDYuX-PyeyrPuDFTPFWc2xLx47j5SGEkEnc6gaR1nxdWfEqQ3lApaAcIBov322AH35PrBZQ4RXRgtJVLK18ZjuztmJWjC8zY7g0CYxvRA3nkSUfwcjiamUeg5gM9uaEk8mtlTSmTkA4MPrEi3Mk_4CgYfNr4LGLt918zFrgxyXAhzmOuycMyqsiyVVTS9jWMsIlNLH7DMyoZNqPp_EmVf3EqaZbtcKxUeC95tTIUYgcyD9neTUbCBc-EBYANQ-A-2phafvKIEgHR8Bhz5ZjYunsK0Wz4IUrWJu7Io1bxIQporUUmoX8Qj0x3ugxT4Qf2VarN5M7t5VU19NPp78K6YOGJJHXFEKXp95WtVg5wrsHEhihhdtAxNanq_X9UXhBPvQO6IxU4
```

## Production Workflow

### 1. Initial Setup (ONE-TIME)

**In Sensor Factory secure environment:**

```bash
# Generate production RSA key pair
cd /path/to/sensor-factory
go run scripts/sensor-factory-license-generator.go --generate-keys

# Output:
# ✅ senhub-license-private-key.pem (KEEP SECRET)
# ✅ senhub-license-public-key.pem (distribute with agent)
```

**Security checklist:**
- [ ] Store private key in secure vault immediately
- [ ] Update agent's `public_key.go` with public key content
- [ ] Delete private key from disk after securing in vault
- [ ] Verify vault access is restricted to Sensor Factory backend only

### 2. Generating Customer Licenses

**In Sensor Factory:**

```bash
# Pro license for specific customer
go run sensor-factory-license-generator.go --generate-license \
  --customer-id "noble-age" \
  --tier "pro" \
  --probes "redfish,citrix,netscaler" \
  --validity-days 365

# Enterprise license with all probes
go run sensor-factory-license-generator.go --generate-license \
  --customer-id "enterprise-corp" \
  --tier "enterprise" \
  --probes "*" \
  --validity-days 730

# Output: JWT token to send to customer
```

### 3. Customer Activation

**Customer receives JWT token and activates:**

```bash
# Activate license via CLI
./agent license activate eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...

# Verify activation via web dashboard
# Navigate to: http://localhost:8080/web/{agentkey}/dashboard
# Check the "License" card for status
```

### 4. License Validation

**Agent validates license on startup:**

```go
// In sensor.go
jwtValidator, err := license.GetDefaultValidator(7) // 7-day grace period
validatedLicense, err := validator.ValidateLicense(licenseToken)

if !validator.IsProbeAuthorized(validatedLicense, probeType) {
    // Probe not authorized - skip startup
}
```

## Grace Period

**7-day grace period** after license expiration:
- Allows time for license renewal
- Probes continue to function during grace period
- Warning messages logged
- After grace period: probes disabled, falls back to free tier

## Deployment Mode

The agent runs standalone (offline-only). There is no SenHub platform connection mode. All license validation is local.

### Self-Hosted (Local Configuration)

**How it works:**
- Agent runs standalone without SenHub platform
- Requires explicit JWT license token in configuration
- Local validation with embedded RSA public key
- No internet required after deployment

**Behavior:**
- No license → **Free tier** (host-local probes + snmp_poll/snmp_trap/icmp_check/http_check/tcp_dial/dns_latency/otlp_receiver/prometheus_scrape/exec/syslog/postgresql and log shippers)
- Valid license → Tier specified in JWT (Free, Pro, Enterprise)

**Configuration:**
```yaml
agent:
  authentication_key: "9bb3df79-2973-4662-8687-8da602175e0b"
  license: eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...  # JWT required

probes:
  - name: Dell iDRAC
    type: redfish  # Requires Pro/Enterprise license
    params:
      endpoint: "https://idrac.example.com"
      username: "root"
      password: "password"
```

## CLI Commands

### Activate License
```bash
./agent license activate <JWT_TOKEN>
```

Validates JWT signature, shows license details, and saves to config file.

### Show License
```bash
./agent license show
```

Displays current license information:
- Tier
- Authorized probes
- Expiration date
- Status (ACTIVE / EXPIRED / GRACE PERIOD)

**API Alternative**: Use `GET /api/{agentkey}/license/status` for programmatic access

### Remove License
```bash
./agent license remove
```

Removes license from config (falls back to free tier).

## Development Workflow

### Testing with Mock Licenses

For development and testing, use `scripts/generate-license-keys.go`:

```bash
# Generate test keys and example licenses
go run scripts/generate-license-keys.go

# Output:
# - license-private-key.pem (TEST ONLY - DO NOT USE IN PRODUCTION)
# - license-public-key.pem
# - example-license-pro.jwt
# - example-license-enterprise.jwt
# - example-license-grace-period.jwt
```

**⚠️ WARNING**: Test keys must be replaced with production keys before deployment!

### Testing License Validation

```bash
# Build agent
make build-darwin

# Test with example license
./dist/senhub-agent_darwin_arm64 license activate $(cat example-license-pro.jwt)
./dist/senhub-agent_darwin_arm64 license show
```

## Security Best Practices

### For Sensor Factory

1. **Key Generation**
   - Generate keys in isolated, secure environment
   - Use 4096-bit RSA for production
   - Never reuse keys across environments

2. **Key Storage**
   - Store private key in vault with encryption at rest
   - Implement access controls (only backend services)
   - Enable audit logging for key access
   - Rotate keys periodically (annual recommended)

3. **License Generation**
   - Validate customer identity before issuing
   - Log all license generation events
   - Include customer ID in JWT subject
   - Set appropriate expiration dates

4. **Distribution**
   - Send licenses via secure channels (encrypted email, portal download)
   - Never log or expose private key
   - Provide clear activation instructions

### For Agent

1. **Public Key**
   - Embed public key in binary (compile-time)
   - Never load from external files (prevents substitution)
   - Verify key format on initialization

2. **License Validation**
   - Validate on every agent startup
   - Check signature before trusting claims
   - Enforce grace period strictly
   - Log all validation failures

3. **Configuration**
   - Store license in config file only
   - Use appropriate file permissions (0600)
   - Don't expose license in logs or APIs

## Troubleshooting

### Invalid License Error

```
❌ Invalid license code: crypto/rsa: verification error
```

**Cause**: JWT signature doesn't match public key.

**Solutions**:
- Verify public key in `public_key.go` matches Sensor Factory's
- Ensure license was generated with matching private key
- Check for token truncation or modification

### Probe Not Authorized

```
🚫 Probe not authorized by license - skipping
```

**Cause**: Probe type not in license's `authorized_probes` list.

**Solutions**:
- Verify license tier includes the probe
- Check probe type matches exactly (case-sensitive)
- Use enterprise license with "*" for all probes

### Grace Period Ended

```
❌ License expired and grace period ended - only free tier probes available
```

**Cause**: License expired more than 7 days ago.

**Solutions**:
- Generate new license with extended expiration
- Activate new license via CLI or Web UI

## File Reference

### Agent Files

```
internal/agent/services/license/
├── license.go              # JWT validator and license logic
├── public_key.go           # Embedded RSA public key
├── mock_validator.go       # Mock validator for testing
└── license_test.go         # Unit tests

internal/agent/services/sensor/
└── sensor.go               # License validation on probe startup

cmd/agent/
└── license.go              # CLI commands (activate, show, remove)
```

### Sensor Factory Files

```
scripts/
├── sensor-factory-license-generator.go  # Production license generator
└── generate-license-keys.go             # Development key generator

docs/
└── LICENSE-SYSTEM.md                    # This document
```

## Web UI and API Integration

### License Status API Endpoint

The agent provides a REST API endpoint to retrieve current license information:

**Endpoint**: `GET /api/{agentkey}/license/status`

**Authentication**: Requires valid agent key in URL path

**Response Format**:

```json
{
  "status": "active",
  "tier": "pro",
  "expires_at": "2026-11-09T17:17:13Z",
  "days_remaining": 365,
  "authorized_probes": ["redfish", "citrix", "netscaler"],
  "free_tier_probes": ["cpu", "memory", "logicaldisk", "network"],
  "message": "License active (365 days remaining)"
}
```

**Response Fields**:

| Field | Type | Description |
|-------|------|-------------|
| `status` | string | License status: `"active"`, `"expired"`, `"grace_period"`, `"none"`, `"invalid"`, `"error"` |
| `tier` | string | License tier: `"free"`, `"pro"`, `"enterprise"` |
| `expires_at` | string | ISO 8601 expiration date (omitted if no license) |
| `days_remaining` | integer | Days until expiration or end of grace period (omitted if no license) |
| `authorized_probes` | array | List of probe types authorized by license (empty for free tier) |
| `free_tier_probes` | array | List of always-available free tier probes |
| `message` | string | Human-readable status message |

**Status Values**:

- `"active"` - License is valid and not expired
- `"grace_period"` - License expired but within 7-day grace period
- `"expired"` - License expired and grace period ended
- `"none"` - No license configured (free tier mode)
- `"invalid"` - License token exists but is invalid/tampered
- `"error"` - Error validating license

**Example Requests**:

```bash
# Get license status
curl http://localhost:8080/api/YOUR_AGENT_KEY/license/status

# Example response - Active Pro license
{
  "status": "active",
  "tier": "pro",
  "expires_at": "2026-11-09T17:17:13Z",
  "days_remaining": 365,
  "authorized_probes": ["redfish", "citrix", "netscaler"],
  "free_tier_probes": ["cpu", "memory", "logicaldisk", "network"],
  "message": "License active (365 days remaining)"
}

# Example response - No license (free tier)
{
  "status": "none",
  "tier": "free",
  "free_tier_probes": ["cpu", "memory", "logicaldisk", "network"],
  "message": "No license configured - running in free tier mode"
}

# Example response - Grace period
{
  "status": "grace_period",
  "tier": "pro",
  "expires_at": "2025-11-01T10:00:00Z",
  "days_remaining": 5,
  "authorized_probes": ["redfish", "citrix", "netscaler"],
  "free_tier_probes": ["cpu", "memory", "logicaldisk", "network"],
  "message": "License expired but in grace period (5 days remaining)"
}
```

### Web Dashboard License Display

The agent's web dashboard displays license information in a dedicated card with automatic refresh.

**Access**: Navigate to `http://localhost:8080/web/{agentkey}/dashboard`

**License Card Features**:

1. **Status Indicator** - Color-coded badge showing current status:
   - 🟢 Green (`active`) - License is valid
   - 🟡 Yellow (`grace_period`) - In grace period, renewal needed
   - 🔵 Blue (`none`) - No license, free tier mode
   - 🟡 Yellow (`expired`, `invalid`) - License issue

2. **Tier Display** - Shows license tier: Free, Pro, or Enterprise

3. **Expiration Information** - Displays:
   - Expiration date (formatted for local timezone)
   - Days remaining until expiration or grace period end
   - Only visible when a license is configured

4. **Authorized Probes Count** - Shows:
   - Number of authorized probes (e.g., "2")
   - "All" for Enterprise wildcard licenses
   - "4 (free)" for free tier only

5. **Auto-Refresh** - License status updates every 30 seconds automatically

**Visual Example**:

```
┌─────────────────────────────────┐
│ 🔐 License        ● Active      │
├─────────────────────────────────┤
│ Tier              Pro           │
│ Expires           2026-11-09    │
│ Days Remaining    365           │
│ Authorized Probes 2             │
└─────────────────────────────────┘
```

**Status Colors**:

- **Green background** (`status-running`) - Active license
- **Yellow background** (`status-warning`) - Grace period, expired, or invalid
- **Blue background** (`status-info`) - No license (free tier)

### Integration Examples

#### Monitor License Expiration

```bash
#!/bin/bash
# Check license expiration and send alert if < 30 days

AGENT_KEY="your-agent-key"
STATUS=$(curl -s http://localhost:8080/api/$AGENT_KEY/license/status)

DAYS=$(echo "$STATUS" | jq -r '.days_remaining')
LICENSE_STATUS=$(echo "$STATUS" | jq -r '.status')

if [ "$LICENSE_STATUS" = "active" ] && [ "$DAYS" -lt 30 ]; then
    echo "WARNING: License expires in $DAYS days"
    # Send notification
fi

if [ "$LICENSE_STATUS" = "grace_period" ]; then
    echo "CRITICAL: License in grace period ($DAYS days remaining)"
    # Send urgent notification
fi
```

#### PRTG Custom Sensor

Create a PRTG sensor to monitor license status:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<prtg>
  <result>
    <channel>Days Remaining</channel>
    <value>365</value>
    <unit>Count</unit>
    <LimitMinWarning>30</LimitMinWarning>
    <LimitMinError>7</LimitMinError>
  </result>
  <result>
    <channel>License Active</channel>
    <value>1</value>
    <unit>Custom</unit>
    <customunit>Status</customunit>
  </result>
  <text>License active (365 days remaining)</text>
</prtg>
```

#### Nagios Check Plugin

```bash
#!/bin/bash
# Nagios plugin to check license status

AGENT_KEY="$1"
WARN_DAYS=30
CRIT_DAYS=7

STATUS=$(curl -s http://localhost:8080/api/$AGENT_KEY/license/status)
LICENSE_STATUS=$(echo "$STATUS" | jq -r '.status')
DAYS=$(echo "$STATUS" | jq -r '.days_remaining // 0')
MESSAGE=$(echo "$STATUS" | jq -r '.message')

case "$LICENSE_STATUS" in
    "active")
        if [ "$DAYS" -lt "$CRIT_DAYS" ]; then
            echo "CRITICAL: $MESSAGE"
            exit 2
        elif [ "$DAYS" -lt "$WARN_DAYS" ]; then
            echo "WARNING: $MESSAGE"
            exit 1
        else
            echo "OK: $MESSAGE"
            exit 0
        fi
        ;;
    "grace_period")
        echo "WARNING: $MESSAGE"
        exit 1
        ;;
    "expired"|"invalid")
        echo "CRITICAL: $MESSAGE"
        exit 2
        ;;
    "none")
        echo "OK: $MESSAGE (free tier)"
        exit 0
        ;;
    *)
        echo "UNKNOWN: $MESSAGE"
        exit 3
        ;;
esac
```

### Future Enhancements (Proposed)

#### Sensor Factory REST API

```http
POST /api/v1/licenses
Content-Type: application/json
Authorization: Bearer <admin-token>

{
  "customer_id": "noble-age",
  "tier": "pro",
  "authorized_probes": ["redfish", "citrix", "netscaler"],
  "validity_days": 365
}

Response:
{
  "license_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_at": "2026-11-09T17:17:13Z",
  "customer_id": "noble-age",
  "tier": "pro"
}
```

#### Agent License Activation UI

Future web interface for license activation:

```
GET /web/{agentkey}/license
→ License management page with activation form

POST /web/{agentkey}/license/activate
Body: { "license_token": "eyJhbGci..." }
→ Activate new license via web UI
```

## Migration from Free to Paid

### Customer Journey

1. **Start with Free Tier**
   - Deploy agent without license
   - Use free probes (cpu, memory, logicaldisk, network)

2. **Purchase License**
   - Customer contacts sales
   - Sensor Factory generates license
   - Customer receives JWT token

3. **Activate License**
   ```bash
   ./agent license activate <JWT_TOKEN>
   ```

4. **Configure Paid Probes**
   - Add redfish/citrix/other probes to config
   - Agent validates and starts authorized probes

5. **License Renewal**
   - Before expiration, generate new license
   - Activate via same process
   - Zero downtime during renewal

## FAQ

**Q: Can one license be used on multiple agents?**
A: Technically yes (JWT doesn't prevent copying), but this violates license terms. Server-side enforcement can be added in future.

**Q: What happens if license expires?**
A: 7-day grace period, then fallback to free tier probes only.

**Q: Can licenses be revoked?**
A: Not currently. License validation is fully local; revocation would require network-based checks.

**Q: How to upgrade from Pro to Enterprise?**
A: Activate new Enterprise license - it replaces the existing one.

**Q: What if private key is compromised?**
A: Generate new key pair, update all agents with new public key, reissue all customer licenses.

**Q: Can agents use citrix or veeam probes without a license?**
A: No. Premium probes require an explicit JWT license with the relevant probes authorized.

## Conclusion

The SenHub Agent license system provides:
- ✅ Secure JWT-based authorization
- ✅ RSA signature verification (tamper-proof)
- ✅ Flexible tier system (free, pro, enterprise)
- ✅ Grace period for renewals
- ✅ Simple CLI activation
- ✅ Embedded validation (no internet required)
- ✅ Web dashboard for visual monitoring
- ✅ REST API for integration with monitoring systems
- ✅ Real-time status updates with auto-refresh

This system balances security, usability, and offline operation requirements while providing comprehensive monitoring and integration capabilities.
