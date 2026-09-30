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
  | `senhub.agent.probes.total` | `senhub.agent.probe.count` |
  | `kafka.consumer_group.lag_sum` | `kafka.consumer_group.topic.lag` |
  | `redis.ops.per_sec` | `redis.commands` |
  | `senhub.netscaler.ssl.certificate.days_to_expiration` (days) | `senhub.netscaler.ssl.certificate.expiry` (seconds) |
  | `senhub.veeam.license.days_remaining` (days) | `senhub.veeam.license.expiry` (seconds) |
  | `ceph.monitor.quorum_count` | `ceph.monitor.quorum.count` |
  | `senhub.citrix.machines.multi_session_fault_total` | `senhub.citrix.machine.multi_session_fault.count` |
  | `mongodb.lock.acquire.wait_count` | `senhub.mongodb.lock.queue.length` |

  In Prometheus, each name above takes underscores (`senhub_veeam_job_count`).

  The agent's own Prometheus endpoint also stops repeating a unit word a
  name already carries, as the OpenTelemetry Collector's translator does,
  which is the form the shipped Grafana dashboards query:
  `senhub_veeam_job_seconds_since_last_run_seconds` becomes
  `senhub_veeam_job_seconds_since_last_run`, and the byte counters of
  HAProxy, CouchDB, NATS, Tomcat, WildFly and container block I/O lose
  their second `bytes` (`senhub_haproxy_bytes_input_bytes_total` becomes
  `senhub_haproxy_bytes_input_total`). `senhub_netscaler_ns_throughput_bytes_per_second`
  is still reported by `promtool` as an abbreviated unit: `ns` is
  NetScaler's name for the appliance scope, not nanoseconds.
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

- **A newer Windows build replaces the installed exe.** `senhub-agent.exe`
  and `senhub-console.exe` had no version resource, so Windows Installer
  could not tell builds apart and an MSI of a newer build could keep the
  exe already installed. Both now carry their version (the release plus a
  build number, so every later build is higher), the company and product
  name, visible in the file's Properties > Details (#1002).

