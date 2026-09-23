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
- **Azure cleans up execution replicas.** Once it has, the verdict and the duration are still readable but the output is gone — counted as `executions.without_logs` rather than reported as an error. Reading a job more often than Azure cleans up is what keeps that number at zero.
- **The history is a window.** The execution counts are what Azure reports now, not everything that ever ran.
- **The log stream of an execution is served by a preview Azure API**, where the application probe's is stable. The verdict, the duration and the counts come from the stable API; only the output depends on the preview one, and Microsoft may change it without a compatibility promise.

## Permissions

The same app registration as the applications probe: read access on the job, plus the action behind `getAuthToken`, which the Reader role does not carry. A credential with Reader alone reports the verdicts and no output.
