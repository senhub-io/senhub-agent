# Next (unreleased)

<div class="rn-filter"></div>


## Changed

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


## Known follow-ups

- Hosts running `auto_update.include_beta: true` resolve `latest` to the newest
  beta and never move to the stable release that supersedes it. Stable hosts are
  unaffected. (#730)
- Relay enrichment is configured under `signals.traces.relay_enrichment`, but now
  governs relayed logs and metrics too; disabling it on the traces signal
  silently disables it for all three. The setting will move to a relay-level
  block, with the current key kept as a deprecated alias. (#766)

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
