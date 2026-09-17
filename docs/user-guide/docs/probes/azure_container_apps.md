<img src="../../assets/probe-logos/azure_container_apps.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The Azure Container Apps probe reads the console log stream of an application hosted on Azure Container Apps: the standard output and standard error of every container of every replica, as they are written. It is the same stream `az containerapp logs show --follow` reads, obtained through Azure Resource Manager with an Entra ID app registration. Nothing changes in the container: no volume, no sidecar, no logging library.

Lines ride the agent's log rail exactly like lines read by `filetail`: the same parsers (raw, regex, json, logfmt), the same multiline folding for stack traces, and the same outputs (OTLP logs first). Each record carries the application, revision, replica and container it came from, so one stream never blends into another.

One probe instance follows one application; add an instance per application. Replicas that appear with a scale-out or a new revision are attached on the next scan; replicas that disappear are released.

**Collected data:**

- Every stdout and stderr line of every replica and container of the application, as log records
- The probe's own state as metrics: control-plane reachability, replicas seen, streams attached, records emitted, streams re-attached after a drop, and the reads left in the subscription's Azure Resource Manager budget

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
    bookmark_path: /var/lib/senhub-agent/squash-logs.bookmark
    min_severity: warn                 # INFO and below never leave the agent
    exclude:
      - 'GET /health '                 # the platform's own probes
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

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `tenant_id` | Yes | - | Entra ID tenant (directory) ID |
| `client_id` | Yes | - | Application (client) ID of the app registration |
| `client_secret` | Yes | - | Client secret of the app registration. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `subscription_id` | Yes | - | Subscription that holds the Container App |
| `resource_group` | Yes | - | Resource group of the Container App |
| `app` | Yes | - | Name of the Container App |
| `containers` | No | - | Container names to read; empty reads every container |
| `tail_lines` | No | `100` | Lines re-read when a stream is (re)attached, 0 to 300; already published lines are dropped |
| `interval` | No | `60` | Seconds between replica scans |
| `bookmark_path` | No | - | File keeping the last published timestamp per stream, so a restart does not publish the re-read tail twice |
| `parser` | No | - | How each line is read |
| `parser.type` | No | `raw` | Shape of a line; raw keeps it whole. One of `raw`, `regex`, `json`, `logfmt` |
| `parser.pattern` | No | - | Regular expression with named groups (type regex) |
| `parser.timestamp_field` | No | - | Parsed field that carries the record timestamp |
| `parser.timestamp_format` | No | - | Go layout of the timestamp field |
| `multiline` | No | - | Fold physical lines into one record |
| `multiline.pattern` | No | - | Regular expression that marks a record boundary |
| `multiline.negate` | No | `false` | Fold the lines that do not match the pattern instead of those that do |
| `multiline.match` | No | `after` | Whether a folded line joins the record before it or the one after it. One of `after`, `before` |
| `max_bytes_per_line` | No | `1048576` | Cap on one assembled record |
| `min_severity` | No | - | Drop records below this severity; a record whose severity cannot be read is kept. One of `trace`, `debug`, `info`, `warn`, `error`, `fatal` |
| `exclude` | No | - | Regular expressions; a line matching one is dropped before parsing |
| `attach_spacing_ms` | No | `400` | Milliseconds between two stream attaches of this instance; the endpoint refuses a burst with 429 |
| `authority_host` | No | `login.microsoftonline.com` | Entra ID authority, for sovereign clouds |
| `management_host` | No | `management.azure.com` | Azure Resource Manager endpoint, for sovereign clouds |

<!-- schema:params:end -->

The `parser` and `multiline` blocks are the same ones [File Tail](filetail.md) documents, with the same fields and the same meaning.

# Log Records

Each record's body is the line (or the folded multiline record). Attributes:

| Attribute | Value |
|-----------|-------|
| `cloud.provider` | `azure` |
| `cloud.region` | The application's Azure region |
| `cloud.resource_id` | The ARM resource ID of the application |
| `senhub.azure_container_apps.app` | The application name. Not `service.name`: that key stays the identity of the resource that emits the record, the agent, and a record-level copy of it collides with it in every store. |
| `senhub.azure_container_apps.revision` | The revision the line came from |
| `senhub.azure_container_apps.replica` | The replica the line came from |
| `container.name` | The container the line came from |
| `log.iostream` | `stdout` or `stderr` |

Fields lifted by the `json`, `regex` and `logfmt` parsers are added as attributes; a `message`, `level` or configured timestamp field is promoted the same way as in `filetail`.

