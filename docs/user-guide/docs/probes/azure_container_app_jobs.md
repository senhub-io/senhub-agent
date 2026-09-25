<img src="../../assets/probe-logos/azure_container_apps.svg" alt="" class="probe-page-logo probe-page-logo-mdi">

# Azure Container App Jobs

**Type:** `azure_container_app_jobs` · **Tier:** Pro · **Platforms:** any

The Azure Container App Jobs probe watches one job: whether its executions ran, whether they worked, how long they took, and what they wrote. One instance per job.

It is a separate probe from [Azure Container Apps](azure_container_apps.md), because a job is not an application. An application runs continuously and the question is whether it is up; its console is a stream the agent tails. A job is discrete: an execution starts, ends and leaves a verdict. Its console output is read **once**, after it has finished, and never again.

Both probes share the same credential, the same Azure Resource Manager access and the same read-budget pacing, so an agent watching applications and jobs on one subscription does not compete with itself.

**Collected data:**

- The verdict, the duration and the age of the last success, per job
- The console output of each finished execution, as log records
- The probe's own state: executions read, executions whose output was already cleaned up, and the reads left in the subscription's ARM budget

## Quick start

```yaml
# probes.d/41-azure-container-app-jobs.yaml
- name: nightly-import
  type: azure_container_app_jobs
  params:
    tenant_id: "00000000-0000-0000-0000-000000000000"
    client_id: "${secret:azure.client_id}"
    client_secret: "${secret:azure.client_secret}"
    subscription_id: "00000000-0000-0000-0000-000000000000"
    resource_group: "rg-production"
    app: "nightly-import"
    bookmark_path: "/var/lib/senhub/nightly-import.executions"
    interval: 60
```

