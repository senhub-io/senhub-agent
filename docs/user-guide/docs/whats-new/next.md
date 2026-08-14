# Next (unreleased)

Three topology identities are re-keyed once on upgrade, overlay networks become
part of the topology, every entity type can finally be found in its own
telemetry, a Docker Swarm probe arrives, Kubernetes coverage triples and gains
cluster events, debug logging works for the first time, and the log file is
written for a person to read.


<div class="rn-filter"></div>



## Breaking changes

### Local database identities are re-keyed once on upgrade

A database with no stable server-reported id falls back to an identity built
from the address it answers on. On loopback that string is identical on every
machine, so two MariaDB instances on two different hosts arrived in the topology
as **one** entity: its version flipped between the two every few minutes, and
its telemetry join keys resolved to one host while its attributes described the
other.

The fallback is now scoped by the host:

| Before | After |
|---|---|
| `127.0.0.1:3306` | `mysql:3306@<host.id>` |
| `127.0.0.1:6379` | `redis:6379@<host.id>` |
| `localhost:27017` | `mongodb:27017@<host.id>` |

Databases reached over a **routable address keep their identity unchanged** —
the address already distinguishes them, and re-keying them would cost a
migration for no defect. The same is true of any instance you named yourself
with `instance_name`, and of PostgreSQL, which reports a stable system
identifier and never used the fallback.

**What a topology consumer sees.** The old entity is retired explicitly and an
alias edge links the new identity to it, so the change reads as a decision
rather than as an agent that went silent. Nothing is rewritten: the retired
entity keeps its history, and a time-travel read at the moment of the switch
shows both entities and the link between them. A current-state query will not
show the alias — its target is retired by then — which is expected, not a
failure.

