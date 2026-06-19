<img src="https://cdn.simpleicons.org/jenkins" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Jenkins CI

The `jenkins` probe monitors a Jenkins controller via its open HTTP REST API,
reporting job status counts, per-job build duration and number, node and
executor counts, and build queue depth.

## Quick start

```yaml
probes:
  - name: jenkins
    type: jenkins
    params:
      endpoint: https://jenkins.example.com
      username: monitor
      api_token: 11abc...
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | required | Base URL of the Jenkins controller (e.g. `https://jenkins.example.com`) |
| `username` | — | Jenkins username for API authentication |
| `api_token` | — | Jenkins API token for the user (preferred over a password) |

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
