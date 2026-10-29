<img src="../../assets/probe-logos/azure_container_apps.svg" alt="" class="probe-page-logo probe-page-logo-si">

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
- name: caj
  type: azure_container_app_jobs
  params:
    tenant_id: "00000000-0000-0000-0000-000000000000"
    client_id: "${secret:azure.client_id}"
    client_secret: "${secret:azure.client_secret}"
    subscription_id: "00000000-0000-0000-0000-000000000000"
    resource_group: "rg-production"
    app: "nightly-import"
    bookmark_path: "/var/lib/senhub-agent/caj.executions"
    interval: 60
```

`app` is the name of the job. It is called `app` because the two probes share their configuration shape; a job goes in the same field an application would.

## From environment variables

Where placing a file is harder than setting a variable (a container platform, a Helm chart), the probe is declared by the agent's [environment rule](../configuration.md#configuring-probes-from-environment-variables). The probe name is yours; the examples call it `CAJ` (Container App Job), as the applications page calls its probe `CA`. The job name goes in `APP`, one probe per job, and a name with a hyphen (`nightly-import`) is a value there, not part of a variable:

```bash
SENHUB_PROBE_CAJ_TYPE=azure_container_app_jobs
SENHUB_PROBE_CAJ_APP=nightly-import
SENHUB_PROBE_CAJ_TENANT_ID=00000000-0000-0000-0000-000000000000
SENHUB_PROBE_CAJ_CLIENT_ID=11111111-1111-1111-1111-111111111111
SENHUB_PROBE_CAJ_CLIENT_SECRET_FILE=/run/secrets/az_client_secret
SENHUB_PROBE_CAJ_SUBSCRIPTION_ID=22222222-2222-2222-2222-222222222222
SENHUB_PROBE_CAJ_RESOURCE_GROUP=rg-production
SENHUB_PROBE_CAJ_BOOKMARK_PATH=/var/lib/senhub-agent/caj.executions
```

A second job is a second probe with its own name (`CAJ2`) and its own `APP`; the [shared credentials](#shared-credentials) save repeating the identity, and [discovery](#discovering-jobs) follows every job with one probe.

!!! note "One entry per job, or discovery"
    An entry follows one job named by `app`, or every job of the subscription with a [`discovery:` block](#discovering-jobs). An entry with neither is refused.

- **One bookmark per job.** The file holds the names of the executions already published; two jobs sharing one overwrite each other and republish runs after a restart. `bookmark_path` defaults to `<probe name>.executions` in the agent's state directory, which should be a volume, so each probe name gets its own file without setting anything. Name it explicitly only to keep a bookmark you had before.
- **`${env:}` inside a value is not expanded.** `SENHUB_PROBE_CAJ_CLIENT_SECRET=${env:AZ_SECRET}` would reach the probe as that literal text. To share the credential between several jobs, point each probe's `CLIENT_SECRET_FILE` at the same mounted secret (`/run/secrets/az_client_secret` above, repeated for `CAJ2`), or declare the probes in `SENHUB_PROBES` (or a `probes.d` file), where `"${env:AZ_CLIENT_SECRET}"` is resolved at every start. See [the environment rule](../configuration.md#no-env-inside-a-value).
- **Precedence.** The variables overlay the probes of the files and of `SENHUB_PROBES`, key by key, and the variable wins.

### Followed by default with a discovery of the applications

When the applications are discovered from the environment (one `SENHUB_PROBE_<NAME>_TYPE=azure_container_apps` probe with a `DISCOVERY__*` variable), the agent also follows the jobs of the same subscription, without a variable of its own: it adds a probe named `jobs` (type `azure_container_app_jobs`) with the same identity (`TENANT_ID`, `CLIENT_ID`, `CLIENT_SECRET` or `CLIENT_SECRET_FILE`, `SUBSCRIPTION_ID`, and the sovereign-cloud hosts), the same `DISCOVERY__RESOURCE_GROUPS` and the same `DISCOVERY__INTERVAL`; the applications' `INCLUDE` and `EXCLUDE` filters are not carried over. The job probe reads the Azure Resource Manager API and the jobs' console stream, never Log Analytics. `SENHUB_PROBE_JOBS_TYPE=none` turns it off. Any other `SENHUB_PROBE_JOBS_*` variable, a probe named `jobs` in a file or in `SENHUB_PROBES`, or several applications discoveries declared from the environment leave the configuration as it is. A configuration written in files is never extended this way. When the identity may not list jobs, the agent warns once and keeps following the applications.

## Discovering jobs

Instead of one entry per job, a `discovery:` block follows every job the subscription holds. It takes the same keys as the applications probe; `max_apps` caps the number of jobs followed.

```yaml
probes:
  caj:
    type: azure_container_app_jobs
    subscription_id: 00000000-0000-0000-0000-000000000000
    discovery:
      resource_groups: [rg-batch, rg-etl]
      include: ["etl-*", "report"]
      exclude: ["*-tmp"]
      max_apps: 50        # cap on followed jobs
      interval: 300       # seconds between two enumerations