`app` is the name of the job. It is called `app` because the two probes share their configuration shape; a job goes in the same field an application would.

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `tenant_id` | Yes | - | Entra ID tenant (directory) ID |
| `client_id` | Yes | - | Application (client) ID of the app registration |
| `client_secret` | Yes | - | Client secret of the app registration. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `subscription_id` | Yes | - | Subscription that holds the job |
| `resource_group` | Yes | - | Resource group of the job |
| `app` | Yes | - | Name of the Container Apps job |
| `containers` | No | - | Container names to read; empty reads every container of the execution |
| `tail_lines` | No | `100` | Lines read from a finished execution, 0 to 300. An execution's log is read once, so this is the whole of what is published for it |
| `interval` | No | `60` | Seconds between two reads of the execution list. A job that runs less often than this is still seen, its verdict being read from history. Its OUTPUT is not: Azure keeps an execution replica about 150 seconds after the run ends, so an interval above that loses the logs |
| `bookmark_path` | No | - | File remembering the executions already published, so a restart does not publish a run's output twice. Without it, a restart republishes the window Azure still holds |
| `parser` | No | - | How each line is read |
| `parser.type` | No | `raw` | Shape of a line; raw keeps it whole. One of `raw`, `regex`, `json`, `logfmt` |
| `parser.pattern` | No | - | Regular expression with named groups (type regex) |
| `parser.timestamp_field` | No | - | Parsed field that carries the record timestamp |
| `parser.timestamp_format` | No | - | Go layout of the timestamp field |
| `max_bytes_per_line` | No | `1048576` | Cap on one line read from an execution |
| `exclude` | No | - | Regular expressions; a line matching one is dropped |
| `authority_host` | No | `login.microsoftonline.com` | Entra ID authority, for sovereign clouds |
| `management_host` | No | `management.azure.com` | Azure Resource Manager endpoint, for sovereign clouds |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.azure_container_app_jobs.up` | 1 | 1 when Azure Resource Manager answered the last read |
| `senhub.azure_container_app_jobs.last_execution.status` | 1 | Verdict of the most recent execution: 0 unknown, 1 running, 2 succeeded, 3 failed |
| `senhub.azure_container_app_jobs.since_last_success` | s | Seconds since the last execution that succeeded |
| `senhub.azure_container_app_jobs.last_execution.duration` | s | How long the most recent finished execution took |
| `senhub.azure_container_app_jobs.executions` | {execution} | Executions in the reported history, split by state |
| `senhub.azure_container_app_jobs.executions.read` | {execution} | Executions whose output has been published |
| `senhub.azure_container_app_jobs.executions.without_logs` | {execution} | Executions whose verdict was read but whose output was gone |
| `senhub.azure_container_app_jobs.records.emitted` | {record} | Log records published from execution output |
| `senhub.azure_container_app_jobs.arm.reads_remaining` | {read} | Reads left in the subscription's ARM budget |

## What to alert on

**`since_last_success`, not the last status.** A scheduled job that has not succeeded since well past its period has a problem, whatever its most recent execution says — a job that stopped being triggered at all reports a perfectly good last status for ever. The metric is absent until one execution has succeeded, so a job that has never worked does not read as healthy.

`last_execution.status` is what tells you *which* kind of problem once the first has fired.

## Operational notes

- **An execution's output is read once, after it finishes.** A run still going is counted but not read: reading it would publish half of it and never the rest. Its lines arrive on the next cycle after it ends.
- **`bookmark_path` is what keeps a restart from republishing.** Without it, the agent republishes the executions Azure still holds each time it starts, and an operator sees yesterday's failures arrive again this morning. The file holds execution names, not a position in a stream.
- **Azure cleans up execution replicas in about two and a half minutes.** Measured on a real job: the replica of a finished execution still answered 150 seconds after the run ended and was gone at 180. Past that the verdict and the duration are still readable — they come from the execution history — but the output is gone, counted as `executions.without_logs` rather than reported as an error.
- **So `interval` is not only a freshness setting here.** An execution is read on the first cycle after it finishes, which is up to `interval` seconds later; an interval above the cleanup window loses the output of every run while the metrics keep looking healthy. The default of 60 seconds leaves roughly a minute and a half of margin. The probe warns at start when the configured interval is at or above 150 seconds.
- **The history is a window.** The execution counts are what Azure reports now, not everything that ever ran.
- **Two of the four job calls are served by a preview Azure API.** Reading the job and listing its executions answer on the same stable version the application probe uses; an execution's replicas and the job's `getAuthToken` answer only on `2023-11-02-preview`. So the verdict, the duration and the counts rest on a stable contract, and only reading the output rests on a preview one that Microsoft may change without a compatibility promise.

## Permissions

The probe reads; it never triggers a job. Three entries are enough, and
one of them has to be a wildcard:

| Entry | What it is for |
|---|---|
| `Microsoft.App/jobs/read` | the job itself: its trigger type and its stream endpoint |
| `Microsoft.App/jobs/*/read` | its executions, **and the replicas of an execution** |
| `Microsoft.App/jobs/getAuthtoken/action` | the short-lived token the log stream accepts |

`Microsoft.App/jobs/start/action` is deliberately **not** in that list:
the probe observes runs, it does not cause them.

### Why the wildcard, and what happens without it

`Microsoft.App/jobs/executions/read` is the obvious spelling for the
second line, and it is not enough. Listing the **replicas** of an
execution is the call that leads to its output, and Azure's provider
catalogue declares no action for it: there is no
`Microsoft.App/jobs/executions/replicas/read` to grant. The wildcard is
what covers it.

What makes this worth a paragraph is the failure mode. A credential
without it is not refused. The replicas call answers **HTTP 200 with an
empty list**, exactly as it does for an execution Azure has already
cleaned up. So the probe reports every verdict, every duration and every
count correctly, and never publishes a single line of output, while
`executions.without_logs` climbs and nothing anywhere says "permission".

Measured, not deduced: on one execution, while it was still running, a
credential holding the narrow action listed zero replicas at the same
moment an administrator listed one. Adding the wildcard, the next run's
output arrived.

If a job's verdicts look right and its logs never come, check this
before anything else.

**An existing Container Apps credential is usually not enough.** A role
assignment written for the applications probe is commonly scoped to
`Microsoft.App/containerApps/...`, which does not cover jobs. That one is
not a deduction either: on a subscription where the applications probe
was already collecting, the job reads were refused with
`AuthorizationFailed` naming `Microsoft.App/jobs/read`. The two probes
share a credential only if its role covers both; a credential already
carrying the four application actions needs these three added to it.

## Metric reference

Every metric this probe can emit. **Metric** is the OpenTelemetry name the
OTLP, Prometheus and Zabbix outputs derive theirs from. **Name** is what a
[Nagios check](../nagios.md) and the API `metrics=` filter match.
**PRTG channel** is the label PRTG shows, placeholders filled from the
series' tags.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Name | PRTG channel | Unit | Description |
|---|---|---|---|---|
| `senhub.azure_container_app_jobs.up` | `azure_container_app_jobs_up` | Control Plane Reachable | # | 1 when Azure Resource Manager answered the last read of the job, 0 when it did not |
| `senhub.azure_container_app_jobs.last_execution.status` | `azure_container_app_jobs_last_execution_status` | Last Execution | # | Verdict of the most recent execution: 0 unknown, 1 running, 2 succeeded, 3 failed |
| `senhub.azure_container_app_jobs.since_last_success` | `azure_container_app_jobs_since_last_success` | Since Last Success | s | Seconds since the last execution that succeeded. On a scheduled job this is the number to alert on: a job well past its period without a success has a problem, whatever its last execution says. Absent until one has succeeded |
| `senhub.azure_container_app_jobs.last_execution.duration` | `azure_container_app_jobs_last_execution_duration` | Last Execution Duration | s | How long the most recent finished execution took; 0 while one is still running |
| `senhub.azure_container_app_jobs.executions` | `azure_container_app_jobs_executions_running` | Executions Running | # | Executions in flight in the history Azure reports |
| `senhub.azure_container_app_jobs.executions` | `azure_container_app_jobs_executions_succeeded` | Executions Succeeded | # | Executions that succeeded in the history Azure reports. Azure keeps a window, so this counts what is visible rather than everything that ever ran |
| `senhub.azure_container_app_jobs.executions` | `azure_container_app_jobs_executions_failed` | Executions Failed | # | Executions that failed in the history Azure reports |
| `senhub.azure_container_app_jobs.executions.read` | `azure_container_app_jobs_executions_drained` | Executions Read | # | Executions whose console output has been published. An execution is read once, after it finishes |
| `senhub.azure_container_app_jobs.executions.without_logs` | `azure_container_app_jobs_executions_without_logs` | Executions Without Logs | # | Executions whose verdict was read but whose output was gone: Azure had already cleaned up the replica. The run is still counted; only its lines are lost |
| `senhub.azure_container_app_jobs.records.emitted` | `azure_container_app_jobs_records_emitted` | Records Emitted | # | Log records published from execution output |
| `senhub.azure_container_app_jobs.arm.reads_remaining` | `azure_container_app_jobs_arm_reads_remaining` | ARM Reads Remaining | # | Reads left in the subscription's Azure Resource Manager budget, as ARM reports it; a fleet of instances shows here how close it is to being throttled |

<!-- schema:metrics:end -->
