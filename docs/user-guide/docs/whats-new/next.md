# Next (unreleased)

Changes since 0.6.0, collected as they are merged.

<div class="rn-filter"></div>

## Breaking Changes

- **Metric names and units follow the Prometheus and OpenTelemetry
  rules.** `promtool check metrics` flagged 0.6.0: some series were
  exported in milliseconds, microseconds, nanoseconds, hours, days or
  bits per second, where both conventions ask for seconds and bytes, and
  some gauges ended in `.total`, which Prometheus reserves for counters
  (`senhub_veeam_jobs_total` read as a counter to every tool). There is
  no transition period: dashboards, alerts and recording rules on the old
  names must be updated when upgrading. PRTG and Nagios are not affected:
  they keep their channel names and the probe's display unit. Zabbix item
  keys are built from these names and values from these units, so re-run
  `zabbix setup` after upgrading to refresh the templates; the items under
  the old keys stop receiving data and their history stays under them.

  Units: every duration is now exported in seconds (Cassandra, Consul,
  Docker, Elasticsearch, Envoy, OpenSearch, RabbitMQ, Redis, SMART, Solr,
  Tomcat, ZooKeeper, and the HTTP check certificate expiry, which was in
  days), and every throughput in bytes per second (host network interface
  speed, NetScaler, Redfish, SNMP interfaces). The value is converted, so
  a Prometheus series gains the `_seconds` or `_bytes_per_second` suffix
  and a scale factor, for instance `senhub_redis_cmd_time_seconds_total`
  where `senhub_redis_cmd_usec_microseconds_total` was.

  Renamed gauges:

  | 0.6.0 | 0.6.1 |
  |---|---|
  | `senhub.ad_hybrid.sync.agents.total` | `senhub.ad_hybrid.sync.agent.count` |
  | `ceph.osd.total` | `ceph.osd.count` |
  | `senhub.citrix.machines.total` | `senhub.citrix.machine.count` |
  | `senhub.citrix.connection_failures.total` | `senhub.citrix.connection_failure.count` |
  | `senhub.ibmi.jobs.total` | `senhub.ibmi.job.count` |
  | `senhub.ibmi.hardware.total` | `senhub.ibmi.hardware.resource.count` |
  | `senhub.db.database.size.total` (MySQL) | `senhub.db.mysql.data.size` |
  | `gpu.memory.total` | `gpu.memory.limit` |
  | `oracle.sga.total` | `oracle.sga.size` |
  | `oracle.pga.total` | `oracle.pga.size` |
  | `oracle.tablespace.total` | `oracle.tablespace.limit` |
  | `phpfpm.processes.total` | `phpfpm.process.count` |
  | `proxmox.node.memory.total` | `proxmox.node.memory.limit` |
  | `proxmox.vm.memory.total` | `proxmox.vm.memory.limit` |
  | `proxmox.storage.total` | `proxmox.storage.limit` |
  | `rabbitmq.consumers.total` | `rabbitmq.consumer.count` |
  | `rabbitmq.queues.total` | `rabbitmq.queue.count` |
  | `rabbitmq.connections.total` | `rabbitmq.connection.count` |
  | `rabbitmq.channels.total` | `rabbitmq.channel.count` |
  | `unifi.devices.total` | `unifi.device.count` |
  | `unifi.clients.total` | `unifi.client.count` |
  | `senhub.veeam.jobs.total` | `senhub.veeam.job.count` |
  | `senhub.vsphere_ha.nsx.transport_nodes.total` | `senhub.vsphere_ha.nsx.transport_node.count` |
  | `redis.cmd.usec` | `redis.cmd.time` |
  | `smart.disk.power_on_hours` | `smart.disk.power_on.time` |
  | `senhub.docker.memory.working_set` | `container.memory.working_set` |

  In Prometheus, each name above takes underscores (`senhub_veeam_job_count`).
  Names ending in `.count` that OpenTelemetry defines, such as
  `system.process.count`, are kept although `promtool` warns on them.
  The Grafana dashboards shipped under `docs/grafana/` use the new names.