Side effect worth knowing: until now a **local database had no host**. Its
identity contained the loopback address, which the anti-collapse guard refuses
to anchor, so the `runs_on` edge was never emitted. Scoped identities clear the
guard honestly, and local databases now appear in their host's impact radius
for the first time. (#740, #779)

### Kubernetes count metrics no longer carry a `_ratio` suffix

Thirty-nine Kubernetes metrics declared the dimensionless unit `1`. The
OTel-to-Prometheus naming rule reads that as a ratio and appends `_ratio`, so a
count of failed job pods reached Prometheus as `k8s_job_failed_ratio` and a
replica count as `k8s_statefulset_ready_ratio`. Both are plain integers.

| Before | After |
|---|---|
| `k8s_job_failed_ratio` | `k8s_job_failed` |
| `k8s_job_active_ratio` / `_succeeded_ratio` / `_desired_completions_ratio` | `k8s_job_active` / `_succeeded` / `_desired_completions` |
| `k8s_statefulset_ready_ratio` / `_desired_ratio` / `_current_ratio` / `_updated_ratio` | `k8s_statefulset_ready` / `_desired` / `_current` / `_updated` |
| `k8s_daemonset_ready_ratio` / `_desired_scheduled_ratio` / `_current_scheduled_ratio` / `_misscheduled_ratio` | `k8s_daemonset_*` without the suffix |
| `k8s_hpa_current_replicas_ratio` / `_desired_` / `_min_` / `_max_` | `k8s_hpa_*_replicas` |
| `k8s_node_ready_ratio`, `k8s_pod_ready_ratio`, `k8s_container_ready_ratio` | same names without `_ratio` |
| `k8s_resourcequota_hard_ratio` / `_used_ratio` | `k8s_resourcequota_hard` / `_used` |

Counts now declare a unit naming what is counted (`{pod}`, `{node}`, `{job}`,
`{cpu}`, `{resource}`) and one-hot state series declare `{state}`; annotation
units carry no Prometheus suffix. `senhub_kubernetes_up_ratio` is deliberately
unchanged — every probe's `up` metric declares unit `1`, and breaking that
alignment for one probe would trade a naming defect for a naming inconsistency
across forty others.

Dashboards and alerts querying the affected series need the suffix removed.


### Two more topology identities are re-keyed once on upgrade

Same family as the database re-key above, and the same one-time effect: the old
node is retired explicitly, a new one appears, and an alias edge links them so
both timelines stay joinable.

**`winservices` and `chrony`** pinned a constant as their identity —
`winservices://localhost` and `chrony://localhost`. Neither contains any host
component, so the value was byte-identical on every machine: every host running
the probe collapsed onto **one** node. Worse than a merge, because each host
still drew its own edge to itself, so that single node fanned out to the whole
fleet and joined the hosts transitively through it. The identity is now
`winservices@<host.id>`, following the contract for a local thing with no stable
id of its own.

**Monitored processes** were keyed on `{process.pid, process.creation.time}` —
unique on one machine, colliding the moment two hosts start a process with the
same pid at the same second, which is what a fleet booted from one image does.
The identity gains `host.id`.

Neither has any entity in our own production graph, so for us these are mines
defused rather than migrations. A fleet that runs `winservices` on Windows
estates will see the re-key.

Without a host id, neither probe now emits anything at all: the identity is
built from it, and inventing one is what produced the collapse. A visible gap
beats a node that is wrong on every machine.

## Changed

### The log file is written for a person to read

The agent's log file was JSON, and worse than dense: the secret-masking layer
re-encodes each entry through a map, and Go sorts map keys alphabetically — so
the timestamp landed at the **end** of every line and the level sat somewhere in
the middle. Nothing aligned from one line to the next.

```
{"count":116,"level":"info","message":"Successfully sent datapoints",
 "module":"data_store","strategy":"http","time":"2026-08-13T11:12:58+02:00"}
```

is now

```
2026-08-13 11:12:58.102 INF Successfully sent datapoints module=data_store count=116 strategy=http
```

Date first, then level, then the sentence, then the structured fields. Console
output carries the date too — a line pasted into a ticket without its date
cannot be correlated with anything. Timestamps gained real sub-second precision;
the previous format stored whole seconds and rendered a constant `.000`.

Secret masking is unaffected: the formatter sits between the masker and the
file, so the masker still redacts structured fields rather than
pattern-matching formatted text.

Set `--log-format json` (or `SENHUB_LOG_FORMAT=json`) to restore the
machine-parseable form for shipping the file to an aggregator. The remote log
shipper is unaffected and stays JSON. (#772)


### Telemetry ingested by `otlp_receiver` keeps the sending application's identity

An application pushing to the agent's OTLP receiver sends its own resource —
its `service.name`, its `host.name`. Until now the agent replaced that identity
with its own on the way out, so several applications behind one agent arrived
at the backend indistinguishable: a log sent with `service.name=my-app` was
stored under the agent's `service.name`, and an ingested metric carried two
different values for `service.name` in a single export — the agent's on the
resource, the application's on the datapoint — leaving the backend to silently
keep one.

The three signals now honour one contract on the OTLP output: the emitting
application's resource is forwarded as sent, and the agent's context (tenant,
site, environment, and the `telemetry.relay.*` identity that names which agent
relayed the data) is only ever **added** to keys the sender left unset. Traces
already worked this way; logs and metrics now match them.

**Nothing changes for PRTG, Nagios, Prometheus, the web UI or the SenHub
cloud.** Those sinks read tags, not an OTLP resource, and ingested telemetry
still reaches them exactly as before — the sending application's resource
attributes keep arriving as tags.

If a dashboard or query relies on ingested **logs** carrying the agent's
`service.name`, point it at the agent's own logs, or at
`telemetry.relay.instance.id`, which names the relaying agent without
overwriting the sender. (#765, #767)

## New

### Overlay networks are topology, not just labels

A Docker Swarm overlay is the reachability boundary: two workloads on the same
segment address each other by name, two on different segments cannot, whatever
the firewall says. Until now it rode as metric labels — queryable, not
traversable — so "can A reach B" meant reading several `docker network inspect`
outputs on the right node.

Overlays are now emitted as `network.segment` entities, declared by the cluster
that owns them and joined by the workloads on them:

```
service.instance --has_segment--> network.segment    (the manager declares)
container        --attached_to--> network.segment    (the node observes)
```

The manager sees which segments exist; the nodes see who is on them, so a
complete picture needs the `swarm` and `docker` probes both running. A
manager-only deployment reports segments with no attachments, which is a stable
state and not a defect.

Only swarm-scoped overlays produce an attachment. A `bridge` network is local to
one engine and identically named on every host, so a segment built from it would
be a node shared by the whole fleet.

Membership is a **necessary and not sufficient** condition for reachability:
network policies restrict on top of it, and the probe still measures attachment,
never traffic.

### Every entity type can now be found in its own telemetry

An entity you cannot pivot to telemetry is a picture, not a tool. Measured on a
real graph at the start of this cycle, that pivot worked for three entity types
out of ten.

It now works for all of them. Each type either stamps the identity it is keyed
on onto the datapoints that describe it — `db.instance.id` on database metrics,
`k8s.pod.uid` alongside the pod name, the VM GUID as `vmid`, the process pid
with its creation instant, `network.segment.id` on segment metrics — or is
explicitly declared as having no telemetry of its own, so a consumer stops
looking instead of guessing.

Identity labels are omitted while unresolved rather than emitted blank: an empty
label and the real one are two series for one subject.

### Containers and pods say what they are, not just what they are called

A host carries its full nameplate. A container carried four attributes and a pod
three — findable by name, described by almost nothing.

Containers now carry what their orchestrator states about them: the Swarm service
and task, the Compose project and service, and their creation time. Pods carry
the workload that owns them, read from the owner reference rather than parsed out
of the name, with a ReplicaSet reported as the Deployment above it — nobody
thinks in ReplicaSets.

Container labels are read through a whitelist, never copied wholesale: they are
arbitrary operator input, and passing them through would let anyone inflate the
graph with unbounded keys.


### Docker Swarm cluster probe

A `swarm` probe reports cluster state from a manager node: nodes and quorum,
service convergence, task lifecycle, and the overlay segments that decide what
can reach what. Free tier.

It answers what no single host can. **Quorum** — a swarm that has lost manager
quorum keeps every container running while silently refusing every change: no
deploy, no rescheduling, no scaling, and from inside one node nothing looks
wrong. **Convergence** — running replicas are counted from tasks, because Swarm
publishes no running count and a service can declare five replicas while five
tasks sit in `rejected`. **Overlays** — one series per (service, segment) pair
carrying the service's virtual IP, so "can A reach B" is a lookup rather than
three `docker network inspect` calls on the right node.

Traffic volume between services is deliberately **not** reported: the Engine API
exposes no per-peer counters, and per-container interface counters cannot be
attributed to a named overlay. The probe maps who can reach whom, not how much
flows. (#757)

### Kubernetes coverage raised to the k8sclusterreceiver standard

The `kubernetes` probe goes from 19 to 55 metric definitions, and gains two
things it never had.

**Cluster events on the log rail.** Kubernetes events are the only place the
cluster explains *why* a metric moved — "Failed to pull image", "0/5 nodes are
available: 5 Insufficient cpu", "Back-off restarting failed container". They
arrive as logs carrying the same join labels as the metrics, so a reader pivots
from "why did this pod restart" to that pod's series without parsing the
sentence.

**Pods and containers in the topology**, with the chain
`container → pod → host`, so a node going down takes its pods, which take their
containers, and an impact query answers transitively.

Storage, quotas and autoscaling are covered too: a claim stuck `Pending`, a
namespace at 99 % of quota and an autoscaler pinned at max are each the reason a
deployment will not scale, and none of them is visible from a workload's own
replica counts. (#756)

### A probe can be turned off without deleting it

`enabled: false` stops a probe and keeps its configuration:

```yaml
probes:
  - name: mysql-prod
    type: mysql
    enabled: false
    params:
      host: 127.0.0.1
```

Deleting the entry worked before and still does, but it takes the credentials,
intervals and tags with it. Omitting the key means enabled, so existing
configurations are unaffected. A disabled probe collects nothing and reports no
topology — it never appears as a monitored-but-broken target — and flipping the
flag on a config reload stops it without restarting the agent. `agent config
check` lists disabled probes explicitly. (#774)


### The OTLP pipeline reports what it relays

The relays had no success counter: an operator could watch
`senhub_agent_otlp_receiver_ingested_total` climb with no way to tell telemetry
the agent had forwarded from telemetry it had accepted and never sent. A field
report spent half a day on that ambiguity — the agent was relaying correctly the
whole time.

Three counters close it, on `/info/otlp`, in `senhub-agent status --otlp` and on
the Prometheus endpoint:

- `senhub_agent_otlp_spans_relayed_total`
- `senhub_agent_otlp_logs_relayed_total`
- `senhub_agent_otlp_metrics_relayed_total`

Each counts only what the collector accepted, never a refused export, and pairs
with the receiver's ingest counter for the same signal: equal totals mean
everything ingested left the agent.

The HTTP receiver's startup log also names every route it serves. It previously
logged only the metrics path even when the logs and traces handlers were mounted,
which read as "traces is not wired" at exactly the moment an operator checks
that. (#764)

### `snmp_poll` collects IPv6 routes

Route collection walked only `ipCidrRouteTable` (RFC 2096), which is IPv4-only,
so an IPv6 or dual-stack router surfaced none of its IPv6 routing table.

`snmp_poll` now also walks `inetCidrRouteTable` (RFC 4292), the
address-family-agnostic successor, and emits IPv6 destinations as
`network.route` entities exactly like IPv4 ones — canonical CIDR identity, host
bits zeroed, RFC 5952 form (`2001:db8:abcd::/48`, `::/0` for the default
route).

Devices implementing both tables list their IPv4 routes twice; the first entity
per destination wins and the IPv4 table is walked first, so IPv4 route
identities are unchanged. A device implementing only one of the two tables is
normal and no longer treated as a failure — collection fails only when neither
table answers, and the error then names both causes.

Zoned address families (`ipv4z`, `ipv6z`) are skipped: their destination is
only meaningful inside one scope and would collide with its unzoned twin.
(#716)

## Fixed

### Debug logging never produced what it advertised

Two independent defects, either one enough to make the feature useless, both
present since module logging was written.

`--filter <module>` produced **nothing at all, ever**. Selective mode pinned the
global log level to Info and then marked the chosen modules Debug — but the
global level is a hard floor, checked before the logger's own level, so the
filter's own selections were vetoed by the line above them. An operator asking
for one noisy subsystem got silence, which reads as "that code path logs
nothing".

`--verbose` reached **sixteen modules out of a hundred and fourteen**. The level
map was treated as an allowlist, so a module absent from it stayed mute even
with debug enabled globally. That map was frozen years ago, which means every
probe added since — `kubernetes` and `swarm` included — was invisible under full
verbose.

Measured on a real agent, forty-second runs: no flag, 0 debug lines; `--verbose`,
1355 lines across 12 modules; `--filter probe`, 15 lines from `probe.*` only,
without the 1238 `strategy.http` lines that drown the verbose output.

The runtime log-level endpoint is fixed as a side effect — raising one module to
debug on a running agent hit the same floor. (#772)


<ul class="rn">
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">Entities</span> <span class="tag t-area">Kubernetes</span> A cluster node and the agent running inside it produced <strong>two host entities instead of one</strong>. Kubernetes returns <code>/etc/machine-id</code> verbatim (32 hex characters) while the agent renders the same bytes as a hyphenated UUID — same machine, same file, two spellings, and a silent duplicate for every node of every cluster. (#762)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">Entities</span> <span class="tag t-area">Kubernetes</span> A pod waiting to be scheduled has no node, so it carried no relation and was dropped before reaching the backend — silently removing from the topology exactly the pod an operator is looking for. It now anchors to the cluster until it is placed. (#761)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">Entities</span> <span class="tag t-area">Network</span> Network interface metrics now carry the identity tag that joins them to their interface entity; the entity existed but nothing in the metrics pointed at it. (#748)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">Entities</span> <span class="tag t-area">Docker</span> Container metrics carried a shortened container id while the container entity is keyed on the full one, so the two could not be joined. (#758)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">OTLP</span> Relation attributes were built and then dropped before the wire, so an identity-alias edge arrived without the belief attributes that make a consumer act on it — the cost of sending it and none of the effect. (#779)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">Auto-update</span> Agents whose configuration predates 0.5.0 requested a doubled <code>/releases/releases/</code> path and got a 404, so auto-update was dead on them — and they could not fetch the fix, because fetching the fix is what was broken. The URL is normalized on load. (#747)</li>
</ul>


### `update` now refreshes the binary the service actually runs

On a hardened Linux install the agent is on disk twice: the CLI copy in
`PATH`, and the copy the systemd unit execs
(`/var/lib/senhub-agent/bin/senhub-agent`), which the unprivileged daemon owns
so it can replace it during auto-update. `sudo senhub-agent update <version>`
only ever replaced the copy it ran from, so the service kept running the old
release while the CLI reported the new one — and the closing "Restart the agent
to use the new version" made it look like the upgrade had landed.

`update` now reconciles both copies and names each file it wrote. Ownership of
the service copy is handed back to the unit's `User=`, so the daemon can still
self-update afterwards. Re-running the command is also the repair for a host
whose service copy already fell behind.

A service copy running a **newer** release than the one being installed is
reported and left untouched rather than downgraded — the daemon legitimately
runs ahead of the CLI.

`senhub-agent --version` reports the skew instead of hiding it:

```
Version: 0.5.3 (commit: a8e67f7)
Service binary: 0.5.4 (/var/lib/senhub-agent/bin/senhub-agent)
Note: the systemd service runs a different build than this CLI binary.
      'sudo senhub-agent update <version>' updates both copies.
```

The other binary's version is read from its build metadata, never by executing
it. Nothing changes for a single-copy install (legacy root unit, Windows, MSI):
both behaviours stay silent when there is only one binary. (#723)



### `refresh-unit` no longer disarms a `--user root` install

`senhub-agent install --user root` writes a unit that runs with full
privileges — which is the entire reason to choose it, for probes that need raw
ICMP sockets or a privileged port. Running `senhub-agent refresh-unit` on such a
host rewrote it to the hardened template with `User=root`, and the hardened
template drops every Linux capability. The service kept starting, so nothing
looked wrong; the active checks that needed those capabilities simply stopped
working.

A refresh on a root install now produces the same unit the install produced,
capabilities included. Root identity is expressed the way the installer
expresses it — by the absence of a `User=` directive, systemd's default being
root — instead of an explicit `User=root` on a capability-dropping unit.

Non-root installs are unchanged: the `senhub` user still gets the hardened unit
verbatim, and a custom service user still gets it re-templated. A refresh still
never switches a root install to the `senhub` user. (#689)

### `filetail` can read the system log files again on a hardened install

On Debian and Ubuntu, `/var/log/syslog` and `/var/log/auth.log` belong to
`syslog:adm` with mode `0640`, so a `filetail` probe pointed at them collected
nothing under the non-root unit — silently, with no error naming the cause. The
journal was never affected: `linux_logs` reads it through the `systemd-journal`
group the unit already grants.

`senhub-agent install`, `senhub-agent refresh-unit` and the `.deb` / `.rpm`
postinstall now join the service user to the `adm` group, which grants exactly
those log files and nothing else. Running `refresh-unit` is how an existing
install picks it up. Where the group does not exist, the join is skipped and the
install still succeeds.

The membership is granted through the user database rather than the unit's
`SupplementaryGroups=`, because a `SupplementaryGroups=` naming a group absent
from the distribution fails the unit at startup with `216/GROUP`.

The admin guide now documents the grant, and warns against the workaround it
replaces: `CAP_DAC_READ_SEARCH` does make the logs readable, but it bypasses
every file read permission check on the host — `/etc/shadow`, private keys and
any customer data included. On Red Hat systems `rsyslog` writes
`/var/log/messages` as `root:root 0600`, where `adm` does not help and
`linux_logs` is the answer. (#732)

### Hosts opted into betas now converge to the stable release

A host running `auto_update.include_beta: true` resolved `latest` to the newest
**beta** and stayed there — it never moved to the stable release that
superseded it. A recette host opted into betas silently stopped tracking
production.

Each channel is published with an alias record first, carrying the resolved
version (`{"latest", "0.5.3"}`, `{"latest-beta", "0.5.3-beta"}`), and the merge
of the two channels de-duplicates by version keeping the first record — so for
the newest release the alias record is usually the only one left. Version
selection then discarded records named `latest`, which made the newest stable
release invisible, while the beta alias, named `latest-beta`, escaped the same
filter and won.

Selection now looks at the version a record carries, never at the name of the
record. A beta genuinely ahead of the newest stable still wins, so opting into
betas keeps delivering them.

Stable hosts were never affected: they resolve `latest` through a different
path. (#730)

### No more registry warning on every HTTP push for the log conduit probes

`filetail`, `linux_logs`, `windows_eventlog` and `snmp_trap` publish their
records straight to the log rail; the only datapoints they hand to the pull cache
are their own throughput and health counters. Those probe types were not declared
in the cache's discriminant-tag registry, so every HTTP-strategy push logged:

```
Probe type not in DiscriminantTagsRegistry - using no discriminant tags
  metric_name=senhub.filetail.records_emitted probe_type=filetail
```

The four types are now declared with an empty discriminant set, which is the
correct shape rather than a gap: these counters carry no per-instance tag, and
the probe name is already part of every cache key — so two `filetail` probes
have always produced two distinct series, and still do. Only the log noise
changes. (#724)


## Security

<ul class="rn">
<li><span class="tag t-security">Security</span> <span class="tag t-area">Dependencies</span> Built on Go <strong>1.26.6</strong>, which clears seven vulnerabilities in the standard library, all with reachable call traces from this agent: quadratic complexity in <code>net/url</code>, an unbounded count of post-handshake TLS messages, <code>ReadHeaderTimeout</code> not applied on the unencrypted HTTP/2 check, and missing recursion guards in <code>encoding/xml</code> and <code>encoding/asn1</code>. <code>govulncheck</code> reports no known reachable vulnerabilities in this release.</li>
</ul>

## Known follow-ups

- The `swarm` and `docker` probes reach the Docker Engine over a Unix socket and
  have no named-pipe support, so neither works against Docker on Windows.
- Default probe configuration still covers four host probes; everything else on
  a machine is collected by nobody until someone writes YAML. (#777)
- A probe can be disabled but not started or stopped at runtime — that needs a
  restart or a config reload. (#775)

- Hosts running `auto_update.include_beta: true` resolve `latest` to the newest
  beta and never move to the stable release that supersedes it. Stable hosts are
  unaffected. (#730)
- Relay enrichment is configured under `signals.traces.relay_enrichment`, but now
  governs relayed logs and metrics too; disabling it on the traces signal
  silently disables it for all three. The setting will move to a relay-level
  block, with the current key kept as a deprecated alias. (#766)

