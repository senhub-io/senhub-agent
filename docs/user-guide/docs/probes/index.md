# Probes

Probes are the building blocks of the agent: each one knows how to
talk to **one class of system** and turns its state into typed
metrics, status enums and (where relevant) log streams. You enable
the probes you need in the `probes` block of `agent-config.yaml`
and they share the same naming and tagging conventions regardless
of the output sink — PRTG, Nagios, Prometheus or OTLP.

The catalog is organised by **vendor family** rather than by
license tier so each section reads like a focused datasheet. The
free/Pro/Enterprise badge on each probe tells you which license
unlocks it.

## Systems & OS

Host-level metrics from the operating system itself — CPU usage,
memory pressure, network throughput, filesystem capacity, and the
local journald stream. These five probes ship in the **free tier**
and form the baseline for every install.

- **[CPU](cpu.md)** *(Free)* — Per-mode utilization (user / system / idle / iowait / interrupt / softirq / nice / steal), per-core breakdown, Unix load average, process count
- **[Memory](memory.md)** *(Free)* — Physical memory by state (used / free / cached / buffers), swap, Windows paging rates
- **[Network](network.md)** *(Free)* — Per-interface throughput, packet, error and discard rates (gauge, bytes per second)
- **[Logical Disk](logicaldisk.md)** *(Free)* — Filesystem capacity (total / used / free / available), inode usage; APFS firmlinks filtered on macOS
- **[Linux Logs](linux-logs.md)** *(Free, Linux only)* — Local systemd journal subscription, OTLP log push
- **[ICMP Check](icmp-check.md)** *(Free)* — Multi-target ping: reachability, packet loss, RTT statistics
- **[HTTP Check](http-check.md)** *(Free)* — HTTP(S) status, latency phases, content match, TLS certificate expiry
- **[TCP Dial](tcp-dial.md)** *(Free)* — Raw TCP connect latency to host:port
- **[DNS Latency](dns-latency.md)** *(Free)* — Resolution latency per name, per resolver

## Synthetic Monitoring

Active probes that issue traffic from the agent host to validate
network paths and web endpoints. Use them to monitor reachability,
latency and end-to-end response time of services that don't expose
their own metrics.

- **[Ping Gateway](ping-gateway.md)** *(Pro)* — Default gateway reachability and round-trip time
- **[Ping WebApp](ping-webapp.md)** *(Pro)* — Web application availability via ICMP
- **[Load WebApp](load-webapp.md)** *(Pro)* — Web page load time measurement (HTTP GET + timing breakdown)
- **[WiFi Signal Strength](wifi-signal-strength.md)** *(Pro)* — Wireless signal quality (Windows + Linux)

## Application Delivery Controllers

Network appliances that route, load-balance and accelerate
application traffic. Read configuration and live operational state
via the vendor's REST API.

- **[NetScaler](netscaler.md)** *(Pro)* — Citrix ADC / NetScaler load balancing virtual servers, services, service groups, GSLB, content switching, SSL certificates, interfaces, HA state

## Virtualization & VDI

Stacks that deliver virtual desktops, applications or shared
sessions. The probe collects user-session, machine, application
and license metrics from the platform's monitoring API.

- **[Citrix](citrix.md)** *(Pro)* — Citrix Virtual Apps and Desktops — sessions, machines per delivery group, app launches, logon duration, license usage

## Server Hardware

Out-of-band hardware monitoring through the server's BMC (Dell
iDRAC, HPE iLO, Cisco CIMC, Lenovo XCC, generic Redfish-compatible
controllers). Reports physical health independent of the operating
system.

- **[Redfish](redfish.md)** *(Pro)* — Power, thermal, fan, voltage, temperature, CPU/memory health, physical disks, logical disks, storage controllers, storage pools, network adapters

## Data Protection

Backup and replication platforms — verify that backup jobs run,
that the repository has free space, that the license is valid, that
the proxies and managed servers are reachable.

- **[Veeam](veeam.md)** *(Pro)* — Veeam Backup & Replication v12+ jobs, repositories, proxies, managed servers, license status, session bottleneck (informational)

## Databases

Relational database engines — connection state, throughput,
replication health, buffer cache, locks, storage, and engine
internals via vendor-specific views.

- **[MySQL / MariaDB](mysql.md)** *(Pro)* — MySQL 5.7+ and MariaDB 10.3+, self-hosted and managed (RDS, Aurora, Cloud SQL, Azure Flexible, Supabase). Connections, replication threads, InnoDB buffer pool, deadlocks, slow queries.
- **[PostgreSQL](postgresql.md)** *(Pro)* — PostgreSQL 12+ self-hosted and managed. Includes the SenHub differentiators: composite replication health, table bloat estimate, backup freshness via `pg_stat_archiver`, idle-in-transaction and long-running-transaction first-class channels, version-aware `pg_stat_statements`.

## Logs & Events

Open-ended ingestion paths for log streams and custom event data.

- **[Syslog](syslog.md)** *(Pro)* — Syslog server (UDP/TCP) — receives RFC3164 / RFC5424 messages and forwards them as OTLP log records
- **[Event](event.md)** *(Pro)* — HTTP receiver — accepts custom JSON events from any source script and republishes them as structured log records

## License tiers

| Tier | Probes included |
|---|---|
| **Free** | CPU, Memory, Network, Logical Disk, Linux Logs |
| **Pro** | Free + Citrix, NetScaler, Redfish, Syslog, Event, Ping Gateway, Ping WebApp, Load WebApp, WiFi Signal, Veeam, MySQL, PostgreSQL |
| **Enterprise** | All probes (wildcard) |

See [Configuration → Licensing](../configuration.md)
for activation details.
