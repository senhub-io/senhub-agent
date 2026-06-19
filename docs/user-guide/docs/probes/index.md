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

Host-level metrics and logs from the operating system itself — CPU
usage, memory pressure, network throughput, filesystem capacity,
and the local log streams. All in the **free tier**, the baseline
for every install.

- **[CPU](cpu.md)** *(Free)* — Per-mode utilization (user / system / idle / iowait / interrupt / softirq / nice / steal), per-core breakdown, Unix load average, process count
- **[Memory](memory.md)** *(Free)* — Physical memory by state (used / free / cached / buffers), swap, Windows paging rates
- **[Network](network.md)** *(Free)* — Per-interface throughput, packet, error and discard rates (gauge, bytes per second)
- **[Logical Disk](logicaldisk.md)** *(Free)* — Filesystem capacity (total / used / free / available), inode usage; APFS firmlinks filtered on macOS
- **[Linux Logs](linux-logs.md)** *(Free, Linux only)* — Local systemd journal subscription, OTLP log push
- **[Windows Event Log](windows-eventlog.md)** *(Free, Windows only)* — Event Log channel subscription with level/EventID/provider filtering, OTLP log push
- **[File Tail](filetail.md)** *(Free)* — Flat-file log tailing: globs, rotation-safe, multiline folding, regex/JSON/logfmt parsing
- **[Process Monitor](process.md)** *(Free)* — Per-process CPU, memory, threads, file descriptors and uptime; optional name/user filtering and top-N mode
- **[Systemd Units](systemd.md)** *(Free, Linux only)* — Active/sub/load state and restart counter per systemd unit via D-Bus
- **[Windows Services](windows-services.md)** *(Free, Windows only)* — Service running/stopped state and SCM status code via the Service Control Manager
- **[Chrony (NTP)](chrony.md)** *(Free)* — NTP synchronisation health (time offset, frequency, skew, stratum) via chronyc
- **[S.M.A.R.T. Disk Health](smart.md)** *(Free)* — SATA/SAS and NVMe drive health via smartctl (smartmontools)
- **[IPMI / BMC Sensors](ipmi.md)** *(Free, Linux only)* — Hardware temperatures, fan speeds and voltages from the BMC via ipmitool
- **[NVIDIA GPU](nvidia.md)** *(Free)* — GPU utilization, memory, temperature and power via nvidia-smi
- **[Modbus TCP](modbus.md)** *(Free)* — IT/OT convergence: poll Modbus TCP Holding Registers on PLCs, sensors and smart-building controllers

## Active Checks

Free synthetic checks issued from the agent host — the building
blocks for availability monitoring. Each probe checks a list of
targets in parallel; a failing target is a measurement (`up = 0`),
never a probe failure.

- **[ICMP Check](icmp-check.md)** *(Free)* — Multi-target ping: reachability, packet loss, RTT statistics
- **[HTTP Check](http-check.md)** *(Free)* — HTTP(S) status, latency phases, content match, TLS certificate expiry
- **[TCP Dial](tcp-dial.md)** *(Free)* — Raw TCP connect latency to host:port
- **[DNS Latency](dns-latency.md)** *(Free)* — Resolution latency per name, per resolver

## Network (SNMP)

Poll and receive from network devices — switches, routers,
firewalls, UPS, printers — over SNMP.

- **[SNMP Poll](snmp-poll.md)** *(Free)* — SNMPv2c polling: MIB-2 / IF-MIB modules, custom OID mappings, LLDP topology discovery
- **[SNMP Trap](snmp-trap.md)** *(Free)* — Trap/inform receiver (v2c + v3), operator-supplied MIB resolution, OTLP log records

## Universal Ingestion

- **[OTLP Receiver](otlp-receiver.md)** *(Free)* — The agent as edge OTLP collector: applications push OTLP metrics (gRPC or HTTP) and they flow to every configured output
- **[Prometheus Scrape](prometheus-scrape.md)** *(Free)* — Pull-side twin: scrape /metrics endpoints (node_exporter, appliance exporters) into the same pipeline
- **[Exec](exec.md)** *(Free)* — Custom checks: run any Nagios plugin or JSON-emitting script on interval, with hard timeout and overlap protection

