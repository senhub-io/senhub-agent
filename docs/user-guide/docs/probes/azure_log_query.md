<img src="../../assets/probe-logos/azure_log_query.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The Azure Log Query probe runs one **KQL query** on one **Azure Log Analytics workspace** at each cycle and publishes every row of the result as a gauge metric. It turns any question you can ask a workspace (a count of failed sign-ins, the free space of a disk reported by VM Insights, the backlog of an application's own log table) into a metric, without installing anything next to the data.

Authentication is an Entra ID app registration (client credentials). One probe instance runs one query on one workspace; declare several instances for several queries.

**Collected data:**

- One gauge per result row, named after the row's `channel`
- `senhub.azure_log_query.rows`: the number of rows the last query returned

# Quick Start

```yaml
# probes.d/42-azure-log-query.yaml — each file under probes.d/ is a YAML array of probes
- name: failed-signins
  type: azure_log_query
  params:
    tenant_id: "00000000-0000-0000-0000-000000000000"
    client_id: "11111111-1111-1111-1111-111111111111"
    client_secret: "${secret:logq.client_secret}"   # OS secret store; inline plaintext is auto-sealed on install
    workspace_id: "33333333-3333-3333-3333-333333333333"
    timespan: PT1H
    interval: 300
    query: |
      SigninLogs
      | where ResultType != 0
      | summarize value = count() by AppDisplayName
      | project channel = strcat("Failed sign-ins ", AppDisplayName),
                value = todouble(value),
                unit = "{count}"
```

# The query contract

The result of the query must contain three columns, whatever the rest of the query does. Column names are matched without regard to case.

