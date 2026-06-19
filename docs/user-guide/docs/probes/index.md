---
hide:
  - toc
---

# Probes

Each probe targets one class of system and turns its state into typed metrics and log streams. Enable the probes you need — they share the same naming and tagging conventions across every output sink.

<div class="probe-catalog">

<div class="catalog-controls">
  <input type="search" id="probe-search" placeholder="Search probes…" autocomplete="off">
  <div class="catalog-families" id="catalog-families">
    <button class="family-btn active" data-family="all">All</button>
    <button class="family-btn" data-family="os-host">OS &amp; Host</button>
    <button class="family-btn" data-family="network">Network</button>
    <button class="family-btn" data-family="databases">Databases</button>
    <button class="family-btn" data-family="web-servers">Web Servers</button>
    <button class="family-btn" data-family="messaging">Messaging</button>
    <button class="family-btn" data-family="containers">Containers &amp; VMs</button>
    <button class="family-btn" data-family="collection">Collection</button>
    <button class="family-btn" data-family="devops">DevOps</button>
    <button class="family-btn" data-family="storage">Storage</button>
    <button class="family-btn" data-family="hardware">Hardware</button>
    <button class="family-btn" data-family="adc">ADC</button>
  </div>
  <div class="catalog-tiers">
    <button class="tier-toggle active" data-tier="all">All tiers</button>
    <button class="tier-toggle" data-tier="free">Free</button>
    <button class="tier-toggle" data-tier="pro">Pro</button>
  </div>
</div>