## Synthetic Monitoring

Active probes that issue traffic from the agent host to validate
network paths and web endpoints. Use them to monitor reachability,
latency and end-to-end response time of services that don't expose
their own metrics.

- **[Ping Gateway](ping-gateway.md)** *(Pro)* — Default gateway reachability and round-trip time
- **[Ping WebApp](ping-webapp.md)** *(Pro)* — Web application availability via ICMP
- **[Load WebApp](load-webapp.md)** *(Pro)* — Web page load time measurement (HTTP GET + timing breakdown)
- **[WiFi Signal Strength](wifi-signal-strength.md)** *(Free)* — Wireless signal quality (Windows + Linux)

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

- **[MySQL / MariaDB](mysql.md)** *(Free)* — MySQL 5.7+ and MariaDB 10.3+, self-hosted and managed (RDS, Aurora, Cloud SQL, Azure Flexible, Supabase). Connections, replication threads, InnoDB buffer pool, deadlocks, slow queries.
- **[PostgreSQL](postgresql.md)** *(Free)* — PostgreSQL 12+ self-hosted and managed. Includes the SenHub differentiators: composite replication health, table bloat estimate, backup freshness via `pg_stat_archiver`, idle-in-transaction and long-running-transaction first-class channels, version-aware `pg_stat_statements`.
- **[Microsoft SQL Server](mssql.md)** *(Free)* — SQL Server health and throughput via DMVs. OTel-first, aligned with otelcol-contrib sqlserverreceiver.
- **[MongoDB](mongodb.md)** *(Free)* — MongoDB server and replica set monitoring via serverStatus + dbStats. Supports standalone, replica set and Atlas targets.
- **[Oracle Database](oracle.md)** *(Free)* — Oracle DB health, sessions, SGA/PGA, buffer cache, tablespace usage and wait classes via go-ora (no OCI client).
- **[Redis / Valkey](redis.md)** *(Free)* — Redis health and throughput via the INFO command: memory, connections, throughput, cache hit/miss, keyspace, replication, persistence.
- **[Apache Cassandra](cassandra.md)** *(Free)* — Cassandra monitoring via Jolokia: connections, latency, compaction, storage, JVM heap and GC.
- **[CouchDB](couchdb.md)** *(Free)* — CouchDB HTTP stats, method/status breakdowns, database reads/writes and I/O bytes.
- **[ClickHouse](clickhouse.md)** *(Free)* — ClickHouse server monitoring via the Prometheus /metrics endpoint.
- **[Elasticsearch](elasticsearch.md)** *(Free)* — Elasticsearch cluster health, JVM, indexing, search and thread pools via the REST API.
- **[OpenSearch](opensearch.md)** *(Free)* — OpenSearch cluster and node metrics via the REST API (same surface as Elasticsearch).
- **[Apache Solr](solr.md)** *(Free)* — Solr JVM, request/cache counters and per-core index metrics via the native metrics API.
- **[InfluxDB](influxdb.md)** *(Free)* — InfluxDB 2.x availability and performance via /health, /metrics and /api/v2/buckets.
- **[Memcached](memcached.md)** *(Free)* — Memcached stats via the TCP text protocol: connections, items, memory, hit/miss, commands, evictions.

## Web & Application Servers

- **[Apache HTTP Server](apache.md)** *(Free)* — mod_status scraping: requests, workers, connections, traffic, uptime
- **[Nginx](nginx.md)** *(Free)* — Nginx stub_status: active connections, request throughput, connection state
- **[HAProxy](haproxy.md)** *(Free)* — HAProxy sessions, throughput and error counters per frontend/backend/server via the stats CSV endpoint
- **[Envoy Proxy](envoy.md)** *(Free)* — Envoy server health, listener connections and per-cluster upstream metrics via the admin interface
- **[PHP-FPM](php-fpm.md)** *(Free)* — PHP-FPM pool monitoring via the status-page JSON endpoint
- **[Varnish Cache](varnish.md)** *(Free)* — Varnish hit/miss, backend connections, threads, sessions and memory via varnishstat
- **[Apache Tomcat](tomcat.md)** *(Free)* — Tomcat requests, sessions, JVM heap, GC and thread pool via Jolokia
- **[WildFly / JBoss](wildfly.md)** *(Free)* — WildFly JVM, Undertow, JTA transactions and JDBC pool metrics via the HTTP Management API

