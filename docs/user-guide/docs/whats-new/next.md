# Next — 0.6.0 (unreleased)

:material-progress-clock: In progress — changes for the next stable release will be listed here.

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