```

With the [shared credentials](#shared-credentials) in the environment, the same probe can be declared from the environment alone:

```bash
SENHUB_PROBE_CAJ_TYPE=azure_container_app_jobs
SENHUB_PROBE_CAJ_DISCOVERY__RESOURCE_GROUPS=rg-batch,rg-etl
SENHUB_PROBE_CAJ_DISCOVERY__EXCLUDE=*-tmp
```

Each discovered job is followed exactly like a configured one. Its metrics carry the `azure_job` label, and the executions already published are remembered in `<probe name>.<job>.executions` in the agent state directory (`caj.etl-daily.executions`). A job that disappears is dropped at the next enumeration, and one that appears is picked up; a failed enumeration keeps the jobs already followed. `app`, `resource_group` and `bookmark_path` cannot be combined with `discovery`. The credential needs `Microsoft.App/jobs/read` on the subscription, or on each resource group listed in `resource_groups`.

If executions come back with no replica and no log arrives, the log names the missing `Microsoft.App/jobs/*/read` permission once per job.

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:3994de8591dff2c663778e7f93549200936e23abffba1b3ee3ff42d26186a192 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `tenant_id` | In practice | - | Entra ID tenant (directory) ID; required unless SENHUB_AZURE_TENANT_ID is set for every Azure probe of the agent |
| `client_id` | In practice | - | Application (client) ID of the app registration; required unless SENHUB_AZURE_CLIENT_ID is set for every Azure probe of the agent |
| `client_secret` | In practice | - | Client secret of the app registration; one of client_secret, client_secret_file, SENHUB_AZURE_CLIENT_SECRET or SENHUB_AZURE_CLIENT_SECRET_FILE is required, in that order of precedence. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `client_secret_file` | No | - | File holding the client secret (a mounted secret, for example); read when the probe is configured. Used when client_secret is not set |
| `subscription_id` | In practice | - | Subscription that holds the job; required unless SENHUB_AZURE_SUBSCRIPTION_ID is set for every Azure probe of the agent |
| `resource_group` | No | - | Resource group of the job; required unless 'discovery' is set, which finds it |
| `app` | No | - | Name of the Container Apps job; required unless 'discovery' is set, which finds them |
| `discovery` | No | - | Follow every job the subscription holds instead of naming one; exclusive with 'app', 'resource_group' and 'bookmark_path'. Each job is followed as if it had its own entry |
| `discovery.interval` | No | `300` | Seconds between two enumerations of the subscription; a job that left is dropped and a new one picked up at the next one |
| `discovery.max_apps` | No | `100` | Hard cap on how many jobs one instance follows (the key is shared with the applications probe); what is left out is named in the log |
| `discovery.resource_groups` | No | - | Bound the search to these resource groups; empty means every group the credential can read |
| `discovery.include` | No | - | Follow only the names matching one of these; '*' is the only wildcard |
| `discovery.exclude` | No | - | Never follow a name matching one of these; wins over include |
| `containers` | No | - | Container names to read; empty reads every container of the execution |
| `tail_lines` | No | `100` | Lines read from a finished execution, 0 to 300. An execution's log is read once, so this is the whole of what is published for it |
| `interval` | No | `60` | Seconds between two reads of the execution list. A job that runs less often than this is still seen, its verdict being read from history. Its OUTPUT is not: Azure keeps an execution replica about 150 seconds after the run ends, so an interval above that loses the logs |
| `bookmark_path` | No | - | File remembering the executions already published, so a restart does not publish a run's output twice. Defaults to <probe name>.executions in the agent state directory (<probe name>.<job>.executions per job under discovery, where this key is refused) |
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

## Shared credentials

To avoid repeating the identity of the app registration in every entry, set it once in the agent's environment. It serves both Azure probes (`azure_container_apps` and `azure_container_app_jobs`). A probe that sets a key itself keeps its own value for that key.

| Variable | Replaces |
|---|---|
| `SENHUB_AZURE_TENANT_ID` | `tenant_id` |
| `SENHUB_AZURE_CLIENT_ID` | `client_id` |
| `SENHUB_AZURE_CLIENT_SECRET` | `client_secret` |
| `SENHUB_AZURE_CLIENT_SECRET_FILE` | path of a file holding the secret (a mounted secret, for example) |
| `SENHUB_AZURE_SUBSCRIPTION_ID` | `subscription_id` |

The secret is taken from the first of these that is set: `client_secret`, `client_secret_file` (a probe key naming a file), `SENHUB_AZURE_CLIENT_SECRET`, `SENHUB_AZURE_CLIENT_SECRET_FILE`. A secret file that cannot be read is an error, not a fallback. The variables are read when the probe is configured, so changing them needs a restart.

```yaml
probes:
  ca:
    type: azure_container_apps
    discovery: {}
  caj:
    type: azure_container_app_jobs
    discovery: {}
```

## Checking the setup

`senhub-agent check azure` verifies, without changing anything, that the credentials of the configured Azure probes (`azure_container_apps` and `azure_container_app_jobs`) can do what the probes need. It prints one verdict per line and never prints a secret.

```bash
senhub-agent check azure               # every enabled Azure probe in the installed configuration
senhub-agent check azure --probe caj   # one probe, by name
senhub-agent check azure /path/to/agent.yaml
```

For each probe it checks, in order:

| Line | What it verifies |
|---|---|
| `token` | Entra ID issues a token for the tenant, client ID and secret of the probe |
| `subscription` | The subscription can be read. A `WARN` here is normal when the role is scoped to a resource group |
| `apps` / `jobs` | The application (or, with `discovery`, every application) and the job are found, grouped by resource group |
| `permission` | Each action the probe needs is granted at its scope, from the effective permissions Azure reports for the credential |

The actions checked are `Microsoft.App/containerApps/read`, `.../revisions/read`, `.../revisions/replicas/read` and `.../getAuthtoken/action` for applications, and `Microsoft.App/jobs/read`, `Microsoft.App/jobs/*/read` and `Microsoft.App/jobs/getAuthtoken/action` for jobs. A role that only grants `jobs/executions/read` is reported as missing `jobs/*/read`: the verdicts would look right while no log line ever arrived.

The command exits with `0` when every required item passes and `1` when one fails (or when no Azure probe is configured), so it can gate a deployment script. Like the other commands, it runs as root or administrator.

## Dashboards

A ready-made Grafana dashboard for this probe, `azure-container-app-jobs.json`, is provided in [`docs/grafana/`](https://github.com/senhub-io/senhub-agent/tree/master/docs/grafana) (see its README for the data path it expects). It covers executions by status per job, duration, time since last success, failures and logs filtered by job and severity.

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
| `senhub.azure_container_app_jobs.read.failures` | `azure_container_app_jobs_read_failures` | Read Failures {reason} | # | Cumulative reads of the job's executions, replicas or output Azure refused, by cause: denied, not_found, throttled, refused, timeout, unreachable, no_replica. denied is almost always a role without Microsoft.App/jobs/*/read |

<!-- schema:metrics:end -->