<div class="probe-grid" id="probe-grid">

  <a href="cpu/" class="probe-card" data-family="os-host" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/cpu-64-bit.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">CPU</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Per-mode utilization, per-core breakdown, load average</span>
  </a>

  <a href="memory/" class="probe-card" data-family="os-host" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/memory.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">Memory</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Physical memory, swap, Windows paging rates</span>
  </a>

  <a href="network/" class="probe-card" data-family="os-host" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/ethernet.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">Network</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Per-interface throughput, packet and error rates</span>
  </a>

  <a href="logicaldisk/" class="probe-card" data-family="os-host" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/harddisk.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">Logical Disk</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Filesystem capacity, inode usage</span>
  </a>

  <a href="linux-logs/" class="probe-card" data-family="os-host" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/linux" alt="" loading="lazy">
    <span class="probe-name">Linux Logs</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Systemd journal subscription → OTLP logs</span>
  </a>

  <a href="windows-eventlog/" class="probe-card" data-family="os-host" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/text-box-search-outline.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">Windows Event Log</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Event Log channels with level/EventID filtering</span>
  </a>

  <a href="process/" class="probe-card" data-family="os-host" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/cog-outline.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">Process Monitor</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Per-process CPU, memory, threads, file descriptors</span>
  </a>

  <a href="systemd/" class="probe-card" data-family="os-host" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/linux" alt="" loading="lazy">
    <span class="probe-name">Systemd Units</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Active/sub/load state and restart counter per unit</span>
  </a>

  <a href="windows-services/" class="probe-card" data-family="os-host" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/cog-play-outline.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">Windows Services</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Service running/stopped state via SCM</span>
  </a>

  <a href="chrony/" class="probe-card" data-family="os-host" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/clock-outline.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">Chrony (NTP)</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">NTP sync health: offset, frequency, skew, stratum</span>
  </a>

  <a href="smart/" class="probe-card" data-family="os-host" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/harddisk.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">S.M.A.R.T. Disk Health</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">SATA/SAS and NVMe drive health via smartctl</span>
  </a>

  <a href="ipmi/" class="probe-card" data-family="os-host" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/server.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">IPMI / BMC Sensors</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Temperatures, fan speeds, voltages from BMC</span>
  </a>

  <a href="nvidia/" class="probe-card" data-family="os-host" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/nvidia" alt="" loading="lazy">
    <span class="probe-name">NVIDIA GPU</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">GPU utilization, memory, temperature, power</span>
  </a>

  <a href="wifi-signal-strength/" class="probe-card" data-family="os-host" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/wifi.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">WiFi Signal</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Wireless signal quality on Windows + Linux</span>
  </a>

  <a href="snmp-poll/" class="probe-card" data-family="network" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/lan.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">SNMP Poll</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">SNMPv2c polling: MIB-2, IF-MIB, LLDP topology</span>
  </a>

  <a href="snmp-trap/" class="probe-card" data-family="network" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/lan-connect.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">SNMP Trap</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Trap/inform receiver (v2c + v3) → OTLP logs</span>
  </a>

  <a href="icmp-check/" class="probe-card" data-family="network" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/check-network.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">ICMP Check</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Multi-target ping: reachability, RTT, packet loss</span>
  </a>

  <a href="http-check/" class="probe-card" data-family="network" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/web.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">HTTP Check</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">HTTP(S) status, latency phases, TLS cert expiry</span>
  </a>

  <a href="tcp-dial/" class="probe-card" data-family="network" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/connection.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">TCP Dial</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Raw TCP connect latency to host:port</span>
  </a>

  <a href="dns-latency/" class="probe-card" data-family="network" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/dns.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">DNS Latency</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Resolution latency per name, per resolver</span>
  </a>

  <a href="modbus/" class="probe-card" data-family="network" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/chip.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">Modbus TCP</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Poll Modbus TCP Holding Registers on PLCs/sensors</span>
  </a>

  <a href="unifi/" class="probe-card" data-family="network" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/ubiquiti" alt="" loading="lazy">
    <span class="probe-name">UniFi</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Ubiquiti controller: devices, APs, client counts, WAN</span>
  </a>

  <a href="ping-gateway/" class="probe-card" data-family="network" data-tier="pro">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/router.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">Ping Gateway</span>
    <span class="probe-tier-badge pro">Pro</span>
    <span class="probe-desc">Default gateway reachability and RTT</span>
  </a>

  <a href="ping-webapp/" class="probe-card" data-family="network" data-tier="pro">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/web.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">Ping WebApp</span>
    <span class="probe-tier-badge pro">Pro</span>
    <span class="probe-desc">Web application availability via ICMP</span>
  </a>

  <a href="mysql/" class="probe-card" data-family="databases" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/mysql" alt="" loading="lazy">
    <span class="probe-name">MySQL / MariaDB</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Connections, InnoDB, replication, slow queries</span>
  </a>

  <a href="postgresql/" class="probe-card" data-family="databases" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/postgresql" alt="" loading="lazy">
    <span class="probe-name">PostgreSQL</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Connections, replication, bloat, archiver, pg_stat_statements</span>
  </a>

  <a href="mssql/" class="probe-card" data-family="databases" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/microsoftsqlserver" alt="" loading="lazy">
    <span class="probe-name">SQL Server</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Health and throughput via DMVs, OTel-aligned</span>
  </a>

  <a href="mongodb/" class="probe-card" data-family="databases" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/mongodb" alt="" loading="lazy">
    <span class="probe-name">MongoDB</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">serverStatus, dbStats, replica set monitoring</span>
  </a>

  <a href="oracle/" class="probe-card" data-family="databases" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/oracle" alt="" loading="lazy">
    <span class="probe-name">Oracle Database</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Sessions, SGA/PGA, buffer cache, tablespace, wait classes</span>
  </a>

  <a href="redis/" class="probe-card" data-family="databases" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/redis" alt="" loading="lazy">
    <span class="probe-name">Redis</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Keyspace, memory, persistence, replication, commands</span>
  </a>

  <a href="cassandra/" class="probe-card" data-family="databases" data-tier="free">
    <img class="probe-logo probe-logo-wm" src="https://upload.wikimedia.org/wikipedia/commons/5/5e/Cassandra_logo.svg" alt="" loading="lazy">
    <span class="probe-name">Cassandra</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Node health, request metrics, compaction, GC</span>
  </a>

  <a href="couchdb/" class="probe-card" data-family="databases" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/couchdb" alt="" loading="lazy">
    <span class="probe-name">CouchDB</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Request rate, open databases, replication state</span>
  </a>

  <a href="clickhouse/" class="probe-card" data-family="databases" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/clickhouse" alt="" loading="lazy">
    <span class="probe-name">ClickHouse</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Via /metrics Prometheus endpoint</span>
  </a>

  <a href="elasticsearch/" class="probe-card" data-family="databases" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/elasticsearch" alt="" loading="lazy">
    <span class="probe-name">Elasticsearch</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Cluster health, JVM, indexing, search, thread pools</span>
  </a>

  <a href="opensearch/" class="probe-card" data-family="databases" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/opensearch" alt="" loading="lazy">
    <span class="probe-name">OpenSearch</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Cluster and node metrics via REST API</span>
  </a>

  <a href="solr/" class="probe-card" data-family="databases" data-tier="free">
    <img class="probe-logo probe-logo-wm" src="https://upload.wikimedia.org/wikipedia/commons/d/d5/Apache_Solr_logo.svg" alt="" loading="lazy">
    <span class="probe-name">Apache Solr</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">JVM, request/error counters, core document count</span>
  </a>

  <a href="influxdb/" class="probe-card" data-family="databases" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/influxdb" alt="" loading="lazy">
    <span class="probe-name">InfluxDB</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Health, /metrics endpoint and bucket API</span>
  </a>

  <a href="memcached/" class="probe-card" data-family="databases" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/memcached" alt="" loading="lazy">
    <span class="probe-name">Memcached</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Connections, items, memory, hit/miss, evictions</span>
  </a>

  <a href="apache/" class="probe-card" data-family="web-servers" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/apache" alt="" loading="lazy">
    <span class="probe-name">Apache HTTP</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">mod_status: workers, connections, requests, traffic</span>
  </a>

  <a href="nginx/" class="probe-card" data-family="web-servers" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/nginx" alt="" loading="lazy">
    <span class="probe-name">Nginx</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Connections, requests via stub_status module</span>
  </a>

  <a href="haproxy/" class="probe-card" data-family="web-servers" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/haproxy" alt="" loading="lazy">
    <span class="probe-name">HAProxy</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Sessions, throughput, error rates per frontend/backend</span>
  </a>

  <a href="envoy/" class="probe-card" data-family="web-servers" data-tier="free">
    <img class="probe-logo probe-logo-wm" src="https://cdn.jsdelivr.net/gh/cncf/artwork@main/projects/envoy/icon/color/envoy-icon-color.svg" alt="" loading="lazy">
    <span class="probe-name">Envoy Proxy</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Server health, listener connections, cluster upstream</span>
  </a>

  <a href="varnish/" class="probe-card" data-family="web-servers" data-tier="free">
    <img class="probe-logo probe-logo-wm" src="https://cdn2.hubspot.net/hubfs/209523/Logos/Varnish%20Cache%20logos/Varnish-Cache_2.0_POS_Small.png" alt="" loading="lazy">
    <span class="probe-name">Varnish Cache</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Hit/miss, backend connections, threads, memory</span>
  </a>

  <a href="php-fpm/" class="probe-card" data-family="web-servers" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/php" alt="" loading="lazy">
    <span class="probe-name">PHP-FPM</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Pool stats, process counts, queue depth, slow requests</span>
  </a>

  <a href="tomcat/" class="probe-card" data-family="web-servers" data-tier="free">
    <img class="probe-logo probe-logo-wm" src="https://upload.wikimedia.org/wikipedia/commons/f/fe/Apache_Tomcat_logo.svg" alt="" loading="lazy">
    <span class="probe-name">Apache Tomcat</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Threads, requests, sessions, JVM heap via Manager</span>
  </a>

  <a href="wildfly/" class="probe-card" data-family="web-servers" data-tier="free">
    <img class="probe-logo probe-logo-wm" src="https://upload.wikimedia.org/wikipedia/commons/a/a8/JBoss_logo.svg" alt="" loading="lazy">
    <span class="probe-name">WildFly / JBoss</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">JVM, Undertow, JTA transactions, JDBC pool</span>
  </a>

  <a href="kafka/" class="probe-card" data-family="messaging" data-tier="free">
    <img class="probe-logo probe-logo-wm" src="https://upload.wikimedia.org/wikipedia/commons/0/05/Apache_Kafka_logo.svg" alt="" loading="lazy">
    <span class="probe-name">Apache Kafka</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Brokers, topics, partitions, consumer group lag</span>
  </a>

  <a href="rabbitmq/" class="probe-card" data-family="messaging" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/rabbitmq" alt="" loading="lazy">
    <span class="probe-name">RabbitMQ</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Queue depth, message counters, per-node resources</span>
  </a>

  <a href="activemq/" class="probe-card" data-family="messaging" data-tier="free">
    <img class="probe-logo probe-logo-wm" src="https://upload.wikimedia.org/wikipedia/commons/a/a7/Apache_ActiveMQ_Logo.svg" alt="" loading="lazy">
    <span class="probe-name">ActiveMQ</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Broker resources, queue/topic throughput via Jolokia</span>
  </a>

  <a href="nats/" class="probe-card" data-family="messaging" data-tier="free">
    <img class="probe-logo probe-logo-wm" src="https://upload.wikimedia.org/wikipedia/commons/9/9c/NATS-logo.png" alt="" loading="lazy">
    <span class="probe-name">NATS</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Server health, connections, message rates, JetStream</span>
  </a>

  <a href="pulsar/" class="probe-card" data-family="messaging" data-tier="free">
    <img class="probe-logo probe-logo-wm" src="https://upload.wikimedia.org/wikipedia/commons/f/fd/Apache-pulsar-logo.svg" alt="" loading="lazy">
    <span class="probe-name">Apache Pulsar</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Broker health, throughput, storage, backlog</span>
  </a>

  <a href="docker/" class="probe-card" data-family="containers" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/docker" alt="" loading="lazy">
    <span class="probe-name">Docker</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Per-container CPU, memory, network I/O, block I/O</span>
  </a>

  <a href="kubernetes/" class="probe-card" data-family="containers" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/kubernetes" alt="" loading="lazy">
    <span class="probe-name">Kubernetes</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Node, pod, deployment, PVC metrics via API server</span>
  </a>

  <a href="hyperv/" class="probe-card" data-family="containers" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://api.iconify.design/devicon/hyperv.svg" alt="" loading="lazy">
    <span class="probe-name">Hyper-V</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Per-VM CPU, memory and state via WMI (Windows only)</span>
  </a>

  <a href="proxmox/" class="probe-card" data-family="containers" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/proxmox" alt="" loading="lazy">
    <span class="probe-name">Proxmox VE</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Nodes, VMs, LXC containers, storage via REST API</span>
  </a>

  <a href="citrix/" class="probe-card" data-family="containers" data-tier="pro">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/citrix" alt="" loading="lazy">
    <span class="probe-name">Citrix VDI</span>
    <span class="probe-tier-badge pro">Pro</span>
    <span class="probe-desc">Sessions, machines, app launches, logon time, licenses</span>
  </a>

  <a href="otlp-receiver/" class="probe-card" data-family="collection" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/opentelemetry" alt="" loading="lazy">
    <span class="probe-name">OTLP Receiver</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Agent as edge OTLP collector (gRPC + HTTP in)</span>
  </a>

  <a href="prometheus-scrape/" class="probe-card" data-family="collection" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/prometheus" alt="" loading="lazy">
    <span class="probe-name">Prometheus Scrape</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Scrape /metrics endpoints into the pipeline</span>
  </a>

  <a href="exec/" class="probe-card" data-family="collection" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/console.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">Exec</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Run Nagios plugins or JSON scripts on interval</span>
  </a>

  <a href="syslog/" class="probe-card" data-family="collection" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/text-box-outline.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">Syslog</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">RFC3164/RFC5424 UDP/TCP receiver → OTLP logs</span>
  </a>

  <a href="filetail/" class="probe-card" data-family="collection" data-tier="free">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/file-document-outline.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">File Tail</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Glob-based log tailing with rotation + structured parsing</span>
  </a>

  <a href="event/" class="probe-card" data-family="collection" data-tier="pro">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/bell-ring-outline.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">Event</span>
    <span class="probe-tier-badge pro">Pro</span>
    <span class="probe-desc">HTTP receiver for custom JSON events → OTLP logs</span>
  </a>

  <a href="load-webapp/" class="probe-card" data-family="collection" data-tier="pro">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/timer-outline.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">Load WebApp</span>
    <span class="probe-tier-badge pro">Pro</span>
    <span class="probe-desc">Web page load time measurement (HTTP GET + timing)</span>
  </a>

  <a href="consul/" class="probe-card" data-family="devops" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/consul" alt="" loading="lazy">
    <span class="probe-name">Consul</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Agent health, catalog, serf members, raft, health-check states</span>
  </a>

  <a href="zookeeper/" class="probe-card" data-family="devops" data-tier="free">
    <img class="probe-logo probe-logo-wm" src="https://upload.wikimedia.org/wikipedia/commons/7/77/Apache_ZooKeeper_logo.svg" alt="" loading="lazy">
    <span class="probe-name">ZooKeeper</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Latency, connections, znodes via mntr four-letter command</span>
  </a>

  <a href="jenkins/" class="probe-card" data-family="devops" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/jenkins" alt="" loading="lazy">
    <span class="probe-name">Jenkins</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Job counts, build durations, queue depth, node status</span>
  </a>

  <a href="ceph/" class="probe-card" data-family="storage" data-tier="free">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/ceph" alt="" loading="lazy">
    <span class="probe-name">Ceph</span>
    <span class="probe-tier-badge free">Free</span>
    <span class="probe-desc">Cluster health, OSD counts, monitor quorum, pool stats</span>
  </a>

  <a href="veeam/" class="probe-card" data-family="storage" data-tier="pro">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/veeam" alt="" loading="lazy">
    <span class="probe-name">Veeam</span>
    <span class="probe-tier-badge pro">Pro</span>
    <span class="probe-desc">Backup jobs, repositories, proxies, license status</span>
  </a>

  <a href="redfish/" class="probe-card" data-family="hardware" data-tier="pro">
    <img class="probe-logo probe-logo-mdi" src="https://api.iconify.design/mdi/server-network.svg?color=%23666" alt="" loading="lazy">
    <span class="probe-name">Redfish</span>
    <span class="probe-tier-badge pro">Pro</span>
    <span class="probe-desc">Power, thermal, fans, disks, network adapters via BMC</span>
  </a>

  <a href="netscaler/" class="probe-card" data-family="adc" data-tier="pro">
    <img class="probe-logo probe-logo-si" src="https://cdn.simpleicons.org/citrix" alt="" loading="lazy">
    <span class="probe-name">NetScaler</span>
    <span class="probe-tier-badge pro">Pro</span>
    <span class="probe-desc">Virtual servers, SSL certs, GSLB, HA state via REST API</span>
  </a>

