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
  count, the runnable process count, the guest and guest nice modes, and
  page faults. On Windows: context switches, idle time, the processor
  count, the negotiated link speed and the page file size. On both: the
  speed and the operational state of a network interface.

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
  full tag set and were never affected. (#915)

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

- **The two libraries a registry scanner reports are raised**, and the
  image's base moves to a supported Alpine. (#900)

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
- Six probes declare dimensions the discriminant registry does not list.
  (#915)
- The collection gap with the native agent is closed on the families we
  cover and measured; what remains is recorded there. (#909)
