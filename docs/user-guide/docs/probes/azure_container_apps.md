<img src="https://cdn.simpleicons.org/microsoftazure" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The Azure Container Apps probe reads the console log stream of an application hosted on Azure Container Apps: the standard output and standard error of every container of every replica, as they are written. It is the same stream `az containerapp logs show --follow` reads, obtained through Azure Resource Manager with an Entra ID app registration. Nothing changes in the container: no volume, no sidecar, no logging library.

Lines ride the agent's log rail exactly like lines read by `filetail`: the same parsers (raw, regex, json, logfmt), the same multiline folding for stack traces, and the same outputs (OTLP logs first). Each record carries the application, revision, replica and container it came from, so one stream never blends into another.

One probe instance follows one application; add an instance per application. Replicas that appear with a scale-out or a new revision are attached on the next scan; replicas that disappear are released.

**Collected data:**

- Every stdout and stderr line of every replica and container of the application, as log records
- The probe's own state as metrics: control-plane reachability, replicas seen, streams attached, records emitted, streams re-attached after a drop

# Quick Start

```yaml
# probes.d/40-azure-container-apps.yaml — each file under probes.d/ is a YAML array of probes
- name: squash-logs
  type: azure_container_apps
  params:
    tenant_id: "00000000-0000-0000-0000-000000000000"
    client_id: "11111111-1111-1111-1111-111111111111"
    client_secret: "${secret:squash-logs.client_secret}"   # OS secret store; inline plaintext is auto-sealed on install
    subscription_id: "22222222-2222-2222-2222-222222222222"
    resource_group: rg-squash
    app: squash-tm
    parser:
      type: raw
    multiline:
      pattern: '^\d{4}-\d{2}-\d{2}'     # a line that starts with a date starts a record
  governance:
    criticality: high
    labels:
      application: squash-tm
```

The `governance` block is the agent's per-probe governance (see [Configuration](../configuration.md#governance-per-probe)); its label `application` is stamped on every log record and on the application entity, which is how a consumer finds everything of one application chain.

# Configuration Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `tenant_id` | string | Yes | - | Entra ID tenant (directory) ID |
| `client_id` | string | Yes | - | Application (client) ID of the app registration |
| `client_secret` | string | Yes | - | App registration client secret. Reference a stored secret via `${secret:<name>.client_secret}`, `${env:VAR}` or `${file:/path}`; inline plaintext is auto-sealed into the OS secret store on install. |
| `subscription_id` | string | Yes | - | Subscription that holds the Container App |
| `resource_group` | string | Yes | - | Resource group of the Container App |
| `app` | string | Yes | - | Name of the Container App |
| `containers` | list | No | all | Container names to read; the others in each replica are ignored |
| `tail_lines` | integer | No | `100` | Lines re-read when a stream is (re)attached; the probe drops the ones it already published |
| `interval` | integer | No | `60` | Seconds between replica scans, and the cadence of the state metrics |
| `parser` | block | No | `type: raw` | Line parser: `type` (`raw`, `regex`, `json`, `logfmt`), `pattern` (regex with named groups), `timestamp_field`, `timestamp_format`. Same block as [File Tail](filetail.md). |
| `multiline` | block | No | off | Folding of physical lines into one record: `pattern`, `negate`, `match` (`after` or `before`). Same block as [File Tail](filetail.md). |
| `max_bytes_per_line` | integer | No | `1048576` | Cap on one assembled record |
| `authority_host` | string | No | `login.microsoftonline.com` | Entra ID authority, for sovereign clouds |
| `management_host` | string | No | `management.azure.com` | Azure Resource Manager endpoint, for sovereign clouds |

# Log Records

Each record's body is the line (or the folded multiline record). Attributes:

| Attribute | Value |
|-----------|-------|
| `cloud.provider` | `azure` |
| `cloud.region` | The application's Azure region |
| `cloud.resource_id` | The ARM resource ID of the application |
| `service.name` | The application name |
| `senhub.azure_container_apps.revision` | The revision the line came from |
| `senhub.azure_container_apps.replica` | The replica the line came from |
| `container.name` | The container the line came from |
| `log.iostream` | `stdout` or `stderr` when the stream tells them apart |

Fields lifted by the `json`, `regex` and `logfmt` parsers are added as attributes; a `message`, `level` or configured timestamp field is promoted the same way as in `filetail`.

# Metrics Collected

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.azure_container_apps.up` | `{status}` | `1` when Azure Resource Manager answered the last replica scan, else `0` |
| `senhub.azure_container_apps.replicas` | `{replica}` | Replicas of the active revisions seen at the last scan |
| `senhub.azure_container_apps.streams.open` | `{stream}` | Streams currently attached, one per replica and container |
| `senhub.azure_container_apps.records_emitted` | `{record}` | Cumulative records published to the log rail |
| `senhub.azure_container_apps.stream.reconnects` | `{reconnect}` | Cumulative streams re-attached after a drop |

# Requirements

- An **Entra ID app registration** with a **client secret**.
- A role assignment on the Container App (or its resource group) that includes `Microsoft.App/containerApps/read`, `Microsoft.App/containerApps/revisions/read`, `Microsoft.App/containerApps/revisions/replicas/read` and the action `Microsoft.App/containerApps/getAuthtoken/action`. **Reader is not enough**: it lacks the last one. Contributor covers all four; a custom role with those four is the least privilege.
- Outbound HTTPS from the agent host to `login.microsoftonline.com`, `management.azure.com` and the log stream endpoint of the application's environment (`*.azurecontainerapps.dev`).

!!! note "A live stream, not an archive"
    The stream carries lines as they are written. While the agent is stopped, lines are not kept for it: on restart the probe re-reads the last `tail_lines` of each container and resumes. An application that needs every line kept keeps Azure's own Log Analytics as well; this probe is the low-latency path.

# Outputs

Log records are delivered to the outputs that consume logs, OTLP first; restrict them with the per-probe `log_strategies` key if needed. The state metrics are available through every configured output; for PRTG and Nagios, query the probe by its configured `name`:

```bash
curl "http://localhost:8080/api/{agentkey}/prtg/metrics/squash-logs"
curl "http://localhost:8080/api/{agentkey}/nagios/metrics/squash-logs"
```
