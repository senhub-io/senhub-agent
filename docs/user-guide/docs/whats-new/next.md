# Next (unreleased)

Changes merged since 0.5.6, not yet released.

The release brings the Zabbix output, proven side by side against the
native Zabbix agent on a Linux and a Windows host, and closes the
collection gaps that comparison exposed.

<div class="rn-filter"></div>

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
    privilege too, so one key is enough to use the console.

    ```yaml
    http:
      endpoints: ["prtg", "web"]
      admin_key: "${secret:agent.admin_key}"
    ```

    **Without it the administration surface is not served at all** — its
    routes are not registered and answer 404, rather than asking for a
    key nobody has. An installation that exists to feed PRTG or Nagios
    never needed that surface and no longer carries it; its pollers are
    untouched.

    **You do not have to do anything.** An agent that starts without an
    administration key generates one and writes it into its `http`
    output, where the next sealing pass moves it into the operating
    system's store like every other secret. `senhub-agent console`
    resolves it, so the Windows desktop and Start Menu shortcuts keep
    opening the console exactly as before — they name the binary, never
    the key.

    What does change: an address you **bookmarked** carries the old key
    and now answers 404. Open the console from the shortcut, or run
    `senhub-agent console --print`, and bookmark that instead. Likewise
    for anything scripted against the configuration API with the agent
    key.

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
  login sessions. The `os_updates` probe also reports how many packages
  are installed, which is what its pending count is measured against.
  (#909)

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

- **The Azure Container Apps probe reports the collection's own state**
  in detail, and **follows every application of a subscription** when a
  discovery block is set, instead of one named application. The role
  does not have to be granted across the subscription: Azure returns
  only what the credential may read.

- **The image publication scans before it pushes**, and the dependency
  scan runs in the development chain, with the scanner a customer
  registry runs. A published release is also checked for completeness
  rather than assumed finished.

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

## Known follow-ups

- Pre-shared keys are not supported. The scope is measured and the
  decision is open. (#903)
- The agent declares no version and ignores the configuration revision,
  so the server resends the whole item list at every refresh. (#905)
- The interface autoregistration creates takes the source address, which
  is wrong behind NAT. (#906)
- A server that already carries an autoregistration action matching the
  same host metadata ends up with two, and the newer one loses silently.
  (#907)
- The collection gap with the native agent is closed on the families we
  cover and measured; what remains is recorded there. (#909)