| Column | Content |
|---|---|
| `channel` | The name of the series. It becomes part of the metric name |
| `value` | A number. A number held in a string is accepted; a row whose value is not numeric is skipped and counted in the log |
| `unit` | The unit of the value (see [Units](#units)); empty means `1` |

If a column is missing, the probe publishes nothing for that cycle and logs once which columns were expected and which were found. When the result has several tables, `PrimaryResult` is used, otherwise the first.

Write the query so that it returns a bounded number of rows. At most `max_rows` rows (200 by default) are processed per cycle; the rest are ignored and counted in the log. The query time span is set by `timespan`, not by the query.

# Naming and normalisation

The metric name is `azure.log_query.<channel>`, where the channel is normalised: lower-cased, accents folded to ASCII (`é` becomes `e`), every character that is not a letter, a digit or a dot replaced by `_`, runs of `_` and of `.` collapsed, separators at the edges removed, and the result cut at 100 characters. A row whose channel is empty once normalised is skipped. The name is never bare: it always carries the `azure.log_query.` prefix.

| `channel` returned | Metric |
|---|---|
| `Disk C: Free Space` | `azure.log_query.disk_c_free_space` |
| `Orders.Pending (Prod)` | `azure.log_query.orders.pending_prod` |

The channel as returned, before normalisation, is kept in the attribute `azure.log_query.channel` (cut at 128 characters). When two rows of one cycle give the same name and the same attributes, the last one wins.

## Units

The `unit` column should be a UCUM unit: `1`, `%`, `By`, `bit`, `s`, `ms`, `us`, `ns`, `min`, `h`, `d`, `Cel`, `Hz`, `By/s`, `bit/s`, the usual prefixed byte units (`kBy`, `MBy`, `GBy`, `TBy`, `KiBy`, `MiBy`, `GiBy`), or an annotation in braces such as `{request}`. A few common words are translated: `Percent` to `%`, `Bytes` to `By`, `Seconds` to `s`, `MilliSeconds` to `ms`, `Count` to `{count}`, `BytesPerSecond` to `By/s`, `Cores` to `{core}`, `Unspecified` and `None` to `1`. Any other unit is replaced by `1` and logged once per cycle with an example.

# Requirements

- An **Entra ID app registration** with a **client secret**.
- The role **`Log Analytics Reader`** assigned to the app registration on the Log Analytics workspace (or on its resource group or subscription).
- Outbound HTTPS from the agent host to `login.microsoftonline.com` and `api.loganalytics.io`.

## Shared credentials

To avoid repeating the identity of the app registration in every entry, set it once in the agent's environment. It serves every Azure probe. A probe that sets a key itself keeps its own value for that key.

| Variable | Replaces |
|---|---|
| `SENHUB_AZURE_TENANT_ID` | `tenant_id` |
| `SENHUB_AZURE_CLIENT_ID` | `client_id` |
| `SENHUB_AZURE_CLIENT_SECRET` | `client_secret` |
| `SENHUB_AZURE_CLIENT_SECRET_FILE` | path of a file holding the secret (a mounted secret, for example) |

The secret is taken from the first of these that is set: `client_secret`, `client_secret_file`, `SENHUB_AZURE_CLIENT_SECRET`, `SENHUB_AZURE_CLIENT_SECRET_FILE`. A secret file that cannot be read is an error, not a fallback. The variables are read when the probe is configured, so changing them needs a restart. This probe needs no subscription.

# Configuration Parameters

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `tenant_id` | In practice | - | Entra ID tenant (directory) ID; required unless `SENHUB_AZURE_TENANT_ID` is set |
| `client_id` | In practice | - | Application (client) ID of the app registration; required unless `SENHUB_AZURE_CLIENT_ID` is set |
| `client_secret` | In practice | - | Client secret of the app registration; one of `client_secret`, `client_secret_file`, `SENHUB_AZURE_CLIENT_SECRET` or `SENHUB_AZURE_CLIENT_SECRET_FILE` is required, in that order. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` |
| `client_secret_file` | No | - | File holding the client secret; used when `client_secret` is not set |
| `workspace_id` | Yes | - | Log Analytics workspace ID (1 to 64 letters, digits or hyphens) to query |
| `query` | Yes | - | KQL query to run; its result must contain `channel`, `value` and `unit` columns |
| `timespan` | No | `PT1H` | ISO 8601 duration of the query time span (`PT15M`, `PT1H`, `P1D`) |
| `interval` | No | `300` | Seconds between two query cycles, at least 10 |
| `max_rows` | No | `200` | Maximum rows processed per cycle, 1 to 10000 |
| `attribute_columns` | No | none | Extra result columns published as attributes `azure.log_query.attribute.<column>` |
| `max_attribute_columns` | No | `5` | Cap on the extra attribute columns actually published, 0 to 20 |
| `authority_host` | No | `login.microsoftonline.com` | Entra ID authority, for sovereign clouds |
| `log_analytics_host` | No | `api.loganalytics.io` | Log Analytics query endpoint, for sovereign clouds |

Extra columns are not published unless named in `attribute_columns`: each distinct value of an attribute is a distinct series, so name only columns of bounded cardinality. A value is cut at 128 characters, and the column name is normalised like a channel.

# Metrics Collected

| Metric | Unit | Attributes | Description |
|---|---|---|---|
| `azure.log_query.<channel>` | from `unit` | `azure.tenant.id`, `cloud.provider`, `azure.log_analytics.workspace.id`, `azure.log_query.channel`, and the chosen `azure.log_query.attribute.<column>` | One gauge per result row |
| `senhub.azure_log_query.rows` | `{row}` | - | Rows the last query returned, before the `max_rows` cap. `0` when the query returned nothing, in which case no other series is published |

# Troubleshooting

| Symptom | Cause |
|---|---|
| Log: `the application needs Log Analytics Reader on the workspace <id>` | Azure answered 403. Assign `Log Analytics Reader` to the app registration on the workspace |
| Log: `result table is missing the column(s) ...` | The query does not return `channel`, `value` and `unit`; the message lists the columns found |
| Log: `query returned no row` | The query ran and matched nothing in the time span; only `senhub.azure_log_query.rows` (0) is published |
| Log: `non numeric rows skipped` | A `value` cell is not a number; cast it in the query with `todouble()` |
| Log: `invalid units replaced by 1` | A `unit` is not a UCUM unit or one of the accepted words |
| Log: `rows beyond max_rows ignored` | Make the query return fewer rows, or raise `max_rows` |

Each condition is logged once when it starts and once when it ends, not at every cycle. A role assigned a few minutes ago can take that long to be honoured by Azure.

# Outputs

The metrics are available through every configured output. For PRTG and Nagios, query the probe by its configured `name`:

```bash
curl "http://localhost:8080/api/{agentkey}/prtg/metrics/failed-signins"
curl "http://localhost:8080/api/{agentkey}/nagios/metrics/failed-signins"
```
