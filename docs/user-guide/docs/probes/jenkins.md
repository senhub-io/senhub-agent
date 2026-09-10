<img src="https://cdn.simpleicons.org/jenkins" alt="" class="probe-page-logo probe-page-logo-si">

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

| Parameter | Required | Default | Description |
|---|---|---|---|
| `endpoint` | Yes | - | Base URL of the controller. Example: `https://jenkins.example.com` |
| `username` | No | - | User the API calls authenticate as; empty queries anonymously |
| `api_token` | No | - | API token of that user. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `15` | Request timeout in seconds |
| `instance_name` | No | - | Stable identity of this controller instead of the one it reports |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.jenkins.up` | 1 | 1 when the last cycle reached the Jenkins controller |
| `senhub.jenkins.job.count` | {job} | Jobs by last-build status (success/failure/unstable/aborted), tagged with `status` |
| `senhub.jenkins.job.last_build.duration` | s | Duration of the last build per job, tagged with `job` |
| `senhub.jenkins.job.last_build.number` | {build} | Last build number per job |
| `senhub.jenkins.node.count` | {node} | Build nodes by state (online/offline), tagged with `state` |
| `senhub.jenkins.executor.count` | {executor} | Total and busy executors |
| `senhub.jenkins.queue.depth` | {item} | Items waiting in the build queue |

## Operational notes

- Generate an API token at `https://<jenkins>/user/<username>/configure`. API tokens are preferred over passwords and can be revoked without changing the account password.
- The `endpoint` parameter is required; the probe will fail to start without it.
- No external SDK is used — the probe speaks the Jenkins JSON REST API directly with the stdlib HTTP client.