## Features

- **A route is identified by its destination and its next hop.** In the
  topology, a `network.route` was `{host.id, route.destination}` (a
  device's: `{network.device.id, route.destination}`) with its gateway as
  an attribute; the gateway now joins the identity, as IP-FORWARD-MIB
  indexes a route. Two routes to one destination through two gateways
  (two NICs, a VPN, ECMP) are two routes instead of one whose gateway
  changed, and a gateway change reads as the route through the old
  gateway gone and one through the new gateway present. Existing route
  entities are replaced once. Routes also carry their egress interface
  (`network.interface.name`), and Windows hosts now emit their routes.

- **Network interfaces carry their subnets.** A `network.interface`
  entity, on a host and on an SNMP device, now carries
  `network.interface.addresses`, the list of its addresses with their
  prefix (`10.10.0.60/24`), and `network.interface.subnets`, the subnets
  they are in (`10.10.0.0/24`), so a topology backend can tell which
  network an address belongs to. On a device the mask comes from IP-MIB
  `ipAdEntNetMask`.

- **The Zabbix server is set at install time.** The MSI takes
  `ZABBIX_SERVER` (and `ZABBIX_HOST_METADATA`), `config init` takes
  `--zabbix-server`, the container `SENHUB_ZABBIX_SERVER`. With a server
  prepared by `zabbix setup`, installing the agent is the only step: the
  host registers and fills in by itself, as with the Zabbix agent's
  installer. Until now the output had to be written by hand after the
  install.

## Fixes

- **PRTG shows NetScaler throughput at its real scale.** The NetScaler
  throughput and link-speed channels carry values in megabits per second
  but told PRTG they were in bits, so PRTG displayed them a million
  times too small. The channel's scale now follows the unit the probe
  reads.

- **File tail reads a Windows log its writer keeps open.** On Windows a
  file another process holds open for writing, such as PRTG's core log,
  sends no change notification until it is closed: the probe waited and
  read nothing, without an error. It now polls the file size on Windows.

- **A Windows host is linked to its gateway in the topology.** The agent
  read routes from the Linux routing table only, so a Windows host sent
  none and stood alone in a topology backend. It now reads the Windows
  routing table and links the host's routes to their gateway, as on
  Linux. On both, a gateway behind a container bridge (a Docker user
  bridge, a CNI bridge) is no longer emitted as a shared address: the
  same value exists on every such host.

- **A legacy single-file configuration no longer fails its migration at
  every start.** When `probes.d/` or `strategies.d/` held fragments, the
  migration to the multi-file layout failed its own check, restored the
  file and left its fragments and a backup behind, so it failed again at
  the next start: one host had 30 `agent.yaml.pre-multi-file.*` copies.
  Fragments a failed attempt left are now removed before migrating, a
  failed attempt undoes what it wrote and drops its backup, and fragments
  an operator wrote there stop the migration with a message naming them,
  since migrating would start what the agent ignores today. The backups
  already written can be deleted.

- **`config check` reports a probe parameter the probe does not read as a
  warning.** It was an ERROR although the agent starts the probe and
  ignores the key, so a script stopping on ERROR stopped on a working
  configuration. A missing required parameter is still an error.

- **`senhub-agent status` reaches an agent bound to one address or
  serving HTTPS.** It always asked `http://localhost:<port>`, so an HTTP
  output bound to one interface, or serving TLS, answered nothing and
  `status` reported a running agent as unreachable. It now dials the
  configured address (loopback when the output listens on every
  address) with the configured scheme, and names the address when it
  cannot reach it.

- **`config check` on Windows resolves `${env:}` as the service does.** A
  reference such as `Bearer ${env:OTLP_BEARER_TOKEN}` set for the service
  was reported as an error from an administrator prompt, which does not
  have the service's variables. The check now reads the service's
  environment (its registry key) when that service runs the checked
  configuration, as it already read the systemd unit's on Linux.

- **`update` restarts the Linux service.** It installed the new binary
  and printed "Restart the agent to use the new version", and the service
  kept running the old one, deleted from disk, while `senhub-agent
  version` read the new file: thirteen hosts of a fleet stayed on the
  previous release for a night with every check green. On Linux the
  running service is now restarted, and the command checks that the new
  process runs the installed binary and prints its version; it fails
  otherwise. `--no-restart` keeps the previous behaviour for scripts.

- **The OTLP export error and drop counters are present at 0.** Since
  0.6.0, `senhub.agent.otlp.export.errors` (by `signal`) and
  `senhub.agent.otlp.dropped` (by `reason`) appeared only after their
  first increment, so a dashboard on a healthy agent showed nothing, and
  an empty panel reads as "no error". Every signal and every reason is
  now emitted from start, at 0. In Prometheus the total across signals
  is `sum without(signal) (senhub_agent_otlp_export_errors_total)`.

- **A new installation answers PRTG, Nagios and Prometheus at once.** The
  installers enabled the console, PRTG and Nagios but not Prometheus, so a
  scrape answered 404 until the list was edited. Every endpoint is now on
  in a configuration an installer writes; what limits access is the
  listen address (loopback by default) and the keys. An existing
  configuration keeps its list.

- **The console's Agent card, `senhub-agent status` and `/health` report
  measured values.** CPU was a constant 0 %, and "Memory" was the Go
  heap, about a tenth of what the operating system charges to the agent.
  Memory is now the resident set (the working set on Windows), with the
  heap alongside, and CPU the agent's share of the whole machine over the
  last interval. Uptime counts from the process start. `/health` returned
  `"version": "HTTP Strategy v1.0"` and `info/system` a nested version of
  `"unknown"`; both carry the release now. `info/system` reported
  `cache.probe_count: 0` and a `cache.memory_usage` that was the process
  heap: the first is the real count, the second is gone.

- **The console counts Pro probe types the same way on every page.** The
  catalogue said "18 Pro types unlock with a license" where the Overview
  and Settings said 17: it counted a Pro type that does not run on this
  operating system, which no licence unlocks. Such a type is now counted
  with the types that run on another platform only. After a failed test,
  the probe editor printed "0 metrics" whatever the test had collected;
  it gives the real count.

- **Previewing a sensor URL no longer counts as a poller.** The console's
  preview reads the same route as PRTG or Nagios, so opening the Sensor
  URLs tab turned "no PRTG request seen" into "last PRTG request just
  now" for a sensor that did not exist yet. The export counts on the
  Outputs page now say they run since the agent started.

- **Test connection tests every push output.** On a Zabbix or SenHub
  cloud output it answered "test failed" in red, the same as an
  unreachable server, because no test existed. Zabbix now opens a TCP
  connection to each server or proxy address, SenHub cloud reaches the
  intake the agent pushes to. For the events output, the test reached
  the base URL; it now reaches `/event/insert`, where the agent posts.

- **Two development endpoints are gone.** `POST
  /api/{key}/debug/inject-test-metrics` and `inject-real-metrics`,
  reachable with the administration key, wrote invented Dell PowerVault
  series into the cache that PRTG, Nagios and Prometheus read, and
  answered with links to a developer's agent on `localhost:8080`.

- **The console's API reference lists the routes this agent serves.** Its
  endpoint list and count came from a hand-written table of 16 routes
  that missed most of the configuration, catalogue and information
  routes; they are now read from the agent's router. The page also
  rewrote `/admin/` paths to `/debug/`, showing routes that do not exist.

- **The container image answers PRTG, Nagios and Prometheus from outside.**
  Its HTTP output listened on the container's loopback, which nothing
  outside the container reaches: a published port answered "connection
  refused". The image now listens on every address, set with
  `SENHUB_HTTP_BIND`; `config init` takes `--http-bind`.
