# Next (unreleased)

Changes land here as they are merged to `dev`.

- **Redis**: the probe now reports the last RDB save, AOF rewrite and AOF write outcomes, the replication backlog (active, size, history), pub/sub channels and patterns, and, on a Sentinel, the status, replica count and sentinel count of each monitored master.
- **Linux packages.** `.deb` and `.rpm` packages for amd64 and arm64, in two editions (`senhub-agent-oss` and `senhub-agent`, which replace each other in one install command), install the agent as a systemd service from the distribution's package manager, tested on Debian 12, Ubuntu 22.04 and 24.04, Rocky Linux 9 and openSUSE Leap 15.6; see [Install from packages](../installation.md#install-from-packages). Signed APT and YUM/DNF/Zypper repositories at `packages.senhub.io` go live with the first published beta; see [Install from the package repositories](../installation.md#install-from-the-package-repositories).
- **Ansible collection.** `senhub.agent` is an Ansible collection with one role, `senhub.agent.agent`, that installs the agent from the signed package repositories (Linux) or the release MSI checked against its manifest (Windows), writes `probes.d` and `strategies.d` from variables, and runs `config check --json` on the result before it replaces the live files; a second run changes nothing. See [Deploying with Ansible](../ansible.md).

- **IPMI**: sensors that share a name (a Dell lists every CPU temperature as `Temp`) are no longer merged into one series; they now read `Temp (CPU 1)`, `Temp (CPU 2)`. Power (`hw.power`, watts) and current (`senhub.hardware.current`, amperes) readings are reported, and a power supply's presence and redundancy ("Presence detected", "Fully Redundant", "Redundancy Lost") now give its status.

<div class="rn-filter"></div>

## Fixes

- **`config check` and the loader now agree on a configuration with no storage strategy.** `config check` reported "No storage strategies configured" as a warning (exit `1`) while the agent refuses to load such a file ("at least one storage strategy is required"). It is now an error (exit `2`), as is a storage entry with no name. A script that applies a configuration after `config check` (Ansible, the MSI) no longer lets one through that the agent then rejects.

## Features

- **A manifest per release, at a stable address.** `packages.senhub.io/releases/stable/latest.json` (and `beta/latest.json`) name the newest release of a channel; `releases/<version>/manifest.json` lists every file with its checksum, size, signature and the container images, so a script finds and verifies the right build without reading the GitHub page. A stable image is now also tagged with its minor line (`ghcr.io/senhub-io/senhub-agent:0.6`). See [Downloading and verifying releases](../releases.md).