</div>

<p class="catalog-empty" id="catalog-empty" style="display:none">No probes match your search.</p>

</div>

<style>
.probe-catalog { margin-top: 1rem; }

/* Controls */
.catalog-controls { margin: 1.5rem 0; }
#probe-search {
  width: 100%;
  padding: .55rem .9rem;
  font-size: .95rem;
  border: 1px solid var(--md-default-fg-color--lightest);
  border-radius: 6px;
  background: var(--md-default-bg-color);
  color: var(--md-default-fg-color);
  margin-bottom: 1rem;
  box-sizing: border-box;
}
#probe-search:focus {
  outline: none;
  border-color: #00a5cc;
  box-shadow: 0 0 0 2px rgba(0,165,204,.15);
}
.catalog-families,
.catalog-tiers {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: .7rem;
}
.family-btn,
.tier-toggle {
  padding: 4px 12px;
  border-radius: 999px;
  border: 1px solid var(--md-default-fg-color--lightest);
  background: var(--md-default-bg-color);
  color: var(--md-default-fg-color--light);
  font-size: .78rem;
  font-weight: 600;
  cursor: pointer;
  transition: all .12s ease;
  font-family: inherit;
  line-height: 1.6;
}
.family-btn:hover,
.tier-toggle:hover {
  border-color: #00a5cc;
  color: #00a5cc;
}
.family-btn.active,
.tier-toggle.active {
  background: #00a5cc;
  border-color: #00a5cc;
  color: #fff;
}

