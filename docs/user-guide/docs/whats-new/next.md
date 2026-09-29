# Next (unreleased)

0.6.0 adds a native [Zabbix output](../zabbix.md): an active agent with
generated templates and autoregistration prepared by
`senhub-agent zabbix setup`, for Zabbix 6.0, 7.0 and 8.0. It also
introduces a separate administration key for the console and the
configuration API, makes `service.instance.id` a UUID, and brings the
fixes listed below.

<div class="rn-filter"></div>

## Before you upgrade

Actions for an installation running 0.5.x. Each one is detailed under
Breaking Changes or Fixes below.

1. **Console bookmarks and configuration API scripts.** A bookmarked
   console address carries the agent key and now answers 401 (Unauthorized): open the
   console with `senhub-agent console`, or get its address with
   `senhub-agent console --print`. Scripts calling the configuration API
   use the `admin_key`.
2. **`service.instance.id` changes once.** Dashboards and queries keyed on
   it start new series after the upgrade.
3. **Unfiltered `process` probe.** Add a `filter` to keep the
   per-process series; without one, only the per-name roll-up remains.
4. **Nagios thresholds.** A value equal to the threshold is now OK;
   review your thresholds. `nagios.yaml` is now read next to `agent.yaml`
   (the former location remains a fallback).
5. **Renamed ActiveMQ and Redfish metrics.** Update the alert rules and
   panels that use them.
6. **Veeam protected objects.** Discovered items are recreated once.
7. **Windows hosts installed from a pre-release (beta) MSI.** Copy
   `C:\ProgramData\SenHub` aside, uninstall the beta MSI once (Settings >
   Apps, or `msiexec /x`), then install the release: the uninstall
   removes the configuration directory.
