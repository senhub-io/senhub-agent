# Next — 0.5.3 (unreleased)

:material-progress-clock: In progress — OTLP receiver handles logs and traces, file-based license, cleaner Windows uninstall, Redis mTLS.

<div class="rn-filter"></div>


## Breaking changes

### `ad_hybrid` and `exchange_online` metric names normalized to the `senhub.*` namespace

The `ad_hybrid` (Azure AD Connect Health) and `exchange_online` probes emitted
their metrics under an inconsistent namespace — only the `up` metric carried the
`senhub.` prefix, every other metric dropped it. All metrics from these two
probes now follow the OTel-first convention used by every other probe: a
`senhub.<probe>.*` name declared through the probe's transformer.

**Who is affected:** dashboards or alerts that query these probes **over native
OTLP**. Prometheus consumers are mostly unaffected (the exporter already
prefixed `senhub_` to the un-namespaced names) except for the two rows marked
below.

**ad_hybrid** (OTLP metric name):

| Before | After |
|---|---|
| `ad_hybrid.sync.health` | `senhub.ad_hybrid.sync.health` |
| `ad_hybrid.sync.agents.healthy` | `senhub.ad_hybrid.sync.agents.healthy` |
| `ad_hybrid.sync.agents.total` | `senhub.ad_hybrid.sync.agents.total` |
| `ad_hybrid.sync.export_errors` | `senhub.ad_hybrid.sync.export_errors` |
| `ad_hybrid.agent.last_seen` | `senhub.ad_hybrid.agent.last_seen` |

(`senhub.ad_hybrid.up` is unchanged.)

**exchange_online** (OTLP metric name):

| Before | After |
|---|---|
| `exchange_online.service.health` | `senhub.exchange_online.service.health` |
| `exchange_online.mail.sent` / `.received` / `.delivered` / `.failed` | `senhub.exchange_online.mail.*` |
| `exchange_online.mailbox.count` | `senhub.exchange_online.mailboxes` |
| `exchange_online.mailbox.active` | `senhub.exchange_online.mailboxes.active` |
| `exchange_online.mailbox.storage.used` | `senhub.exchange_online.mailbox.storage.used` (unit `By`) |
| `exchange_online.mailbox.quota.exceeded` | `senhub.exchange_online.mailbox.quota_exceeded` |

(`senhub.exchange_online.up` is unchanged.)

**Prometheus-specific changes** (the only two visible in Prometheus output):

| Before | After |
|---|---|
| `senhub_exchange_online_mailbox_count` | `senhub_exchange_online_mailboxes` |
| `senhub_exchange_online_mailbox_storage_used` | `senhub_exchange_online_mailbox_storage_used_bytes` |

The mail-flow counters are now correctly typed as counters, and mailbox storage
now declares the `By` unit.

### `hyperv_ha`, `mssql_ha`, `oracle_enterprise` and `vsphere_ha` metric names normalized

The same normalization was applied to the four high-availability probes — they had
the identical inconsistency (only `up` was namespaced) and no transformer. All
their metrics now live under `senhub.<probe>.*`, with declared units and types.

**hyperv_ha** (OTLP): `hyperv.replica.health` / `.state` / `.lag`,
`hyperv.cluster.node.state`, `hyperv.cluster.group.state` →
`senhub.hyperv_ha.replica.*` / `senhub.hyperv_ha.cluster.*`.

**mssql_ha** (OTLP): `sqlserver.ag.replica.role` / `.health` / `.connected`,
`sqlserver.ag.database.lag`, `sqlserver.ag.log_send_queue` / `redo_queue` /
`log_send_rate` / `redo_rate` → `senhub.mssql_ha.*`.

**oracle_enterprise** (OTLP **and** Prometheus): `oracle.awr.*`, `oracle.ash.*`,
`oracle.rac.*`, `oracle.dataguard.*` → `senhub.oracle_enterprise.*`. This also
fixes a Prometheus namespace collision with the Free `oracle` probe
(`senhub_oracle_awr_db_time` → `senhub_oracle_enterprise_awr_db_time`).
`oracle.rac.instance.count` → `senhub.oracle_enterprise.rac.instances`; the two
RAC metrics are now typed as counters.

**vsphere_ha** (OTLP): `vsphere.vsan.*` and `vsphere.nsx.*` →
`senhub.vsphere_ha.vsan.*` / `senhub.vsphere_ha.nsx.*`. The vSAN object
health/degraded pair is collapsed to `senhub.vsphere_ha.vsan.objects` with an
`object.state` attribute.

(In every case the pre-existing `senhub.<probe>.up` metric is unchanged.)


## New

<ul class="rn">
<li><span class="tag t-new">New</span> <span class="tag t-area">OTLP</span> The <strong>OTLP receiver</strong> probe now accepts <strong>logs and traces</strong>, not just metrics. A <code>signals</code> list (<code>metrics</code>, <code>logs</code>, <code>traces</code>) chooses what it ingests; logs and traces are relayed onward through a configured OTLP export strategy. This turns the agent into a single OTLP intake for every signal type. (#655)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">License</span> The license now lives in its own <code>license.jwt</code> file next to <code>agent.yaml</code>. Hand a customer a single file to drop in place, then restart — no pasting a long token into YAML. <code>license activate</code> writes the file for you, and an existing inline license is moved into it automatically on the next start. (#639)</li>
</ul>


## Improved

<ul class="rn">
<li><span class="tag t-improved">Improved</span> <span class="tag t-area">OTLP</span> Explicit-bucket histograms pushed to the OTLP receiver are now carried <strong>natively</strong> end to end — re-exported over OTLP as a real histogram (buckets, sum, count, min/max) and rendered as a classic histogram on the Prometheus endpoint. (#659)</li>
<li><span class="tag t-improved">Improved</span> <span class="tag t-area">Windows</span> <span class="tag t-area">MSI</span> A plain uninstall now leaves a clean tree: the transient <code>logs\</code> and <code>update\</code> folders are removed, while configuration, the sealed secret store and the license are preserved. A full purge — including config and secrets — stays opt-in with <code>PURGE_DATA=1</code>. (#648)</li>
<li><span class="tag t-improved">Improved</span> <span class="tag t-area">Observability</span> The <code>collect_errors_total</code> self-metric is now broken down by <code>probe</code> and <code>reason</code>, so collection failures are attributable per probe and per cause. (#646)</li>
<li><span class="tag t-improved">Improved</span> <span class="tag t-area">Windows</span> Filesystem metrics now carry <code>system_filesystem_type</code> and <code>system_device</code> attributes, and the host name is a consistent lower-cased FQDN. (#627)</li>
</ul>


## Fixed

<ul class="rn">
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">Windows</span> <span class="tag t-area">Network</span> On multi-NIC Windows hosts, network adapters are now matched exactly instead of by a loose substring, and an adapter that matches no performance-counter instance still emits its counters rather than being dropped. (#643, #644)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">systemd</span> The <code>systemd</code> probe now emits its unit as a topology entity — the entity was previously built but never registered, so it never reached the backend. (#471)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">systemd</span> <span class="tag t-area">CLI</span> <code>refresh-unit</code> now reconciles the service <code>ExecStart</code>, so a CLI-installed agent keeps its correct start command after a refresh. (#396)</li>
</ul>


## Security

<ul class="rn">
<li><span class="tag t-security">Security</span> <span class="tag t-area">Redis</span> The Redis probe now supports <strong>TLS client-certificate authentication (mTLS)</strong> via <code>tls_cert_file</code> / <code>tls_key_file</code>, plus a custom CA bundle with <code>tls_ca_file</code>. (#405)</li>
</ul>