/* Grid */
.probe-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 1rem;
  margin-top: 1.5rem;
}
.probe-card {
  display: flex;
  flex-direction: column;
  padding: 1rem;
  border: 1px solid var(--md-default-fg-color--lightest);
  border-radius: 8px;
  background: var(--md-default-bg-color);
  text-decoration: none !important;
  color: inherit !important;
  transition: border-color .15s ease, box-shadow .15s ease;
  gap: .35rem;
}
.probe-card:hover {
  border-color: #00a5cc;
  box-shadow: 0 2px 8px rgba(0,165,204,.15);
}

/* Logos */
.probe-logo {
  width: 2rem;
  height: 2rem;
  object-fit: contain;
  flex-shrink: 0;
  align-self: center;
}
[data-md-color-scheme="slate"] .probe-logo-si,
[data-md-color-scheme="slate"] .probe-logo-wm {
  filter: brightness(1.4);
}
[data-md-color-scheme="slate"] .probe-logo-mdi {
  filter: brightness(0) invert(0.8);
}

.probe-name {
  font-weight: 700;
  font-size: .95rem;
  margin-top: .2rem;
}
.probe-tier-badge {
  display: inline-block;
  font-size: .62rem;
  font-weight: 700;
  letter-spacing: .04em;
  text-transform: uppercase;
  padding: 1px 7px;
  border-radius: 999px;
  width: fit-content;
}
.probe-tier-badge.free {
  background: rgba(76,175,80,.16);
  color: #2e7d32;
}
.probe-tier-badge.pro {
  background: rgba(252,190,54,.25);
  color: #8a5d00;
}
.probe-desc {
  font-size: .78rem;
  color: var(--md-default-fg-color--light);
  line-height: 1.4;
}