## Message Queues & Streaming

- **[Apache Kafka](kafka.md)** *(Free)* — Kafka broker, topic, partition and consumer-group monitoring via the Admin API
- **[RabbitMQ](rabbitmq.md)** *(Free)* — RabbitMQ broker health, queue depth and node metrics via the HTTP Management API
- **[Apache ActiveMQ](activemq.md)** *(Free)* — ActiveMQ broker resource usage and per-destination message throughput via Jolokia
- **[NATS Server](nats.md)** *(Free)* — NATS connections, subscriptions, message throughput, cluster routes and JetStream via the HTTP management API
- **[Apache Pulsar](pulsar.md)** *(Free)* — Pulsar broker health, throughput, storage and backlog via the Admin REST API and /metrics

## Container & Virtualization

- **[Docker](docker.md)** *(Free)* — Per-container CPU, memory, network I/O, block I/O and running state via the Docker Engine API
- **[Kubernetes](kubernetes.md)** *(Free)* — Kubernetes nodes, pods, containers and deployments via the API server
- **[Proxmox VE](proxmox.md)** *(Free)* — Proxmox VE cluster: nodes, QEMU VMs, LXC containers and storage pools via the REST API
- **[Hyper-V](hyperv.md)** *(Free, Windows only)* — Hyper-V VM CPU, memory and state via WMI on the local Windows Server host

## Storage

- **[Ceph](ceph.md)** *(Free)* — Ceph cluster health, OSD counts, monitor quorum and per-pool stats via the REST Management API
- **[S.M.A.R.T. Disk Health](smart.md)** *(Free)* — See Systems & OS above

## Service Discovery & CI

- **[Consul](consul.md)** *(Free)* — Consul agent health, catalog services, Serf members, Raft latency and health-check state distribution
- **[Apache ZooKeeper](zookeeper.md)** *(Free)* — ZooKeeper latency, connections, znodes, watches and file descriptors via the mntr four-letter command
- **[Jenkins CI](jenkins.md)** *(Free)* — Jenkins job status counts, per-job build duration, node counts and queue depth via the HTTP REST API

## Logs & Events

Open-ended ingestion paths for log streams and custom event data.

- **[Syslog](syslog.md)** *(Free)* — Syslog server (UDP/TCP) — receives RFC3164 / RFC5424 messages and forwards them as OTLP log records
- **[Event](event.md)** *(Pro)* — HTTP receiver — accepts custom JSON events from any source script and republishes them as structured log records

## License tiers

| Tier | Probes included |
|---|---|
| **Free** | CPU, Memory, Network, Logical Disk, Linux Logs, Windows Event Log, File Tail, Process Monitor, Systemd Units, Windows Services, Chrony, S.M.A.R.T., IPMI, NVIDIA GPU, Modbus TCP, ICMP Check, HTTP Check, TCP Dial, DNS Latency, SNMP Poll, SNMP Trap, OTLP Receiver, Prometheus Scrape, Exec, Syslog, WiFi Signal, MySQL, PostgreSQL, MSSQL, MongoDB, Oracle, Redis, Cassandra, CouchDB, ClickHouse, Elasticsearch, OpenSearch, Solr, InfluxDB, Memcached, Apache HTTP Server, Nginx, HAProxy, Envoy, PHP-FPM, Varnish, Tomcat, WildFly, Kafka, RabbitMQ, ActiveMQ, NATS, Pulsar, Docker, Kubernetes, Proxmox VE, Hyper-V, Ceph, Consul, ZooKeeper, Jenkins CI |
| **Pro** | Free + Citrix, NetScaler, Redfish, Event, Ping Gateway, Ping WebApp, Load WebApp, Veeam |
| **Enterprise** | All probes (wildcard) |

See [Configuration → Licensing](../configuration.md)
for activation details.