# Metrics Collected

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.azure_container_apps.up` | `{status}` | `1` when Azure Resource Manager answered the last replica scan, else `0` |
| `senhub.azure_container_apps.scan.failures` | `{scan}` | Cumulative scans Azure refused, one series per `reason` |
| `senhub.azure_container_apps.replicas` | `{replica}` | Replicas of the active revisions seen at the last scan |
| `senhub.azure_container_apps.revisions.active` | `{revision}` | Revisions marked active at the last scan |
| `senhub.azure_container_apps.streams.open` | `{stream}` | Streams currently attached, one per replica and container |
| `senhub.azure_container_apps.streams.wanted` | `{stream}` | Streams the last scan decided to hold |
| `senhub.azure_container_apps.stream.reconnects` | `{reconnect}` | Cumulative streams re-attached after a drop |
| `senhub.azure_container_apps.stream.attach_throttled` | `{attach}` | Cumulative attaches refused for rate by the stream endpoint |
| `senhub.azure_container_apps.stream.token.ttl` | `s` | Seconds left on the console stream token |
| `senhub.azure_container_apps.records_emitted` | `{record}` | Cumulative records published to the log rail |
| `senhub.azure_container_apps.records_dropped` | `{record}` | Cumulative lines dropped at the source by `exclude` or `min_severity` |
| `senhub.azure_container_apps.records_unparsed` | `{record}` | Cumulative lines the declared parser could not read |
| `senhub.azure_container_apps.records.last_age` | `s` | Seconds since a line last left this probe |
| `senhub.azure_container_apps.arm.reads_remaining` | `{request}` | Reads left in the subscription's Azure Resource Manager budget, as the last answer reported it |

These describe the collection, not the application. What the containers themselves consume is Azure Monitor's to answer, through a different interface.

Four of them answer a question the others cannot:

- **`streams.wanted` against `streams.open`.** A gap means a replica the scan knows about whose stream is not attached, which the replica count alone cannot show once a replica holds more than one container or a container filter is set.
- **`scan.failures` by `reason`.** Reachability reads the same for a refused secret, an application that was deleted and a control plane pushing back; the reasons are `denied`, `not_found`, `throttled`, `refused`, `timeout` and `unreachable`. A refused scan costs no line, but a replica that appears while scans are refused is not picked up, so this is where that gap becomes visible.
- **`records_unparsed`.** A line the declared parser cannot read is dropped. Declare `json` on an application that also writes a plain startup banner and those lines leave no trace anywhere else.
- **`records.last_age`.** Silence, not failure: an application with nothing to say is legitimately silent. Read against `streams.open`, it separates a pipe that is alive from one that is merely attached.

`stream.reconnects` is expected to climb on its own. Azure closes every console stream about every ten minutes and the probe re-attaches; `stream.attach_throttled` is the one that says the pace is too fast for the environment.

# Requirements

- An **Entra ID app registration** with a **client secret**.
- A role assignment on the Container App (or its resource group) that includes `Microsoft.App/containerApps/read`, `Microsoft.App/containerApps/revisions/read`, `Microsoft.App/containerApps/revisions/replicas/read` and the action `Microsoft.App/containerApps/getAuthtoken/action`. **Reader is not enough**: it lacks the last one. Contributor covers all four; a custom role with those four is the least privilege.
- Outbound HTTPS from the agent host to `login.microsoftonline.com`, `management.azure.com` and the log stream endpoint of the application's environment (`*.azurecontainerapps.dev`).

!!! tip "Filter at the source"
    Every line read is sent, stored and billed somewhere. A Java application
    at INFO level writes tens of thousands of lines a day per replica; with
    `min_severity: warn` most of them never leave the agent, and the
    `records_dropped` metric shows what was left out. Filtering here saves
    the network egress from Azure, the intake and the storage, where a
    backend-side rule would only save the disk.

!!! note "How many applications one agent can follow"
    Each scan asks the control plane three questions per application, so
    two hundred applications at the default interval means ten calls a
    second. Measured on a bench of two hundred: five calls a second held
    for minutes with no refusal, ten a second was refused almost
    entirely, and two idle minutes restored the budget. The streams
    themselves were never the problem, and a refused scan costs no line:
    the streams already attached keep running, but a replica that
    appears while scans are refused is not picked up.

    Keep the scan under five calls a second: about a hundred
    applications at the default sixty-second `interval`, five hundred at
    `interval: 300`. Past that, split them across several agents, each
    with its own app registration, since the limit follows the caller.

!!! note "What the stream endpoint does, measured"
    Azure closes every console stream about ten minutes after it was
    attached, all of them at once; the probe re-attaches within seconds
    and, on a bench of twenty-four containers, no line was lost across
    the cut. Attaches are paced (one every 400 ms) because a burst of
    twenty is refused with 429 "request rate is too high"; a refused
    attach is retried with the delay the endpoint asks for. Expect the
    `stream.reconnects` counter to grow by the number of streams every
    ten minutes: that is normal.

!!! note "A live stream, not an archive"
    The stream carries lines as they are written. While the agent is stopped, lines are not kept for it: on restart the probe re-reads the last `tail_lines` of each container, skips the ones its bookmark says were published, and resumes. Lines written beyond that window while the agent was down are lost to it. An application that needs every line kept keeps Azure's own Log Analytics as well; this probe is the low-latency path.

# Outputs

Log records are delivered to the outputs that consume logs, OTLP first; restrict them with the per-probe `log_strategies` key if needed. The state metrics are available through every configured output; for PRTG and Nagios, query the probe by its configured `name`:

```bash
curl "http://localhost:8080/api/{agentkey}/prtg/metrics/squash-logs"
curl "http://localhost:8080/api/{agentkey}/nagios/metrics/squash-logs"
```
