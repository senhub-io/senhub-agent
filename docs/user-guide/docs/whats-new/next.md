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