- **SNMP: a polled device's routes now link to their gateway.** Routes read from a device's routing table reach the gateway's address with `next_hop_via`, as host routes already do, when the gateway is a public address, so a router's default route resolves to the same address node as a host's. Private (RFC1918, CGNAT, ULA), loopback, link-local and multicast gateways stay unlinked, because unrelated switches behind one private gateway would otherwise merge.
- **Failed log batches are kept on disk by default.** The OTLP output's
  on-disk queue for event logs the collector could not take used to run
  only when `persistence.path` was set. It is now on for every install
  (package, MSI, container), in the state directory (`otlp-queue/`), and
  replays at boot and when the collector answers again. Behaviour change:
  during an outage the agent can now use up to 128 MiB of disk in the
  state directory (`logs_queue_max_bytes`). New `logs_queue_max_age`
  (24 hours) drops older batches at boot and every 10 minutes, counted as
  `dropped_by_age`. `persistence.enabled: false` or `SENHUB_LOG_QUEUE=false`
  turns it off; `SENHUB_LOG_QUEUE_RETENTION` and
  `SENHUB_LOG_QUEUE_MAX_BYTES` tune it. An unwritable state directory logs
  one warning and the agent runs without the queue. In a container, mount
  a persistent volume on the state directory for the queue to outlive a
  restart. An explicit `persistence.path` keeps working unchanged. See
  [Logs survive an outage](../otlp.md#logs-survive-an-outage).
- **Probes from environment variables.** `SENHUB_PROBE_<NAME>_TYPE=<type>` declares a probe and `SENHUB_PROBE_<NAME>_<PARAM>=value` sets its parameters, typed from the probe's schema, with `__` for nested keys and `_FILE` to read a secret from a file. The agent reads them itself, so containers, systemd units, Helm and Podman share one rule, and they adjust the probes of a mounted configuration too (the environment wins, key by key). A mistake stops the load and names the variable. `config show` lists the variables the probes were read from, and never prints a secret read from the environment. See [Configuring probes from environment variables](../configuration.md#configuring-probes-from-environment-variables).
- **Probe SDK: the state directory.** `probesdk/state` gives a probe the directory where the agent keeps what must survive a restart (`state.Dir()`, `state.Path(name)`), so a probe that keeps a bookmark can default it beside the agent's identity.

- **IBM i: QHST, QSYSOPR and audit events reach the logs backend.** The history log, message queue and audit journal collectors read their events but sent none of them on: each event is now an OpenTelemetry log record (message text as body, the partition's own time as timestamp, IBM i severity 0-99 mapped to `INFO`, `WARN`, `ERROR`, `ERROR3` or `FATAL`, attributes under `ibmi.*`). `history_log_min_severity` is the volume lever. The scheduled-job and other age gauges no longer read below zero when the partition clock runs ahead of the reference.
- **`status` works on every install.** The agent now answers `senhub-agent status` on a local channel (a Unix socket in its state directory, readable by the service account and root only; a named pipe restricted to administrators on Windows), whether or not the HTTP output is enabled. Hosts installed before the HTTP output was on by default used to get a degraded view computed by the command itself. `status` asks the local channel first and falls back to the HTTP output. The channel is read-only: it sends the status and reads nothing. It reports probe health and failed outputs but not the per-probe metric counts, which only the HTTP cache holds.
- **Two agents on one host no longer describe each other twice.** Each agent
  writes its own `service.instance.id` to `instance.id` in its state
  directory (`/var/lib/senhub-agent`, `C:\ProgramData\SenHub`), readable by
  other accounts only when the agent key is a random UUID, and by its owner
  alone otherwise. An agent that finds another agent's process listening on
  the host reuses that id instead of creating a second `service.instance`.
  When the file cannot be read, the previous behaviour applies.
- **Redfish: the whole probe now reaches Prometheus and OTLP.** 100 of the
  139 metrics the probe can emit had no definition and were dropped by
  those outputs (processors, memory modules, network, power, firmware,
  event-log counters, vendor storage readings). They are now declared with
  OTel names, units and types, and a test fails when the probe starts to
  emit a name that has none. PRTG channel names are unchanged. Memory
  modules, adapters, ports and cache levels also become separate series on
  the pull sinks instead of overwriting each other.
- **Redfish: `collections` is validated and says what it turns off.** The
  list still replaces the default set, but an unknown name or an empty list
  now stops the probe at load with the accepted values, and the start-up
  log names the default subsystems the list disabled. A configuration that
  lists the six defaults is unchanged.
- **Redfish: names keep the BMC's own text.** `hw.name` and the other
  name attributes in OTLP, Prometheus and Zabbix are no longer stripped of
  `, ; ( ) [ ] { } < > | \ " ' ` # & ? =`. PRTG channel names and URL
  filters keep their cleaned form.

- **Linux host security signals in the `process` probe.** Three new
  machine-wide metrics on Linux: `senhub.system.kernel.open_files` (file
  handles allocated, from `/proc/sys/fs/file-nr`, to read against the
  existing `senhub.system.kernel.max_files`), `senhub.system.passwd.checksum`
  (CRC32 of `/etc/passwd` as a number that changes when the file changes)
  and `senhub.system.passwd.modified_timestamp` (its modification time).
  The generated Zabbix template for the probe carries a warning trigger
  that fires when the checksum changes, `change(...)<>0`. Re-run
  `zabbix setup` to refresh the templates. Other platforms emit nothing
  new.
## Before you upgrade

- **`SENHUB_AZURE_APP` is removed from the container image.** The image no longer reads `SENHUB_AZURE_APP` or the `SENHUB_AZURE_*` variables that went with it, and a container that still sets `SENHUB_AZURE_APP` on its first start **stops** with a message naming the replacement, rather than starting a collector that reads no log. A container that already has its configuration on a volume keeps running on the file the old variable wrote, until that file is removed.

  Declare the probe with the agent's own `SENHUB_PROBE_<NAME>_*` variables instead. The probe name is yours (letters and digits only); the application name goes in `APP`, so a name with a hyphen is a value, not a variable. The credentials, one value each before, are repeated for each probe.

  | Before | After |
  |---|---|
  | `SENHUB_AZURE_APP=oltp,billing` | one probe per application, `SENHUB_PROBE_OLTP_TYPE=azure_container_apps` and `SENHUB_PROBE_OLTP_APP=oltp`, then the same for `BILLING` |
  | `SENHUB_AZURE_TENANT_ID` | `SENHUB_PROBE_<NAME>_TENANT_ID` |
  | `SENHUB_AZURE_CLIENT_ID` | `SENHUB_PROBE_<NAME>_CLIENT_ID` |
  | `SENHUB_AZURE_CLIENT_SECRET` | `SENHUB_PROBE_<NAME>_CLIENT_SECRET`, or `SENHUB_PROBE_<NAME>_CLIENT_SECRET_FILE=/run/secrets/...` |
  | `SENHUB_AZURE_SUBSCRIPTION_ID` | `SENHUB_PROBE_<NAME>_SUBSCRIPTION_ID` |
  | `SENHUB_AZURE_RESOURCE_GROUP` | `SENHUB_PROBE_<NAME>_RESOURCE_GROUP` |
  | bookmark in the state directory, written for you | `SENHUB_PROBE_<NAME>_BOOKMARK_PATH=/var/lib/senhub-agent/<app>.bookmark`, one distinct file per probe |

  List mode, `SENHUB_AZURE_APP=oltp,billing` becomes:

  ```bash
  SENHUB_PROBE_OLTP_TYPE=azure_container_apps
  SENHUB_PROBE_OLTP_APP=oltp
  SENHUB_PROBE_OLTP_TENANT_ID=...
  SENHUB_PROBE_OLTP_CLIENT_ID=...
  SENHUB_PROBE_OLTP_CLIENT_SECRET_FILE=/run/secrets/aca_client_secret
  SENHUB_PROBE_OLTP_SUBSCRIPTION_ID=...
  SENHUB_PROBE_OLTP_RESOURCE_GROUP=rg-squash
  SENHUB_PROBE_OLTP_BOOKMARK_PATH=/var/lib/senhub-agent/oltp.bookmark
  # and the same eight lines with BILLING, APP=billing and billing.bookmark
  ```

  Discovery mode, which the old variable could not express (one probe for the whole subscription, no `APP` and no `RESOURCE_GROUP`):

  ```bash
  SENHUB_PROBE_ACA_TYPE=azure_container_apps
  SENHUB_PROBE_ACA_TENANT_ID=...
  SENHUB_PROBE_ACA_CLIENT_ID=...
  SENHUB_PROBE_ACA_CLIENT_SECRET_FILE=/run/secrets/aca_client_secret
  SENHUB_PROBE_ACA_SUBSCRIPTION_ID=...
  SENHUB_PROBE_ACA_DISCOVERY__INTERVAL=300
  SENHUB_PROBE_ACA_BOOKMARK_PATH=/var/lib/senhub-agent/aca.bookmark
  ```

  The old variable named the bookmark `<application>.bookmark` in the state directory: give `BOOKMARK_PATH` the same file and the probe resumes where it stopped instead of re-reading its recent lines. The old probe was named after the application; a name with a hyphen cannot be written in a variable, so such a probe changes name (`squash-tm` becomes, say, `squashtm`) and its series change `probe_name` with it. `SENHUB_PROBES` is unchanged.

- **Three entity identities change once.** The `unifi`, `kubernetes` and
  `systemd` probes no longer key their entities on an address or a hostname.
  A UniFi controller is identified by the UUID it reports about itself (or
  `unifi@<host.id>` when it runs on the agent's host); a controller that is
  remote and whose UUID the account cannot read has no entity. A Kubernetes
  cluster is identified by its `kube-system` namespace UID alone, so a
  cluster whose UID is unreadable has no cluster entity. A systemd unit is
  `systemd://<host.id>/<unit>`. The old entities are retired once: a
  consumer sees one disappearance, then the new identity.
- **Zabbix counter items become rates.** In the generated templates, every
  item built from a cumulative counter (network bytes and packets, disk
  I/O, CPU time, request totals) now carries the Change per second
  preprocessing step and a per-second unit (`Bps`, `/s`, `s/s`). The
  items keep their keys, so after you re-run `zabbix setup` and the
  templates are updated, the same item switches from a total to a rate:
  its history from before the upgrade holds totals and the new values are
  rates, so a graph spanning the upgrade shows a step. Rewrite the
  triggers and calculated items you wrote against these items to compare
  a rate.
## Features

- **Linux disk I/O.** The `logicaldisk` probe now reports, per whole block
  device, the bytes and operations read and written and the time spent on
  I/O (`system.disk.io`, `system.disk.operations`, `system.disk.io_time`),
  as cumulative counters, until now a Windows-only measurement. Partitions
  and `loop`, `ram`, `zram`, `fd` and `sr` devices are left out. They reach
  Prometheus, OTLP and Zabbix, which discovers one set of items per device.
- **Host clock.** The `cpu` probe reports the host's time
  (`senhub.system.time`, seconds since the Unix epoch) on Linux and Windows,
  so a monitoring server can check clock drift: `time() - senhub_system_time_seconds`
  in Prometheus, and in Zabbix a `fuzzytime()` trigger whose tolerance is
  `{$SENHUB.CLOCK.DRIFT.MAX}` (60 seconds by default).
## Breaking Changes

- **Exit codes of the command line are a contract.** `0` done, `1`
  warning, `2` failure, `3` unchanged. Failures that exited `1` now exit
  `2`, and `config check` exits `1` when it found warnings only (running
  without a licence is the free tier, reported as information) and `status` exits `1` when the service is stopped, the
  agent does not answer, it reports itself unhealthy or a probe is in
  error. A script that
  tests for any non-zero code is unaffected; one that compares with `1`
  must follow. The container image and the Windows installer are updated
  to match. See [Exit codes](../cli.md#exit-codes).

## Features

- **`doctor` diagnoses an install in one pass.** It aggregates the
  service state, the systemd unit and binary comparison, `config check`,
  the secret store, the licence expiry, the HTTP port, the connection
  test of every output, the probes' last cycle and the disk, prints a
  `fix` for each problem, and exits `1` on a warning and `2` on a
  failure. It needs no root and works with the service stopped.
  `--json` prints `senhub.cli.doctor/v1`. See [Doctor](../cli.md#doctor).
- **`--json` output.** `version`, `status`, `config check`, `config
  show`, `config set`, `config init` and `secret status` print one JSON
  object with a `senhub.cli.<command>/v1` schema identifier. Failures
  are JSON objects too. See [JSON output](../cli.md#json-output).
- **Idempotent provisioning commands.** `config init`, `config set` and
  `install` run again on a machine already in the requested state write
  nothing and exit `3`. `config init --ok-if-unchanged` exits `0` in that
  case, for installers that treat any other code as a failure. `install`
  also repairs a drifted unit or a disabled service in place (exit `0`, no
  restart) and takes `--json` with a `changed` field.
- **filetail reports where each tail stands.** For every file followed, `senhub.filetail.read_offset` and `senhub.filetail.file_size` (Prometheus `senhub_filetail_read_offset_bytes` and `senhub_filetail_file_size_bytes`, attribute `log.file.path`) let a rule detect a frozen tail: the file grew and the offset did not move.

## Fixes

- **A per-module debug level now writes debug lines.** Raising one module
  (for example `probe.ibmi`) to `debug` through the log-level API or the
  console answered `200` but wrote nothing, because the production logger
  kept a global `info` floor. The module now logs at debug while every other
  module stays at `info`. The setting is kept in memory: it survives a
  configuration reload and is lost on an agent restart (see
  [Troubleshooting](../troubleshooting.md)).
- **Oracle 23ai: login with a password longer than 30 characters.** The
  `oracle` probe could not log in to Oracle Database 23ai with a password of
  more than 30 characters: every cycle reported `senhub.db.up = 0` with
  `ORA-01017`, while SQL\*Plus accepted the same credentials. The driver does
  not announce long password support, which 23ai requires; the probe now does.
  Passwords of 30 characters or fewer were never affected.
- **Zabbix links the host probes of each platform by default.** `zabbix setup` linked five templates whatever the platform, so a Windows host collecting its services or its event log counter got no item for them. The probes marked `universal` in their definition (on Windows, `winservices` and `windows_eventlog` join the five) are linked per platform, `--all-probes` links every template, and a guard fails the build if a registered probe emits metrics and no template declares them (ent#109).
- **Zabbix templates ship graphs and dashboards.** The CPU, memory, logical disk and network templates carry graph prototypes (utilization, load, throughput, operations, queue, errors, per instance) declared in the probe definitions under `graphs:`, and a dashboard of their own graphs per template, listed under the host's Dashboards menu (7.0 export; the 6.0 export keeps the graphs only) (ent#110).
- **Windows services are discovered in Zabbix, with an "automatic service not running" trigger.** The `winservices` probe now reports each service's start type (`windows.service.start_type`); the template creates the state, status and start type of every service by discovery and raises a problem when a service set to start automatically has not been running for `{$SENHUB.WINSERVICES.GRACE}` (5 minutes). Discovery is filtered by `{$SENHUB.WINSERVICES.MATCHES}` and `{$SENHUB.WINSERVICES.NOT_MATCHES}`. The probe's own heartbeat is no longer declared once per service (ent#114).
- **Zabbix triggers cover more of a host.** Processor queue (Windows) and load (Linux), paging, disk queue and busy time, inodes, interface errors and discards, and a restart (new `system.uptime` from the `cpu` probe) join the usage and state triggers, each limit a `{$SENHUB.*}` macro with a documented default. Triggers are declared in the probe definitions under `triggers:` (ent#115).