8. **Zabbix users of a 0.6.0 pre-release.** Re-run `zabbix setup`, then
   relink the templates (**Mass update > Templates**) on the hosts that
   registered before; see
   [Zabbix](../zabbix.md#a-host-already-registered-keeps-the-templates-it-was-given).
9. **Interface queries.** SNMP interface series move from the label
   `interface_name` to `network_interface_name`; interface entities are
   replaced once.

## Breaking Changes

- **The web console and the configuration API need their own key.** The
  agent key is what a monitoring tool is given to read this agent, and
  it travels in the URL path, so it lands in the access log of every
  machine between the poller and the agent. It also opened everything
  that *changes* the agent: clearing the metric cache, injecting values
  into it, changing log levels, reading the agent's own logs, editing
  probes and outputs. A read-only consumer held the means to falsify
  what it read.

    The two surfaces are now told apart. `admin_key` on the `http`
    output opens the administration one, and it alone; the agent key
    reads and stops there. The administration key carries the read
    privilege too, so one key is enough to use the
    [web console](../web-interface.md).

    ```yaml
    http:
      endpoints: ["prtg", "web"]
      admin_key: "${secret:agent.admin_key}"
    ```

    **You do not have to do anything.** When the `http` output has no
    `admin_key`, the agent generates one at its first start and writes it
    into that output, where the next sealing pass moves it into the
    operating system's store like every other secret. Only if that
    generation fails are the administration routes left unregistered:
    they answer 404 until a key is set by hand. Pollers reading with the
    agent key are untouched. `senhub-agent console` resolves the key, so
    the Windows desktop and Start Menu shortcuts keep opening the console
    as before: they name the binary, never the key.

    What does change: an address you **bookmarked** carries the old key
    and now answers 401 (Unauthorized). Open the console from the shortcut, or run
    `senhub-agent console --print`, and bookmark that instead. Likewise
    for anything scripted against the configuration API with the agent
    key.

- **`service.instance.id` is now an RFC 4122 UUID.** The OpenTelemetry
  semantic conventions ask for a UUID; the agent now derives one (version
  5) from its key. Each agent's
  `service.instance.id` therefore changes once at upgrade: series keyed
  on it start new, and the agent's `service.instance` entity in a
  topology backend is replaced by a new one (the old one expires).
  Setting `resource.service.instance.id` on the OTLP output still
  overrides it.

- **Interfaces are keyed on `network.interface.name`.** The attribute
  that names a network interface, and identifies the `network.interface`
  entity with its device or host, follows the OpenTelemetry name:
  `interface.name` becomes `network.interface.name` on the entities, on
  the SNMP interface metrics and on the host network metrics (Prometheus
  label `network_interface_name`, which the host series already used).
  Every interface entity in a topology backend is replaced once, and SNMP
  interface series change label. On Windows, the host network metrics now
  carry the connection name (`Ethernet 2`) in `network.interface.name`,
  where the adapter description sat before; the description stays in the
  `interface` attribute.

- **MAC addresses take the OpenTelemetry form.** The entity attributes
  and identities that carry a MAC (SNMP and host interfaces, LLDP
  neighbours, the `mac:` device identity) render it in uppercase with
  hyphens, `BC-24-11-1B-04-82`, instead of `bc:24:11:1b:04:82`. Metric
  labels are unchanged: the Redfish and Wi-Fi probes pass on the MAC as
  the device reports it.

- **The `process` probe no longer reports every process by default.**
  Without a `filter`, it emitted six series per process, and the identity
  of those series carried the process id: a machine with 837 processes
  produced 4596 series and added about fifty a minute, for ever, since
  every program start minted a set that was never fed again. An
  unfiltered view now reports the roll-up over the processes sharing a
  name, which gains the CPU and the memory summed beside the count that
  was already there.

    This removes series on every output, not only on Zabbix. An
    installation running the probe without a filter loses
    `process.cpu.utilization`, `process.memory.usage`,
    `process.memory.virtual_memory_usage`, `process.threads`,
    `process.open_file_descriptors` and `process.uptime`, and gains
    `senhub.process.group.cpu.utilization` and
    `senhub.process.group.memory.usage` beside `process.count`.

    **To keep the per-process detail**, name what to watch or bound the
    sample. Any one of these is enough:

    ```yaml
    - name: process
      type: process
      params:
        filter:
          by_name: "^(nginx|php-fpm)"   # or by_user, or top_n
    ```

    The processes named this way also become entities on the topology
    graph, which an unfiltered view never did, for the same reason.

    A named view reports both: the per-process detail and the roll-up
    over the processes sharing a name. The detail is identified by the
    process id, so a named program that restarts leaves the items of its
    former workers behind on a sink that creates what it is sent. That
    is what watching a process one by one means, and the filter is what
    bounds it, which is why an unfiltered view reports the roll-up
    alone. (#910)

- **Nagios checks follow the plugin convention.** A threshold is now the
  last acceptable value: a value equal to it is OK, where the agent
  alerted on it. A metric is read as a health state only when its
  definition names its values, where any name containing `status` or a
  small integer was read that way: a count of two failed Veeam jobs
  read CRITICAL against a warning threshold of five. Review the
  thresholds you wrote against the old reading.

    The operator's `nagios.yaml` is now read from the directory that
    holds the agent configuration (`/etc/senhub-agent/` on Linux,
    `C:\ProgramData\SenHub\` on Windows), with the former location as a
    fallback. A file with a misspelt key, a threshold that is not a
    number or an unknown aggregation is refused and reported, where it
    was silently replaced by the shipped checks. See
    [Nagios](../nagios.md).

- **Three metric names change.** The per-destination ActiveMQ counts are
  `activemq.destination.consumer.count` and
  `activemq.destination.producer.count`; they shared the broker totals'
  names and rendered under the broker-wide channel. A Redfish drive's
  predicted failure is `senhub.hardware.physical_disk.failure_predicted`,
  a 0/1 gauge; as a state of `hw.status` it made every healthy drive
  overwrite its own health with `unknown`. Channels and display names
  are unchanged.

- **A Veeam protected object is identified by its id.** A machine
  protected by several jobs comes back once per job, and keyed on its
  name one entry overwrote the others: on one server, a third of the
  objects reported a sibling's figures. The series gain the object id,
  so discovered items are recreated once.

## Features

### Zabbix output

- **The [Zabbix output](../zabbix.md)**, as a native active agent. It
  connects out to port 10051, registers the host through
  autoregistration, asks which items the server wants and pushes their
  values. Low-level discovery finds the probe instances and the values
  each host actually feeds, the templates are generated from the same
  definitions the keys come from, and an optional listener answers the
  server's polls on 10050 in both wire dialects.

- **One command prepares the server.** `senhub-agent zabbix setup`
  imports the templates, creates the host group and creates the
  autoregistration action. After it, a machine needs the agent and two
  lines naming the server, with nothing typed in the Zabbix interface.
  It is an administrator command run once; a deployed agent never holds
  an API token. Every step is idempotent, which is also how a template
  is refreshed after an upgrade.

- **Zabbix 6.0, 7.0 and 8.0.** Every generated template imports into 7.0
  and 8.0, and autoregistration, discovery, inventory, the polled port
  and the proxy group redirection are tested on a server of each.
  `zabbix setup` reads the server's version first: before 6.4 it sends
  the API token in the request body, the only place those versions read
  it, and from 6.4 on in the `Authorization` header that 8.0 requires;
  before 6.2 it exports the templates in the 6.0 format with the matching
  import rules, without `--version`.

- **Proxies and proxy groups.** Point `server` at a proxy and nothing
  else changes. A proxy group is named by listing its members separated
  by commas: the agent talks to the first that answers and follows the
  group's redirection to whichever member holds the host, for the check
  list, the values and the heartbeat alike. When that member goes down
  the agent asks the configured addresses again, which is how it learns
  where the host moved.

- **One template set per platform.** A definition declares every metric
  its probe can produce, and a probe does not produce the same ones
  everywhere. The agent appends its operating system to the host
  metadata it registers with, and `setup` creates one autoregistration
  action per platform, so a Linux host is linked to the Linux templates
  and a Windows host to the Windows ones without anyone choosing.

- **The host's inventory fills itself.** The agent already discovers
  what the machine is, and Zabbix keeps those facts in host inventory.
  Nine text items carry the operating system, the hardware, the vendor,
  the model and the serial number into the matching fields, and `setup`
  puts new hosts in automatic inventory mode. A fact the agent did not
  find is not sent, so a field an operator typed by hand is not blanked.

- **Certificate encryption on the polled port.** The passive listener
  takes its own `tls` block: with a certificate it encrypts what it
  serves, and with an authority it also demands one from whoever polls.
  A certificate that cannot be read stops the agent rather than leaving
  a listener that serves in clear. (#904)

- **Pre-shared keys, both directions.** Most Zabbix sites encrypt with
  a pre-shared key, and it is the only encryption Zabbix offers for
  autoregistration. The agent speaks the TLS 1.2 PSK profile both Zabbix
  lines accept, outbound and on the polled port, and reads the key from
  a file hex-encoded the way Zabbix writes it, never from the
  configuration. An encrypted autoregistration creates the host with PSK
  set on both directions by itself. (#903)

- **The agent says what it is to the server**: its version, its session
  and the revision of the item list it holds. The server shows it in the
  host's agent columns and resends the list only when it changed, where
  it resent all of it at every refresh. (#905)

- **Behind NAT, the agent names the address to poll.**
  `passive.advertise` takes a name or an address and autoregistration
  creates the interface on it, instead of on the translated source
  address the server cannot reach. (#906)

- **`zabbix setup` names the other actions a new host would match.**
  Zabbix runs every autoregistration action whose condition matches, and
  two that link templates collide in silence: the host comes up with
  whichever set won and items that never fill. `setup` now lists those
  actions and says what will happen, without disabling one an operator
  wrote. (#907)

- **The templates raise problems.** A state metric raises a High or a
  Warning problem from the codes its lookup classes as an error or a
  warning, the classification PRTG and Nagios already read; processor,
  memory and disk usage raise a Warning and a High problem past
  thresholds held in macros a site overrides per host or per group.
  Items and triggers carry the `component` and `scope` tags the native
  templates use.

- **A utilization reads as a percentage in Zabbix.** The templates
  multiply the OTel fraction by 100 on the server side and show it in
  `%`, as the native agent does. The agent still sends the fraction,
  under the same key.

- **The agent answers for itself.** `agent.ping`, `agent.version` and
  `agent.hostname` are served on both rails and declared in a template
  of their own, so a host monitored actively has the availability line
  a native agent gives for free.

- **An application's own metrics reach Zabbix without anyone declaring
  them.** Zabbix speaks no OpenTelemetry, so an agent that speaks both
  is the bridge between an instrumented application and a Zabbix
  server. A template ships for the OpenTelemetry semantic conventions,
  HTTP server, JVM and database client, generated from the same
  definitions as every other template. A host carrying it discovers the
  applications relaying through the agent and creates their items, keyed
  on the sender's `service.name` and on the attributes the convention
  defines. Nothing is rewritten on the way: the name, the unit and the
  value stay the application's.

    A duration arrives as a distribution, and a sink holding one value
    per item cannot hold one, so it is sent as its count and its sum
    under keys that say which is which. A metric outside the shipped
    conventions keeps the shorter key its own name gives it. An
    application exporting part of a dimension set gets items only for
    what it sends. (#922, #940)

### OpenTelemetry and topology

- **The [container](../container.md) sends entities by default.** An
  agent started from the image exported measurements and logs but no
  entities, so a topology backend never saw the host, the agent or what
  it watches. The output the image writes now enables them;
  `SENHUB_ENTITIES=false` turns them off.

- **The agent shows its instance id.** `senhub-agent key instance-id`,
  `senhub-agent status` and the **Agent** card of the web console show
  the `service.instance.id` the agent's telemetry and entity carry, so
  it can be found in a metrics store or a topology graph.

- **Logs and traces from this machine carry its host identity.** An
  application sending its OTLP to the local agent gets the agent's
  `host.id` and `host.name` on what it sends, when it stated no
  `host.*` of its own, so the three signals of one machine join on one
  key. Only a sender known to be local is stamped: a new Unix socket
  listener (`address: unix:/run/senhub-agent/otlp.sock`) is local by
  construction, and a loopback TCP sender counts only when it is not
  itself a relay. Two counters,
  `senhub.agent.otlp_receiver.received` and
  `.received.without_host_id`, by signal, origin and service, make the
  join's coverage measurable.

- **`config check` says when an OTLP output will send no entity event.**
  Entities are off unless enabled, and a host missing from the topology
  had nothing anywhere saying why. (#938)

### Nagios

- **A Nagios command calls one check.** `GET
  /api/{key}/nagios/check/{name}` runs one configured check and answers
  in plugin format, `STATUS - message | perfdata`, with 404 for a check
  that is not configured. Before, configured checks were reachable only
  as JSON for all of them at once, which needed a wrapper script. The
  Nagios output has its own page, [Nagios](../nagios.md), tested against
  Nagios Core.

### Probes

- **Counters a native Zabbix agent reports.** On Linux: interrupts and
  context switches per second, the processor count, the runnable process
  count, the guest and guest nice modes, page faults, and the kernel's
  ceilings on open file descriptors and on processes, which is what the
  counted ones are measured against. On Windows: context switches, idle
  time, the processor count, the negotiated link speed and the page file
  size. On both: the speed and the operational state of a network
  interface, and the number of open login sessions. On Linux, the
  `os_updates` probe also reports how many packages are installed, which
  is what its pending count is measured against. (#909)

- **A new probe watches Azure Container Apps jobs.** The existing probe
  follows applications, which run continuously; a job is discrete: an
  execution starts, ends and leaves a verdict. `azure_container_app_jobs`
  reports whether the last run worked, how long it took, how long it has
  been since one succeeded, and publishes the console output of each
  execution once it has finished. It shares the credential, the Azure
  Resource Manager access and the read-budget pacing of the applications
  probe.

    The metric to alert on is the age of the last success, not the last
    status: a job that stopped being triggered reports a perfectly good
    last status for ever.

- **The Azure Container Apps probe reports the collection's own state**
  in detail, and **follows every application of a subscription** when a
  discovery block is set, instead of one named application. The role
  does not have to be granted across the subscription: Azure returns
  only what the credential may read.

- **The Veeam probe says which protected objects get no job status**,
  per platform, so a backup missing from the consolidated sensor can be
  explained from the Veeam console.

- **Every probe page lists every metric.** A generated reference gives
  each metric its OTel name, its PRTG and Nagios channel, its unit and
  its description. 303 of the 1314 metrics the probes emit were named
  nowhere, and the 93 IBM i metrics without a description now have one.

### CLI, console and packaging

- **`license activate` reads the token from standard input.**
  `senhub-agent license activate - < license.jwt` keeps the token out of
  the process list and the shell history, as `secret set` already does
  for secrets. The argument still works.

- **The Windows console shortcuts no longer run a VBScript.** They ran
  `wscript.exe` on a VBScript launcher whose only job was to open the
  console without showing a terminal: the agent being a console
  program, a shortcut aimed at it leaves a command window in front of
  the browser for as long as it waits for the service, up to fifteen
  seconds on a fresh install. VBScript is a feature-on-demand since
  Windows 11 24H2 and is being removed, and a scripting host launching a
  signed binary is a pattern endpoint protection flags. The console
  launcher no longer uses it: `senhub-console.exe`, a signed launcher
  built for the Windows graphical subsystem, opens the console without
  a terminal window.

    Nothing to do: the shortcuts are rewritten by the installer.

- **The systemd-creds [secret store](../secret-store.md) wires itself
  into the unit.** `install` and `refresh-unit` write the credentials
  drop-in from `creds.d/`, or remove it when the store is empty;
  `secret migrate --wire-unit` wires what it has just sealed;
  `uninstall` removes it. `secret wire-unit` was a step an operator had
  to know about. Tested under systemd 252, 255 and 257. (#605)

## Fixes

- **`prometheus_scrape` no longer warns on every exporter.** A target
  ending in `/metrics` was taken for the agent's own endpoint, which
  flagged node_exporter and almost any exporter, the probe's ordinary
  use. Only the agent's legacy path, or `/metrics` on this host's
  default agent port, is warned about now.

- **A [Cassandra](../probes/cassandra.md) node without traffic is no
  longer reported down.** On a node that has served no read or no write,
  the latency mean has no value yet and Jolokia returns it empty; the
  probe failed its whole collection on it and published `up = 0`. It
  now leaves that one measurement out. It also read an `Errors` counter
  that Cassandra does not have, which failed every collection on a real
  node: the errors metric is now the sum of failures, timeouts and
  unavailables.

- **The [ActiveMQ](../probes/activemq.md) probe reaches a current
  broker.** ActiveMQ Classic 5.16 and later refuse a Jolokia request that
  carries no `Origin` header (HTTP 403), which is every request the probe
  made, so the probe stayed down on a default install. It now names the
  broker's own address as the origin. The per-queue and per-topic
  metrics were never collected either: the probe listed destinations
  with a Jolokia request every broker rejects, and now searches for them.

- **The [WildFly](../probes/wildfly.md) probe authenticates on a
  default install.** The management interface asks for HTTP Digest and
  refuses Basic, the only scheme the probe sent: every collection ended
  on HTTP 401. The probe now answers the Digest challenge, and keeps
  working against an interface set to Basic.

- **The [ClickHouse](../probes/clickhouse.md) probe collects on a
  default install.** It read the Prometheus `/metrics` page on port 8123,
  which ClickHouse does not serve there (HTTP 404; the Prometheus endpoint
  is off unless configured on a port of its own), so the probe reported
  every server down. It now reads the system tables over the HTTP
  interface with the configured user. Three metrics named counters that
  ClickHouse does not have and were never emitted: connections is now
  the sum of the TCP, HTTP, MySQL and PostgreSQL connections, active
  parts reads `PartsActive`, and written data reads the bytes written to
  MergeTree parts.

- **The [vSphere HA](../probes/vsphere_ha.md) probe connects with the
  documented address.** Given a bare `https://vcenter` host, as in the
  documentation, it sent its requests to the root of the server instead
  of the `/sdk` endpoint, and vCenter answered 404 on every collection.
  The `/sdk` path, and `https://` when no scheme is given, are now
  supplied.

- **The [Redfish](../probes/redfish.md) probe reads standard power
  supplies and storage.** A supply described as the Redfish schema
  defines it (`InputRanges` as a list) failed to parse, so a conformant
  BMC produced no power-supply metric; the storage collection requested
  a doubled `Systems/Systems/...` path and got 404 on every generic
  system; and any supply whose name contained the letter "a" was tagged
  `controller=A`. Metrics that the probe emits without a definition are
  still missing from the Prometheus and OTLP outputs (#954).

- **The [Ceph](../probes/ceph.md) probe reports OSDs, monitors and pools
  as they are.** It read fields that the Manager dashboard API does not
  return: every cluster showed 0 OSDs up and 0 in, which an OSD-down
  alert fires on, and 0 monitors, and the pool statistics were never
  requested, so objects, stored data and operations stayed at 0.

- **The [NetScaler](../probes/netscaler.md) probe reports service group
  traffic.** It asked NITRO for all bindings and all member statistics
  at once, which NITRO refuses: every service group published 0
  requests, responses, throughput and connections whatever its traffic,
  and service groups were never tagged with their vServer. Bindings and
  member statistics are now read per object; a group whose members
  cannot all be read leaves its traffic metrics out rather than
  publishing a wrong sum.

- **An OTLP output that sends no entities says so.** A strategy file
  written without `signals.entities.enabled: true` sent metrics and logs
  but no entity, and a topology backend stayed empty with nothing in the
  agent log to explain it. The agent now logs a warning at start naming
  the setting.

- **The [process](../probes/process.md) roll-up no longer churns on
  Linux kernel workers.** Kernel workqueue threads rename themselves as
  they pick up work and live seconds, so each became its own roll-up
  series and, on the Zabbix output, an item left without data. They are
  now counted together under `kworker`.

- **The [Hyper-V](../probes/hyperv.md) probe collects.** Its two WMI
  queries named properties the Hyper-V classes do not have
  (`NumberOfProcessors` on `Msvm_ComputerSystem`, `CPUUsage` on
  `Msvm_SummaryInformation`), and WMI rejects a whole query for one
  unknown property: every collection failed with "Invalid query", on any
  host. The probe now reads the processor count and load where Hyper-V
  publishes them.

- **The [Pulsar](../probes/pulsar.md) probe reads the broker totals.**
  It looked only for the per-namespace series, which a broker publishes
  once topics exist: a current broker without topics reported `up` and
  nothing else, and one with topics reported per namespace. It now reads
  the broker-level `pulsar_broker_*` aggregates, and falls back to the
  per-namespace series on a broker that does not publish them.

- **A change made with `sudo` no longer stops a non-root service.**
  `sudo senhub-agent config set ...`, `secret set ...` and
  `license activate` rewrote the file as root with mode 0600. The Linux
  service runs as `senhub` and could no longer read it: the reload was
  refused, and the next restart failed to start. A file written by root
  in the configuration directory now takes the owner of that directory.

- **The install tells you which binary to call.** The closing line of a
  Linux install repeated the path you ran the installer from, often a
  download directory. It now names the installed
  `/usr/local/bin/senhub-agent`, by its full path: on the RHEL family
  `sudo` does not search `/usr/local/bin`, and `sudo senhub-agent` is
  "command not found" there.

- **A failed DNS lookup names the resolver it asked.** With `resolvers`
  set, the query went to the named resolver, but the error still named
  the system one (`lookup x on 127.0.0.53:53`), which sent operators to
  the wrong server. It now names the resolver that was queried.

- **The Docker and Swarm probes speak the API version the engine
  serves.** They called the Engine API at a fixed version 1.43. Docker
  Engine 29 refuses anything below 1.44, so the Docker probe fell back
  to reading cgroups, without container names, network or restart
  counts, and reported the socket as unreachable; Engine 20.10, which
  serves up to 1.41, refused it too. The probes now read the range the
  engine serves and pick a version inside it.

- **A target that goes down stops showing its last values.** A probe
  whose target is down keeps running and reports only that it is down.
  Its other values were still inside the window during which the agent
  presents a value as current, so OTLP exported them with fresh
  timestamps, Zabbix received them, and PRTG, Nagios and Prometheus
  served them, for up to about two and a half minutes. A run of a probe
  now retires the series it no longer reports. Zabbix keeps them in
  discovery, so its items are not disabled for the length of an outage.

- **Nagios checks read the same way throughout.** A metric the check
  aggregates, such as the processor in `system_health`, answered
  `OK - OK: cpu_usage_total 2.50% (aggregated from 1 metrics)` beside
  metrics written `cpu_user: OK 0.10%`. It now reads
  `cpu_usage_total: OK 2.50%`, and names the aggregation only when it
  combined several series: `(max of 4 series)`.

- **An output that starts after entity detection receives the entities
  at once.** The OTLP output subscribes to entity events when it starts,
  which can be just after detection's first cycle: at agent start, or
  when the output is enabled or changed from the console. It then missed
  that cycle and waited for the next re-emission, up to ten minutes by
  default, before learning of the host. A new subscriber now triggers a
  cycle that sends it the whole current state.

- **The [container](../container.md) reaches a collector that listens in
  plain text.** Its variables had no way to turn TLS off, so a collector
  without TLS, such as a sidecar on `localhost:4317`, could only be
  reached by mounting a whole configuration; the export failed on "first
  record does not look like a TLS handshake". `SENHUB_OTLP_TLS=false`
  now does it.

- **The free tier is described as it is.** Without a licence the agent
  logged "using free tier (cpu, memory, logicaldisk, network)", four
  probes, while the free tier runs every probe type except the paid
  ones. The message now says so.

- **No colour codes in container logs.** Run in the foreground, as a
  container runs it, the agent wrote its log lines with terminal colour
  codes, which reached `docker logs` and log collectors as `ESC[90m` and
  `ESC[32mINFESC[0m`. Colour is now used only when standard error is a
  terminal.

- **`status` answers inside a container.** It asked the service manager
  first and stopped when there was none, so in a container it printed
  `"rc-service" failed` and nothing else. It now says there is no
  service manager and asks the running agent, as it does on a host.

- **A configuration the agent cannot use no longer stops the output it
  replaces.** An edited output was stopped first and then rebuilt; when
  the new configuration was refused (a key file the service cannot read,
  a value out of range) the agent was left without that output until a
  restart. The new configuration is now checked first: refused, it is
  reported in the log and on the console while the output keeps running
  as before, and it is applied at the next change once fixed.

- **A listener refused its port says what to do.** Syslog on 514 or
  traps on 162, their standard ports, fail under the non-root Linux
  service with a bare "bind: permission denied". The error now adds that
  the port is below 1024 and names the two ways out: the
  `CAP_NET_BIND_SERVICE` capability in a unit drop-in, or a port above
  1023. The syslog guide no longer suggests `setcap`, which the service
  unit ignores.

- **A listener keeps listening when its configuration changes.** Editing
  a syslog, trap or OTLP receiver probe started the new instance while
  the old one still held the port: the new one failed with "address
  already in use", the old one was then stopped, and nothing listened
  until the retry two minutes later. The old instance is now stopped
  first.

- **Storing a first secret with systemd-creds no longer locks out the
  others.** On a host whose secrets live in the age store, one
  `secret set` with the systemd-creds backend switched the whole host to
  it: every age secret, the agent key among them, answered "secret not
  found", and the next start failed. While both stores exist the agent
  now reads both, which also makes moving from one to the other possible
  one secret at a time. See [Secret store](../secret-store.md).

- **`config check` judges required parameters by the probe's schema.**
  A hand-written table beside the schemas had gone stale: it demanded a
  `listen_address` syslog has never read, a `destination` for
  `ping_gateway`, which takes no parameter, and a `password` from a
  NetScaler probe configured with an API key, and reported working
  configurations in error.

- **A syslog or OTLP receiver probe is no longer CRITICAL in Nagios.**
  These probes relay records and hold no metric, and the probe summary
  answered "No metrics available", CRITICAL, for as long as they ran.
  It now reports the probe's own state: OK while it runs and receives,
  CRITICAL with the cause when it fails.

- **Enabling entities takes effect on save.** The console applies an
  output change without a restart, but entity detection kept the choice
  it made when the agent started: entities enabled from the console were
  only sent after the next restart. Detection now follows the
  configuration.

- **A console value that holds a reference is written as typed.** A
  header entered as `Bearer ${secret:name}` was sealed into a new secret
  whose value was that reference. The output resolved it when exporting,
  but the console's save check resolves one level, so every later edit
  of that output was refused as "unresolved". A value carrying `${...}`
  anywhere is now kept verbatim, as the start-time seal already did.

- **Chrony is offered on Linux and macOS only.** It declared no platform,
  so a Windows console offered it and it started there with no chronyc,
  reporting down for ever.

- **Windows Services names a selected service it cannot find.** A
  misspelt or unreadable service simply had no series; the probe now
  logs each such name once.

- **The console's list fields show their example one value per line.**
  A list field split on lines but displayed the probe's example
  comma-separated (`wuauserv, Spooler`), so a list typed like the
  example was saved as one value; for Windows Services that value matched
  no service and nothing was collected. Ten probe types had such an
  example.

- **The container's volume warning names only what is lost.** A
  container started with `SENHUB_HOST_ID` and `SENHUB_AGENT_KEY` but no
  volume was told it would arrive as a new host; those two variables
  already carry the identity and the key. The warning now lists only the
  log bookmarks in that case, and says what losing them costs per probe.

- **The Pro web application checks no longer flood the log.** Their
  probe types had no discriminant declaration, and the pull cache warned
  once per datapoint: dozens of lines a minute per probe. They are
  declared, like five other probe types that lacked one, the warning is
  logged once per type, and a test now requires the declaration for
  every probe that has a definition.

- **No internal routing label on the exported series.** The Pro web
  application and gateway checks carried `prtg_metric_id`, a tag meant
  for the legacy PRTG push, and it reached Prometheus, OTLP and Zabbix
  with its unexpanded `[name]` template. Private tags now stay inside
  the agent.

- **`config check` reads the service's environment.** A token set in the
  unit's `Environment=` or `EnvironmentFile=` was missing from the shell
  running the check, so every such host ended on an error for a working
  file. When the checked configuration is the one the installed service
  runs, the check now uses the unit's variables (a variable already
  exported in the shell wins). Linux only; values are never printed.

- **`config check` reports a probe name used twice.** The agent runs
  the first probe of that name and ignores the others, and only its log
  said so; the check listed every one of them as OK.

- **`refresh-unit` keeps the binary the service runs.** On a host
  still in the pre-0.5.4 layout that also held an older copy under
  `/usr/local/bin`, the refreshed unit pointed at that older copy,
  silently downgrading the agent.

- **No false warning about the update registry URL.** `update` and
  `config check` reported that a URL ending in `/` "carries a path";
  the built-in default itself triggered it on every update.

- **A systemd-creds install no longer reports a seal failure on every
  start.** The service runs as a non-root account and only root can
  encrypt with the host key, so the start-time seal always failed and
  restored its backups. It now says once that the inline secrets stay in
  place, and names the command that seals them.

- **A probe that runs less often than the push or the cache retention
  stays visible between two runs.** On OTLP, a gauge from a probe
  running every 30 minutes gave one sample per run, so an alert with a
  short lookback resolved while the condition still held (#890). The
  pull outputs dropped a value five minutes after it was produced, and
  the PRTG path held that limit in its own code whatever
  `cache.retention_minutes` said: an hourly probe such as `os_updates`,
  or an `exec` check every thirty minutes, answered an empty PRTG sensor
  most of the time, and the Web UI listed it with no metric. A value is
  now exported as current, and served on PRTG, Nagios and Prometheus,
  until its probe's next run is due.

- **`senhub-agent update` no longer needs the archive and the binary in
  memory.** It streams both, where it used about 250 MB for a 100 MB
  binary and was killed on a small host. (#891)

- **`config check` names the environment variable behind an empty
  credential**, instead of pointing at a configuration that may be
  correct. (#892)

- **Every application of a Container Apps collector reaches the pull
  outputs.** Following several applications split the state metrics by
  application, but the HTTP cache still keyed on the probe alone, so
  PRTG, Nagios and the Web UI published one application's state and
  dropped the rest, silently. The OTLP and Prometheus outputs key on the
  full tag set and were never affected.

    The same review found six more probes in that shape, predating it,
    and all six are now registered. Three carried a real loss: an IBM i
    host published one user profile class and dropped the others, a
    syslog collector published one sending machine's event count and
    dropped every other machine's, and a Redfish collector polling
    several service processors mixed their drives together. (#915)

- **The legacy PRTG POST endpoint no longer merges instances into one
  channel.** `POST /api/{key}/prtg/metrics` stripped the instance from
  channel names, and PRTG keeps one channel per name: a MySQL server's
  per-database sizes, a PowerStore's volumes and a Windows host's drives
  each came out as a single channel, every instance but the last lost.

- **Nagios checks apply what they declare.** `probe_filter` matches the
  probe type as well as its name, so the shipped Veeam checks work on a
  probe named otherwise; `tag_specific_thresholds`, `tag_<name>=` filters
  and POST overrides were parsed and ignored, and are now applied. A
  check naming a metric its platform never emits is reported at load.
  Plugin output carries no semicolon, which Nagios reserves, and lists
  its series in the same order at every poll.

- **The probe pages named 160 metrics the agent never emits**, some a
  misspelling of a real one, some never collected. A reader building a
  panel on one got an empty series.

- **Every metric declares the dimensions it carries**, instead of
  inheriting the probe's union. On swarm, kubernetes, netscaler and
  memcached a cluster counter claimed a container name, and Zabbix
  refused the resulting discovery rule. The memory probe declares which
  of its metrics exist only on Windows or only on Unix.

- **Redfish hardware health reads as a state on every output.** The
  tables that name the health codes (OK, Warning, Critical), the power
  states and the drive failure prediction lived in files the agent
  embedded and never read. A server's health reached PRTG, Nagios and
  Zabbix as a bare 0 to 3: no PRTG lookup to download, no Nagios state,
  no Zabbix value map and no trigger. They are now in the lookup
  registry, a drive predicting its own failure has a table of its own
  that calls it an error, and a test fails on any definition naming a
  lookup that does not exist. (#931)

- **A probe that cannot start is no longer absent in silence.** It was
  logged once at start and then left out of the probe total, so the
  agent reported every probe healthy while one, for instance with an
  expired password, collected nothing. It now counts in the total and
  never as healthy, the Web UI shows it failing with the reason, and the
  agent tries it again every two minutes, so a credential fixed
  afterwards is picked up without a restart. The error of a probe's
  first collection, which the scheduler swallowed, is logged too. (#935)

- **A copied example host identity is refused.** A container given
  `SENHUB_HOST_ID=01234567-89ab-cdef-0123-456789abcdef`, the shape of a
  documentation example, merged on the topology graph with every other
  host given it, silently. The container now refuses such a value at
  start, judged on its shape (all zeros, a long ascending run) rather
  than against a list; the agent logs an error when the machine itself
  reports one; and a host whose identity came from `SENHUB_HOST_ID`
  carries `senhub.host.id.source=configuration`, so a collision can be
  traced to the copy.

- **The Citrix licence grace period is exported in seconds.** The probe
  reports hours and the definition gave a unit the mapper cannot
  convert, so 48 hours left read as 48 seconds on OTLP and Prometheus.
  Probe definitions are now decoded strictly, a key the schema does not
  define fails the build, and a metric published in seconds or bytes
  must come from a unit the mapper converts. The same pass fixed the
  PRTG unit of ten metrics that showed a raw label (`BytesFile`,
  `TimeSeconds`, a byte count as `Count`), and the NetScaler heartbeat
  rates now read `pkt/s`. (#930)

- **A Windows host installed from a beta MSI can install the next
  release.** A beta MSI carried its version as `0.5.5-beta`, which
  Windows Installer does not read as a version: it registered as newer
  than any release, and `senhub-agent-0.5.6-amd64.msi` refused to
  install with "A newer version of SenHub Agent is already installed".
  The MSI now carries the numeric part only, and a release replaces the
  beta of the same number. **A host still on a beta MSI built before
  this fix must uninstall it once** (Settings > Apps, or `msiexec /x`);
  the configuration under `C:\ProgramData\SenHub` is removed by the
  uninstall, so copy it aside first.

- **A probe the open-source build does not carry names the edition that
  does.** On the open-source MSI a `veeam` probe was reported as
  "requires a valid license, upgrade license to enable", and
  `config check` said "unknown type". A licence cannot add code the
  binary does not contain; both now say the probe ships in the full
  edition.

- **`config check` no longer warns on every Windows MSI install** that
  "self-update will fail every cycle". It tried to open the running
  executable for writing, which the service holds, although an
  MSI-managed install updates through a new MSI and never writes its
  binary.

- **`config check` validates `nagios.yaml`.** A file the agent would
  refuse at start is an error, and a check naming a metric this
  platform does not emit is a warning, instead of both being found in
  the log after a restart. (#939)

- **`SENHUB_AGENT_KEY` keeps the agent identity of a container without a
  volume**, as `SENHUB_HOST_ID` keeps the host's. With both, two
  successive containers with no shared state report the same host and
  the same agent. (#882)

- **Saving an output from the Web UI no longer drops a header it did not
  show.** The console hides every value of the OTLP `headers` map, and
  the server only put back the ones that were `${secret:}` references:
  a plain `X-Tenant: acme` disappeared from the file on a save that
  changed nothing. What the console hides is now kept, by the same rule
  that hides it, and the preview of the file shows it. (#856)

## Internal

- The image publication scans before it pushes, and the dependency scan
  runs in the development chain, with the scanner used by common
  container registries; the two libraries it reported are raised and the
  image's base moves to a supported Alpine (#900). A published release is
  also checked for completeness rather than assumed finished.
- The documentation deploy refuses to run from a branch that publishes
  no line; a manual run from a release branch overwrote the development
  line. (#913)
