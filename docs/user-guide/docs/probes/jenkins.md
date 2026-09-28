<img src="../../assets/probe-logos/jenkins.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Jenkins CI

The `jenkins` probe monitors a Jenkins controller via its open HTTP REST API,
reporting job status counts, per-job build duration and number, node and
executor counts, and build queue depth.

## Quick start

```yaml
# probes.d/10-jenkins.yaml — each file under probes.d/ is a YAML array of probes
- name: jenkins
  type: jenkins
  params:
    endpoint: https://jenkins.example.com
    username: monitor
    api_token: ${secret:jenkins.api_token}   # OS secret store; inline plaintext is auto-sealed on install
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:2cbeb73f24bc1c3b81ef974106f9b2df6c8507e6ec2f540fa56aa669dcaca839 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | Yes | - | Base URL of the controller. Example: `https://jenkins.example.com` |
| `username` | In practice | - | User the API calls authenticate as; empty queries anonymously |
| `api_token` | In practice | - | API token of that user. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `15` | Request timeout in seconds |
| `instance_name` | No | - | Stable identity of this controller instead of the one it reports |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.jenkins.up` | 1 | 1 when the last cycle reached the Jenkins controller |
| `senhub.jenkins.job.count` | {job} | Jobs by last-build status (success/failure/unstable/aborted), tagged with `status` |
| `senhub.jenkins.job.duration` | s | Duration of the last build per job, tagged with `job` |
| `senhub.jenkins.job.last_build_number` | {build} | Last build number per job |
| `senhub.jenkins.node.count` | {node} | Build nodes by state (online/offline), tagged with `state` |
| `senhub.jenkins.node.executor.count` | # | Executors across online nodes, by state (busy, idle) |
| `senhub.jenkins.queue.size` | # | Items in the build queue |

## Operational notes

- Generate an API token at `https://<jenkins>/user/<username>/configure`. API tokens are preferred over passwords and can be revoked without changing the account password.
- The `endpoint` parameter is required; the probe will fail to start without it.
- No external SDK is used — the probe speaks the Jenkins JSON REST API directly with the stdlib HTTP client.

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
| `senhub.jenkins.up` | `senhub.jenkins.up` | Jenkins Up | # | 1 when the last cycle reached the Jenkins controller, 0 otherwise |
| `senhub.jenkins.job.count` | `senhub.jenkins.job.count` | Jenkins Jobs {status} | # | Number of jobs whose last build ended in this status (success/failure/unstable/aborted) |
| `senhub.jenkins.job.duration` | `senhub.jenkins.job.duration` | Jenkins {job} Last Build Duration | ms | Duration of the job's last build |
| `senhub.jenkins.job.last_build_number` | `senhub.jenkins.job.last_build_number` | Jenkins {job} Last Build Number | # | Build number of the job's last build |
| `senhub.jenkins.node.count` | `senhub.jenkins.node.count` | Jenkins Nodes {status} | # | Number of build nodes/agents by status (online/offline) |
| `senhub.jenkins.node.executor.count` | `senhub.jenkins.node.executor.count` | Jenkins Executors {state} | # | Number of executors across online nodes by state (busy/free) |
| `senhub.jenkins.queue.size` | `senhub.jenkins.queue.size` | Jenkins Queue Size | # | Number of items in the build queue |
| `senhub.jenkins.queue.blocked` | `senhub.jenkins.queue.blocked` | Jenkins Queue Blocked | # | Number of blocked items in the build queue |

<!-- schema:metrics:end -->
