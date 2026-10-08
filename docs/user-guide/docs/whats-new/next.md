# 0.6.2 (unreleased)

0.6.2 adds Linux packages and signed package repositories, three ways to
deploy the agent at scale (an Ansible collection, a Helm chart and a Podman
Quadlet unit), probes declared from environment variables, and a manifest
for every release. It also changes the exit codes of the command line,
keeps failed log batches on disk by default, and removes
`SENHUB_AZURE_APP` from the container image.

<div class="rn-filter"></div>

## Before you upgrade

Actions for an installation running 0.6.1. Each one is detailed under
Breaking Changes or Changes below.

1. **Containers that set `SENHUB_AZURE_APP`.** The image no longer reads
   it and a first start with it set stops. Declare the probe with
   `SENHUB_PROBE_<NAME>_*` variables first.
2. **Scripts that compare an exit code with `1`.** Failures now exit `2`.
   A script that tests for any non-zero code is unaffected.
3. **Zabbix.** Re-run `zabbix setup` to refresh the templates: counter
   items become rates, and the host templates gain graphs, dashboards,
   triggers and discovery. Rewrite the triggers and calculated items you
   wrote against counter items.
4. **Topology backends.** The `unifi`, `kubernetes` and `systemd`
   entities are replaced once under a new identity.
5. **Disk use during a collector outage.** The OTLP output now keeps
   failed log batches on disk by default, up to 128 MiB in the state
   directory.

## Breaking Changes
- **The host's name is now one value everywhere.** A host whose `global_tags` set `host.name` already carried it on its metrics and logs, but its host entity (the node in the topology graph), the entities of its systemd units and the records the OTLP receiver stamps with the host kept the machine's own name. All of them now use the override, so on such a host the graph node's `host.name` changes once, to the value its logs already carry (a dashed `preprod-...-shop` becomes `preprod.sensorfactory.shop`). The `host.id` is unchanged, so the node is the same node. A host with no override keeps its name, except the `host.name` on its systemd unit entities, which was the raw OS name and is now the lower-case name the host entity carries. A record about another machine (a syslog sender, a relayed OTLP stream) keeps its own origin name; it is never replaced.

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

