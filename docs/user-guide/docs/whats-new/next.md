# Next (unreleased)

Changes merged since 0.5.6, not yet released.

The release brings the Zabbix output, proven side by side against the
native Zabbix agent on a Linux and a Windows host, and closes the
collection gaps that comparison exposed.

<div class="rn-filter"></div>

## Breaking Changes

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
    bounds it — which is why an unfiltered view reports the roll-up
    alone. (#910)

- **Nagios checks follow the plugin convention.** A threshold is now the
  last acceptable value: a value equal to it is OK, where the agent
  alerted on it. A metric is read as a health state only when its
  definition names its values, where any name containing `status` or a
  small integer was read that way — a count of two failed Veeam jobs
  read CRITICAL against a warning threshold of five. Review the
  thresholds you wrote against the old reading.

    The operator's `nagios.yaml` is now read from the directory that
    holds the agent configuration (`/etc/senhub-agent/` on Linux,
    `C:\ProgramData\SenHub\` on Windows), with the former location as a
    fallback. A file with a misspelt key, a threshold that is not a
    number or an unknown aggregation is refused and reported, where it
    was silently replaced by the shipped checks.

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
  name one entry overwrote the others: on a real server, 66 of 200
  objects reported a sibling's figures. The series gain the object id,
  so discovered items are recreated once.

## Features

- **The Zabbix output**, as a native active agent. It connects out to
  port 10051, registers the host through autoregistration, asks which
  items the server wants and pushes their values. Low-level discovery
  finds the probe instances and the values each host actually feeds,
  the templates are generated from the same definitions the keys come
  from, and an optional listener answers the server's polls on 10050 in
  both wire dialects.

- **One command prepares the server.** `senhub-agent zabbix setup`
  imports the templates, creates the host group and creates the
  autoregistration action. After it, a machine needs the agent and two
  lines naming the server, with nothing typed in the Zabbix interface.
  It is an administrator command run once; a deployed agent never holds
  an API token. Every step is idempotent, which is also how a template
  is refreshed after an upgrade.

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

- **Zabbix 7.0 and 8.0.** Every generated template imports into both
  lines, and autoregistration, discovery, inventory, the polled port and
  the proxy group redirection are measured on a server of each. On 8.0
  the API wants its token in a header; `zabbix setup` has always sent
  it there.

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
  thresholds held in macros a site overrides per host or per group. A
  template linked to a host used to collect everything and alert on
  nothing. Items and triggers carry the `component` and `scope` tags the
  native templates use.

- **A utilization reads as a percentage in Zabbix.** The templates
  multiply the OTel fraction by 100 on the server side and show it in
  `%`, as the native agent does, where an operator read `0.9531` for a
  processor at 95 %. The agent still sends the fraction, under the same
  key.

- **The agent answers for itself.** `agent.ping`, `agent.version` and
  `agent.hostname` are served on both rails and declared in a template
  of their own, so a host monitored actively has the availability line
  a native agent gives for free.

- **The counters a native Zabbix agent reports and we did not.** On
  Linux: interrupts and context switches per second, the processor
  count, the runnable process count, the guest and guest nice modes,
  page faults, and the kernel's ceilings on open file descriptors and on
  processes, which is what the counted ones are measured against. On
  Windows: context switches, idle time, the processor count, the
  negotiated link speed and the page file size. On both: the speed and
  the operational state of a network interface, and the number of open
  login sessions. On Linux, the `os_updates` probe also reports how many
  packages are installed, which is what its pending count is measured
  against. (#909)

- **An application's own metrics reach Zabbix without anyone declaring
  them.** Zabbix speaks no OpenTelemetry, so an agent that speaks both
  is the only bridge between an instrumented application and a Zabbix
  server — and the bridge was half built: the values arrived, the server
  asked for none of them. A template now ships for the OpenTelemetry
  semantic conventions, HTTP server, JVM and database client, generated
  from the same definitions as every other template. A host carrying it
  discovers the applications relaying through the agent and creates
  their items, keyed on the sender and on the attributes the convention
  defines. Nothing is rewritten on the way: the name, the unit and the
  value stay the application's.

    A duration arrives as a distribution, and a sink holding one value
    per item cannot hold one, so it is sent as its count and its sum
    under keys that say which is which. A metric outside the shipped
    conventions keeps the shorter key its own name gives it. An
    application exporting part of a dimension set gets items for the
    rest of it, which stay empty. (#922)

- **A new probe watches Azure Container Apps jobs.** The existing probe
  follows applications, which run continuously; a job is discrete — an
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

- **The image publication scans before it pushes**, and the dependency
  scan runs in the development chain, with the scanner a customer
  registry runs. A published release is also checked for completeness
  rather than assumed finished.

- **A Nagios command calls one check.** `GET
  /api/{key}/nagios/check/{name}` runs one configured check and answers
  in plugin format, `STATUS - message | perfdata`, with 404 for a check
  that is not configured. Before, configured checks were reachable only
  as JSON for all of them at once, which needed a wrapper script. The
  Nagios output has its own page, proven against a real Nagios Core.

- **Every probe page lists every metric.** A generated reference gives
  each metric its OTel name, its PRTG and Nagios channel, its unit and
  its description. 303 of the 1314 metrics the probes emit were named
  nowhere, and the 93 IBM i metrics without a description now have one.

- **The Veeam probe says which protected objects get no job status**,
  per platform, so a backup missing from the consolidated sensor can be
  explained from the customer's own console.

- **`config check` says when an OTLP output will send no entity event.**
  Entities are off unless enabled, and a host missing from the topology
  had nothing anywhere saying why. (#938)

## Fixes

- **A value from a probe that runs less often than the push is exported
  as current between two runs.** A gauge from a probe running every 30
  minutes gave one sample per run, so an alert with a short lookback
  resolved while the condition still held. (#890)

- **`senhub-agent update` no longer needs the archive and the binary in
  memory.** It streams both, where it used about 250 MB for a 100 MB
  binary and was killed on a small host. (#891)

- **`config check` names the environment variable behind an empty
  credential**, instead of pointing at a configuration that may be
  correct. (#892)

- **`config check` accepts every output compiled into the build.** It
  read a fixed list, so a `zabbix` output was reported as unknown on a
  build that carries it.

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

- **A third of a freshly registered Zabbix host's items no longer stay
  empty.** Several metrics collapse onto one OTel name and differ by the
  value of one attribute, and a platform feeds only part of such a
  family. The agent now discovers which values it really sends, so only
  those become items. (#898)

- **A metric a platform never produces is no longer declared on it.**
  A Linux host carried the processor's deferred procedure calls and the
  peak of a page file it does not have. (#908)

- **A metric's own dimensions replace the probe's rather than extend
  them.** Merging them gave the Windows drive metrics a device and a
  mount point they do not have, and combined with the rule that skips a
  series carrying none of its labels it dropped every filesystem from a
  Linux host.

- **Two applications relaying the same metric name through one agent no
  longer share a Zabbix item.** The OTLP receiver relays what
  applications send under the names they chose, and the agent describes
  none of those names, so the key carried the receiving probe alone.
  Two services reporting `http.server.request.duration` built the same
  key, and the second value overwrote the first on an item that went on
  looking healthy. The key now carries the sender's `service.name`, and
  a discovery rule offers the senders seen. Nothing was lost in
  practice, because no template declares those keys and a Zabbix server
  asks for nothing else — but an operator creating the item by hand
  walked straight into it.

- **The two libraries a registry scanner reports are raised**, and the
  image's base moves to a supported Alpine. (#900)

- **Naming a probe in `zabbix setup` no longer unlinks the others.**
  `--probe` replaced the templates every machine runs instead of adding
  to them, although the command's own message says they are linked as
  well: an operator adding one commercial template silently unlinked the
  processor, the memory, the network and the disks from the
  autoregistration action, and every host registering afterwards came up
  with none of them.

- **The documentation deploy refuses to run from a branch that
  publishes no line.** It chose the version line by asking whether the
  branch was master and published everything else as the development
  line, so a manual run from a release branch overwrote it. (#913)

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
  refused the resulting discovery rule. The memory probe declares which of its
  metrics exist only on Windows or only on Unix.

- **Every generated Zabbix template imports into a real server.** Eleven
  were refused: a display name with a slash or an ampersand, and the
  6.0 export format.

- **A probe that runs less often than the Zabbix push stays visible
  between two runs.** The output forgot a value after three push
  intervals, so an hourly probe such as `os_updates` reached the server
  ninety seconds an hour: a host registered in between waited an hour
  for its first value, and the probe's discovery vanished until the next
  run. The value is now kept until its probe's next run is due, as the
  OTLP output does since #890.

- **A probe that runs less often than every five minutes stays visible
  on PRTG, Nagios and Prometheus between two runs.** The pull outputs
  dropped a value five minutes after it was produced, and the PRTG path
  held that limit in its own code whatever `cache.retention_minutes`
  said. An hourly probe such as `os_updates`, or an `exec` check every
  thirty minutes, answered an empty PRTG sensor most of the time, and
  the Web UI listed it with no metric. The value is now served until its
  probe's next run is due.

- **Redfish hardware health reads as a state on every output.** The
  tables that name the health codes (OK, Warning, Critical), the power
  states and the drive failure prediction lived in files the agent
  embedded and never read. A server's health reached PRTG, Nagios and
  Zabbix as a bare 0 to 3: no PRTG lookup to download, no Nagios state,
  no Zabbix value map and no trigger. They are now in the lookup
  registry, a drive predicting its own failure has a table of its own
  that calls it an error, and a test fails on any definition naming a
  lookup that does not exist. (#931)

- **A Zabbix instance no longer gets items for metrics it never
  sends.** A discovery rule created every item of its dimension set for
  every instance, so a virtual network card whose kernel reports no
  speed carried a speed item that stayed empty for ever. The agent now
  lists, per instance, the metrics it feeds, and the templates only
  create those. (#940)