/* Dark mode overrides */
[data-md-color-scheme="slate"] .probe-card {
  background: var(--md-code-bg-color);
}
[data-md-color-scheme="slate"] .probe-tier-badge.free {
  color: #66bb6a;
}
[data-md-color-scheme="slate"] .probe-tier-badge.pro {
  color: #ffd54f;
}

.catalog-empty {
  color: var(--md-default-fg-color--light);
  font-style: italic;
  margin-top: 2rem;
}
.probe-card.hidden {
  display: none;
}
</style>

<script>
(function() {
  var search = document.getElementById('probe-search');
  var empty = document.getElementById('catalog-empty');
  var familyBtns = document.querySelectorAll('.family-btn');
  var tierBtns = document.querySelectorAll('.tier-toggle');

  var activeFam = 'all';
  var activeTier = 'all';

  function filter() {
    var q = search.value.toLowerCase().trim();
    var visible = 0;
    document.querySelectorAll('.probe-card').forEach(function(card) {
      var fam  = card.dataset.family;
      var tier = card.dataset.tier;
      var name = card.querySelector('.probe-name').textContent;
      var desc = card.querySelector('.probe-desc').textContent;
      var text = (name + ' ' + desc).toLowerCase();
      var matchFam  = activeFam  === 'all' || fam  === activeFam;
      var matchTier = activeTier === 'all' || tier === activeTier;
      var matchText = !q || text.includes(q);
      var show = matchFam && matchTier && matchText;
      card.classList.toggle('hidden', !show);
      if (show) visible++;
    });
    empty.style.display = visible === 0 ? '' : 'none';
  }

  search.addEventListener('input', filter);

  familyBtns.forEach(function(btn) {
    btn.addEventListener('click', function() {
      familyBtns.forEach(function(b) { b.classList.remove('active'); });
      this.classList.add('active');
      activeFam = this.dataset.family;
      filter();
    });
  });

  tierBtns.forEach(function(btn) {
    btn.addEventListener('click', function() {
      tierBtns.forEach(function(b) { b.classList.remove('active'); });
      this.classList.add('active');
      activeTier = this.dataset.tier;
      filter();
    });
  });
})();
</script>