- **Exit codes of the command line are a contract.** `0` done, `1` warning, `2` failure, `3` unchanged. Failures that exited `1` now exit `2`. `config check` exits `1` when it found warnings only (running without a licence is the free tier, reported as information), and `status` exits `1` when the service is stopped, the agent does not answer, it reports itself unhealthy or a probe is in error. A script that tests for any non-zero code is unaffected; one that compares with `1` must follow. The container image and the Windows installer are updated to match. See [Exit codes](../cli.md#exit-codes).
- **Three entity identities change once.** The `unifi`, `kubernetes` and `systemd` probes no longer key their entities on an address or a hostname. A UniFi controller is identified by the UUID it reports about itself (or `unifi@<host.id>` when it runs on the agent's host); a controller that is remote and whose UUID the account cannot read has no entity. A Kubernetes cluster is identified by its `kube-system` namespace UID alone, so a cluster whose UID is unreadable has no cluster entity. A systemd unit is `systemd://<host.id>/<unit>`. The old entities are retired once: a consumer sees one disappearance, then the new identity.
- **Zabbix counter items become rates.** In the generated templates, every item built from a cumulative counter (network bytes and packets, disk I/O, CPU time, request totals) now carries the Change per second preprocessing step and a per-second unit (`Bps`, `/s`, `s/s`). The items keep their keys, so after you re-run `zabbix setup` and the templates are updated, the same item switches from a total to a rate: its history from before the upgrade holds totals and the new values are rates, so a graph spanning the upgrade shows a step. Rewrite the triggers and calculated items you wrote against these items to compare a rate.

## Features

### Packages and repositories

- **Linux packages.** `.deb` and `.rpm` packages for amd64 and arm64, in two editions (`senhub-agent-oss` and `senhub-agent`, which replace each other in one install command), install the agent as a systemd service from the distribution's package manager. They are tested on Debian 12, Ubuntu 22.04 and 24.04, Rocky Linux 9 and openSUSE Leap 15.6. See [Install from packages](../installation.md#install-from-packages).
- **Signed package repositories.** APT and YUM/DNF/Zypper repositories at `packages.senhub.io`, with a `stable` and a `beta` channel. See [Install from the package repositories](../installation.md#install-from-the-package-repositories).
- **A manifest per release, at a stable address.** `packages.senhub.io/releases/stable/latest.json` (and `beta/latest.json`) name the newest release of a channel; `releases/<version>/manifest.json` lists every file with its checksum, size, signature and the container images, so a script finds and verifies the right build without reading the GitHub page. A stable image is now also tagged with its minor line (`ghcr.io/senhub-io/senhub-agent:0.6`). See [Downloading and verifying releases](../releases.md).

### Deployment tools

- **Deploying at scale.** A documentation section with one page per tool, each with a runnable example and the same points: version pinning, licence, secrets, validation before apply, upgrade and removal. Pages for Intune, GPO and SCCM, cloud-init, Docker Compose, Helm and Podman, and an overview that maps each estate to a tool. See [Deploying at scale](../deploying/index.md).
- **Ansible collection.** `senhub.agent` is an Ansible collection with one role, `senhub.agent.agent`, that installs the agent from the signed package repositories (Linux) or the release MSI checked against its manifest (Windows), writes `probes.d` and `strategies.d` from variables, and runs `config check --json` on the result before it replaces the live files; a second run changes nothing. See [Deploying with Ansible](../ansible.md).
- **Helm chart.** `charts/senhub-agent` runs the agent in a Kubernetes cluster, by default one agent per node (a DaemonSet) with an identity of its own, monitoring the node it runs on and, on request, the cluster through the `kubernetes` probe. The chart is published to `oci://ghcr.io/senhub-io/charts`. See [Kubernetes (Helm)](../kubernetes-helm.md).
- **Podman and Quadlet.** A Quadlet unit runs the container image as a systemd service under Podman, monitoring the host, with the bearer token and the licence as Podman secrets. It follows the `0.6` line tag, so auto-update delivers patch releases. See [Running the agent with Podman](../podman.md).

### Configuration

- **Probes from environment variables.** `SENHUB_PROBE_<NAME>_TYPE=<type>` declares a probe and `SENHUB_PROBE_<NAME>_<PARAM>=value` sets its parameters, typed from the probe's schema, with `__` for nested keys and `_FILE` to read a secret from a file. The agent reads them itself, so containers, systemd units, Helm and Podman share one rule, and they adjust the probes of a mounted configuration too (the environment wins, key by key). A mistake stops the load and names the variable. `config show` lists the variables the probes were read from, and never prints a secret read from the environment. See [Configuring probes from environment variables](../configuration.md#configuring-probes-from-environment-variables).
- **Agent governance in the console.** The Settings page edits the governance of the host (owner, criticality, lifecycle, location, labels) with the same fields as a probe, and writes it to the top-level `governance:` block of `agent.yaml`; the running agent follows the change without a restart, and `config check` validates the block. A block set from `${env:}` or `${file:}` references is shown read-only. See [Settings](../web-interface.md#settings).
- **Probe SDK: the state directory.** `probesdk/state` gives a probe the directory where the agent keeps what must survive a restart (`state.Dir()`, `state.Path(name)`), so a probe that keeps a bookmark can default it beside the agent's identity.

### Command line and status

- **`doctor` diagnoses an install in one pass.** It aggregates the service state, the systemd unit and binary comparison, `config check`, the secret store, the licence expiry, the HTTP port, the connection test of every output, the probes' last cycle and the disk, prints a `fix` for each problem, and exits `1` on a warning and `2` on a failure. It needs no root and works with the service stopped. `--json` prints `senhub.cli.doctor/v1`. See [Doctor](../cli.md#doctor).
- **`--json` output.** `version`, `status`, `config check`, `config show`, `config set`, `config init` and `secret status` print one JSON object with a `senhub.cli.<command>/v1` schema identifier. Failures are JSON objects too. See [JSON output](../cli.md#json-output).
- **Idempotent provisioning commands.** `config init`, `config set` and `install` run again on a machine already in the requested state write nothing and exit `3`. `config init --ok-if-unchanged` exits `0` in that case, for installers that treat any other code as a failure. `install` also repairs a drifted unit or a disabled service in place (exit `0`, no restart) and takes `--json` with a `changed` field.
- **`status` works on every install.** The agent answers `senhub-agent status` on a local channel (a Unix socket in its state directory, readable by the service account and root only; a named pipe restricted to administrators on Windows), whether or not the HTTP output is enabled. `status` asks the local channel first and falls back to the HTTP output. The channel is read-only. It reports probe health and failed outputs but not the per-probe metric counts, which only the HTTP cache holds.
- **Two agents on one host no longer describe each other twice.** Each agent writes its own `service.instance.id` to `instance.id` in its state directory (`/var/lib/senhub-agent`, `C:\ProgramData\SenHub`), readable by other accounts only when the agent key is a random UUID, and by its owner alone otherwise. An agent that finds another agent's process listening on the host reuses that id instead of creating a second `service.instance`. When the file cannot be read, the previous behaviour applies.

### Probes

- **Linux disk I/O.** The `logicaldisk` probe reports, per whole block device, the bytes and operations read and written and the time spent on I/O (`system.disk.io`, `system.disk.operations`, `system.disk.io_time`), as cumulative counters, until now a Windows-only measurement. Partitions and `loop`, `ram`, `zram`, `fd` and `sr` devices are left out. They reach Prometheus, OTLP and Zabbix, which discovers one set of items per device.
- **Host clock.** The `cpu` probe reports the host's time (`senhub.system.time`, seconds since the Unix epoch) on Linux and Windows, so a monitoring server can check clock drift: `time() - senhub_system_time_seconds` in Prometheus, and in Zabbix a `fuzzytime()` trigger whose tolerance is `{$SENHUB.CLOCK.DRIFT.MAX}` (60 seconds by default).
- **Linux host security signals in the `process` probe.** `senhub.system.kernel.open_files` (file handles allocated, from `/proc/sys/fs/file-nr`, to read against `senhub.system.kernel.max_files`), `senhub.system.passwd.checksum` (CRC32 of `/etc/passwd`, a number that changes when the file changes) and `senhub.system.passwd.modified_timestamp` (its modification time). The generated Zabbix template carries a warning trigger on a checksum change; re-run `zabbix setup` to refresh it. Other platforms emit nothing new.
- **filetail reports where each tail stands.** For every file followed, `senhub.filetail.read_offset` and `senhub.filetail.file_size` (Prometheus `senhub_filetail_read_offset_bytes` and `senhub_filetail_file_size_bytes`, attribute `log.file.path`) let a rule detect a frozen tail: the file grew and the offset did not move.
- **Redis.** The probe reports the last RDB save, AOF rewrite and AOF write outcomes, the replication backlog (active, size, history), pub/sub channels and patterns, and, on a Sentinel, the status, replica count and sentinel count of each monitored master.
- **IPMI.** Sensors that share a name (a Dell lists every CPU temperature as `Temp`) are no longer merged into one series; they read `Temp (CPU 1)`, `Temp (CPU 2)`. Power (`hw.power`, watts) and current (`senhub.hardware.current`, amperes) readings are reported, and a power supply's presence and redundancy ("Presence detected", "Fully Redundant", "Redundancy Lost") give its status.
- **IBM i: QHST, QSYSOPR and audit events reach the logs backend.** The history log, message queue and audit journal collectors send each event as an OpenTelemetry log record (message text as body, the partition's own time as timestamp, IBM i severity 0-99 mapped to `INFO`, `WARN`, `ERROR`, `ERROR3` or `FATAL`, attributes under `ibmi.*`). `history_log_min_severity` is the volume lever.
- **Redfish: the whole probe reaches Prometheus and OTLP.** 100 of the 139 metrics the probe can emit had no definition and were dropped by those outputs (processors, memory modules, network, power, firmware, event-log counters, vendor storage readings). They are now declared with OTel names, units and types, and a test fails when the probe starts to emit a name that has none. PRTG channel names are unchanged. Memory modules, adapters, ports and cache levels also become separate series on the pull sinks instead of overwriting each other.

## Changes

- **The `event` probe is free.** It was listed with the Pro probes although its code ships in the open core and is registered in every build. It now runs without a licence, like `otlp_receiver`.
- **Failed log batches are kept on disk by default.** The OTLP output's on-disk queue for event logs the collector could not take used to run only when `persistence.path` was set. It is now on for every install (package, MSI, container), in the state directory (`otlp-queue/`), and replays at boot and when the collector answers again. During an outage the agent can use up to 128 MiB of disk there (`logs_queue_max_bytes`). New `logs_queue_max_age` (24 hours) drops older batches at boot and every 10 minutes, counted as `dropped_by_age`. `persistence.enabled: false` or `SENHUB_LOG_QUEUE=false` turns the queue off; `SENHUB_LOG_QUEUE_RETENTION` and `SENHUB_LOG_QUEUE_MAX_BYTES` tune it. An unwritable state directory logs one warning and the agent runs without the queue. In a container, mount a persistent volume on the state directory for the queue to outlive a restart. An explicit `persistence.path` keeps working unchanged. See [Logs survive an outage](../otlp.md#logs-survive-an-outage).
- **SNMP: a polled device's routes link to their gateway.** Routes read from a device's routing table reach the gateway's address with `next_hop_via`, as host routes already do, when the gateway is a public address, so a router's default route resolves to the same address node as a host's. Private (RFC1918, CGNAT, ULA), loopback, link-local and multicast gateways stay unlinked, because unrelated switches behind one private gateway would otherwise merge.
- **Redfish: `collections` is validated and says what it turns off.** The list still replaces the default set, but an unknown name or an empty list now stops the probe at load with the accepted values, and the start-up log names the default subsystems the list disabled. A configuration that lists the six defaults is unchanged.
- **Redfish: names keep the BMC's own text.** `hw.name` and the other name attributes in OTLP, Prometheus and Zabbix are no longer stripped of `, ; ( ) [ ] { } < > | \ " ' ` # & ? =`. PRTG channel names and URL filters keep their cleaned form.
- **Zabbix links the host probes of each platform by default.** `zabbix setup` linked five templates whatever the platform, so a Windows host collecting its services or its event log counter got no item for them. The probes marked `universal` in their definition (on Windows, `winservices` and `windows_eventlog` join the five) are linked per platform, `--all-probes` links every template, and a guard fails the build if a registered probe emits metrics and no template declares them.
- **Zabbix templates ship graphs and dashboards.** The CPU, memory, logical disk and network templates carry graph prototypes (utilization, load, throughput, operations, queue, errors, per instance) declared in the probe definitions under `graphs:`, and a dashboard of their own graphs per template, listed under the host's Dashboards menu (7.0 export; the 6.0 export keeps the graphs only).
- **Windows services are discovered in Zabbix, with an "automatic service not running" trigger.** The `winservices` probe reports each service's start type (`windows.service.start_type`); the template creates the state, status and start type of every service by discovery and raises a problem when a service set to start automatically has not been running for `{$SENHUB.WINSERVICES.GRACE}` (5 minutes). Discovery is filtered by `{$SENHUB.WINSERVICES.MATCHES}` and `{$SENHUB.WINSERVICES.NOT_MATCHES}`. The probe's own heartbeat is no longer declared once per service.
- **Zabbix triggers cover more of a host.** Processor queue (Windows) and load (Linux), paging, disk queue and busy time, inodes, interface errors and discards, and a restart (new `system.uptime` from the `cpu` probe) join the usage and state triggers, each limit a `{$SENHUB.*}` macro with a documented default. Triggers are declared in the probe definitions under `triggers:`.

## Fixes

- **Loopback connections on OS dynamic ports no longer create `depends_on` endpoints.** A host talking to itself through a port the OS picks per request (for example `127.0.0.1:497xx` on Windows) produced one one-shot endpoint per connection. A loopback peer whose port is in the OS dynamic range (49152-65535 on Windows and macOS, the kernel `ip_local_port_range` on Linux, 32768-60999 by default) is now ignored; loopback services on fixed ports and all non-loopback peers are unchanged.

- **A monolithic install gets its administration key on the first start, and a key added while the agent runs is served.** The key was minted before the monolithic `storage:` list was split into `strategies.d/`, so the http output came out without one; and the console routes were registered only at start, so a key added by a reload answered 404 until a restart. The split now runs first, a reload that adds the key rebuilds the routes, and `senhub-agent console` tells you to restart the service once when the running agent does not serve the console.

- **OTLP event logs are no longer lost when the collector is down and the agent restarts.** The on-disk queue handed a queued batch back to the in-memory log pipeline and deleted its file at once, without waiting for the collector's answer: with the collector down a batch went from disk to memory and back in milliseconds, and what was in memory at shutdown was lost (1575 of 3833 lines in a measured outage with a restart). A queued batch is now sent directly and its file is removed only after the collector acknowledged it; while the collector is down new batches go straight to disk and the agent probes it on a retry clock of 5 seconds doubling to 5 minutes; and a normal stop writes the records still held in memory to the queue. Delivery is at-least-once (a record can arrive twice, and replayed records can arrive after newer ones). See [Logs survive an outage](../otlp.md#logs-survive-an-outage).

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
- **Windows: host identity is no longer re-read on every collection.** The host information call, which walks every process on Windows, ran on each host-probe collection and each entity reconcile. It is now cached for 60 seconds, so a hostname change is still picked up within a minute (ent#578).
- **A `governance` change on a probe now reaches its entities on reload.** Editing only the `governance` block of a probe was skipped as "already running", so its entities kept the old owner, criticality and location until an agent restart. The running probe now takes the new block at the next configuration reload and the next entity emission carries it, without restarting the probe (ent#577).
- **A restart with the OTLP collector down no longer warns about the last metrics.** The stop logged a warning, "metric exporter shutdown: context deadline exceeded", for a final export the agent had no chance to deliver. When the last pushes already failed (or none was acknowledged for three intervals) the agent skips that export and logs one information line, "final metrics point not delivered: collector unreachable". A deadline or failure while the collector was healthy is still a warning.
- **`version`, the status API and the console show the open-core commit.** A build that compiles the open core from another checkout (the commercial edition) reported only its own commit, so the core source behind a binary could not be told. A new `Core commit` line in `senhub-agent version` (and `core_commit` in its JSON, `/info/system` and `status`) and a row in the console Agent card name it (ent#579).
- **`filetail` keeps the lines a writer appends to a rotated file, and its self-metrics survive a bad path.** After a rename-and-create rotation the probe left the old file at once, so lines the application still wrote to it until logrotate told it to reopen were lost. The probe now reads the old file to its end before switching, and keeps reading it for 30 seconds, each line once. A configured path that cannot be read no longer hides `senhub.filetail.records_emitted` and the offsets of the healthy files for the whole time the error lasts (ent#537).
- **Windows: an uninstall removes the install folder.** `C:\Program Files\SenHub Agent` stayed behind when it held a file the agent had written after the installer laid its own, typically the hidden previous executable a self-update sets aside. The uninstall now removes the folder, and the agent deletes that leftover at its next start. `%ProgramData%\SenHub` follows its own rule, unchanged. The folder is removed only when it recorded a path containing `SenHub`, so an install placed by hand in a shared directory does not take that directory with it (ent#125).

## Footprint

- **Less resident memory at rest.** The agent parsed all 85 embedded probe
  definitions and the seven console pages at start whatever was configured.
  A definition is now parsed when its probe type first needs it, and a
  console page when it is first opened. On the default configuration (four
  host probes, HTTP and Zabbix outputs) the anonymous resident memory fell
  from 17.5 to 11.8 MiB after half an hour. The rest of the resident size
  is the executable's own pages, shared with the page cache.
- **Five recurring lines moved from `INFO` to `DEBUG`.** Each fired on
  every cycle of a healthy agent: `kubernetes: event cycle complete`,
  `Successfully synced events` and `Server confirmed receipt of events`
  (event output), `No update required` and `Auto-update skipped: expected
  version is not newer than current` (auto-update). Start, stop,
  configuration change and error lines are unchanged. Run with `--verbose`
  or raise the module level to see them again.
- **Idle timers.** The probe start retry no longer ticks every two minutes
  while every probe is running; it arms a timer only while one is waiting
  to be retried. The agent's instance id file is no longer replaced when it
  already holds the id.
- **`config check` and the loader now agree on a configuration with no storage strategy.** `config check` reported "No storage strategies configured" as a warning (exit `1`) while the agent refuses to load such a file ("at least one storage strategy is required"). It is now an error (exit `2`), as is a storage entry with no name. A script that applies a configuration after `config check` (Ansible, the MSI) no longer lets one through that the agent then rejects.
- **A per-module debug level now writes debug lines.** Raising one module (for example `probe.ibmi`) to `debug` through the log-level API or the console answered `200` but wrote nothing, because the production logger kept a global `info` floor. The module now logs at debug while every other module stays at `info`. The setting is kept in memory: it survives a configuration reload and is lost on an agent restart (see [Troubleshooting](../troubleshooting.md)).
- **Oracle 23ai: login with a password longer than 30 characters.** The `oracle` probe could not log in to Oracle Database 23ai with a password of more than 30 characters: every cycle reported `senhub.db.up = 0` with `ORA-01017`, while SQL\*Plus accepted the same credentials. The driver does not announce long password support, which 23ai requires; the probe now does. Passwords of 30 characters or fewer were never affected.
- **IBM i: age gauges no longer read below zero.** The scheduled-job and other age gauges stay at zero when the partition clock runs ahead of the reference.