- **A removed probe leaves the PRTG, Nagios and Prometheus lists at once.**
  After its configuration was removed, renamed or disabled, a probe stayed
  in `/prtg/probes`, the Nagios views, Prometheus and the console until its
  cached series expired, up to an hour and a half for an hourly probe. The
  HTTP output now drops it on the reload (#997).

- **File tail follows a rotated idle file.** When an idle file was rotated
  or truncated, its bookmark kept the previous file's offset until the
  next line, so a restart in between skipped or replayed lines of the new
  file. The bookmark moves to the new file's start as soon as it is
  reopened (#999).

- **IPMI reports sensor readings, and no longer reports a missing sensor as
  failed.** The probe read `ipmitool sdr elist full` as if it were the
  plain `sdr` layout and took the sensor number for the reading, so no
  temperature, fan speed, voltage or power value was ever sent, only a
  status per sensor. The readings now reach every output: PRTG sensors on
  real hardware gain Temperature, Fan Speed, Voltage and PSU channels, which
  can bring a host with many sensors close to PRTG's channel limit per
  sensor. A sensor the BMC has no reading for (`ns`, "No Reading", such as
  an absent fan) sends no value instead of a status at 0 (#994).

- **An entity reported by two sources arrives whole.** When two probes of
  one agent described the same entity in a cycle (a device polled directly
  and seen in another's LLDP table, an address named by a route and by an
  interface), its relations went to whichever copy came last, and a thin
  copy could replace a full one. The copies are now merged: every
  attribute and every relation. Where two sources disagree on a value,
  the one kept does not depend on the order the probes started in; the
  disagreement is logged once as a warning and counted in
  `senhub.agent.entity.attribute.conflicts`, since it is a defect to fix.

- **An SNMP device keeps its full description in the topology.** Each
  LLDP neighbour was also built as a device carrying only its name, then
  dropped before sending with a warning, 54 a minute on a 40-device lab.
  Where that neighbour was also polled, its thin copy could win over the
  full one, and the device reached the topology backend with its name
  alone. Neighbours are no longer built; the warnings stop.

- **Syslog RFC 3164 messages are timed in the host's zone.** The header
  of an RFC 3164 message carries the sender's local time with no zone, and
  the parser read it as UTC: on a host in Paris, a UniFi access point's
  09:49:59 reached the log store as 09:49:59Z, two hours in the future, so
  a search on the last minutes found nothing and log alerts fired two
  hours late. The timestamp is now read in the agent host's zone. RFC 5424
  timestamps, which carry their offset, are unchanged.

- **TLS and the endpoint list apply without a restart.** Enabling or
  removing the `tls` block of the HTTP output logged a successful update
  but kept serving the previous protocol until the bind address changed
  or the service restarted; an endpoint taken out of `endpoints` kept
  being served likewise. Any change to `tls` now rebuilds the listener,
  and the endpoint list is replaced, not added to, on each reload.

- **File tail no longer reads an idle file again after a restart.** A
  watched file that wrote no line while the agent ran was saved in the
  bookmark at offset 0 when the agent stopped, and read again from its
  first byte at the next start (over 1 600 records for one PRTG log).
  Each file's position is now recorded as soon as its tail starts. Files
  seen for the first time are read as before: from the end, or from the
  first byte with `from_beginning`.

- **File tail keeps its bookmark up to date after a burst.** Lines
  arriving together inside one flush interval left the bookmark at the
  first of them until another line came, so a crash meanwhile replayed
  lines already sent. The bookmark is now written within two seconds of
  the last line read, whether or not another follows.

- **`config check` no longer prints the agent key.** It reports that the
  key is set and has the expected form.

- **The console's connection tests fail when the output would.** The
  SenHub cloud test passed on any HTTP answer, a 404 from the intake
  included, without checking the agent key: it now fails when the key is
  missing or refused. The PRTG and event tests fail on a 404 or a server
  error instead of counting them as reached.

- **`zabbix setup` no longer prints a trapper port it cannot know.** Its
  install instructions showed the frontend host with `:10051` whatever
  port the server listens on; they now show the host alone, which the
  agent completes with 10051, and say to add the port when it differs.

- **A restart no longer removes SNMP links from the topology for one
  cycle.** After a restart, a device polled before its LLDP neighbours
  could not resolve them yet and left those links out of what it
  published until its next topology sweep, one polling cycle (five or
  ten minutes on the lab fleets, an hour on a fleet polled hourly); a
  topology backend recorded the links as removed by the agent
  and got them back one sweep later. Neighbours are now resolved when
  the topology is published, and after the start a device waits for the
  neighbours the agent has not polled yet instead of publishing without
  them, for at most a third of the entity liveness interval (two minutes
  by default). The backend keeps the device as it was meanwhile. The
  cost: during that wait, a lost report can let the device expire in the
  backend where it used to survive two; the agent chooses an honest
  expiry over a false removal.

- **SNMP devices no longer expire in the topology between two reports.** The
  agent publishes a whole topology cycle at once; on a fleet of about
  forty devices that is more events than the OTLP exporter's entity
  buffer holds, and the overflow was dropped without a trace. The same
  leading devices were dropped cycle after cycle, stayed unannounced past
  their liveness interval, and expired in the topology backend together
  with their interfaces, to come back a few minutes later; on the lab
  fleet some did so twenty times in twelve hours. A full buffer now makes
  the publish wait for room, and an event still dropped, when the
  exporter stops draining, is counted under the OpenTelemetry SDK names
  a standard dashboard reads: `otel.sdk.processor.log.processed` with
  `error.type="queue_full"` (without it, the events handed over), and
  `otel.sdk.processor.log.queue.size` and `.capacity` for the hand-off's
  buffer, identified by `otel.component.type="senhub_entity_channel"`. In
  Prometheus: `senhub_otel_sdk_processor_log_processed_total`. Beta 2
  counted it as `senhub.agent.otlp.dropped{reason="entity_queue_full"}`,
  which no stable release carried.

- **Hardware health follows the OpenTelemetry states.** Redfish emitted
  `hw.status{hw.state="unknown"}` for a component whose health the BMC does
  not report; the convention has no such state, so a conforming backend
  ignored the series. Such a component now has every state at 0. A drive's
  predicted failure, a separate `senhub.hardware.physical_disk.failure_predicted`
  gauge since 0.6.0, is again the `hw.state="predicted_failure"` series of
  its `hw.status`, which the shipped Redfish Grafana dashboard reads: its
  "predicted failure" panel showed nothing in 0.6.0. IPMI's `hw.status` gains
  the `hw.state="ok"` attribute the convention requires. PRTG channels do
  not change; the Zabbix items of both change key, refreshed by re-running
  `zabbix setup`.

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
  The fix is in the new release's `update`: going from 0.6.0 to 0.6.1 still
  runs the 0.6.0 command, so restart the service once after that update
  (`sudo systemctl restart senhub-agent`).

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
