<img src="https://cdn.simpleicons.org/veeam" alt="" class="probe-page-logo probe-page-logo-si">

# Veeam Backup & Replication

Monitors Veeam Backup & Replication v13 via the REST API. Collects job status, repository capacity, license usage, proxy health, protected objects, and infrastructure server availability.

**License**: Pro

## Prerequisites

- Veeam Backup & Replication v13 or later
- A Veeam account with **Backup Administrator** role (Backup Viewer is not sufficient for the REST API)
- Network access from the agent to the Veeam server on port **9419** (REST API)

## Configuration

```yaml
# probes.d/30-veeam.yaml — each file under probes.d/ is a YAML array of probes
- name: veeam-prod
  type: veeam
  params:
    endpoint: "https://veeam-server"
    port: 9419
    username: 'DOMAIN\svc_monitoring'
    password: ${secret:veeam-prod.password}   # OS secret store; inline plaintext is auto-sealed on install
    interval: 300
    verify_ssl: false
    hours_to_check: 24
```

### Parameters

<!-- Hand-maintained: this probe's schema lives in senhub-agent-enterprise; check its parser before editing. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `endpoint` | Yes | - | Backup server hostname or URL, for example `https://veeam.example.com`. The port is appended when the address carries none |
| `port` | No | `9419` | REST API port, used when `endpoint` carries none. An integer |
| `username` | Yes | - | Veeam account with the Backup Administrator role |
| `password` | Yes | - | Password of the Veeam account. A secret: reference it with `${secret:...}`, `${env:...}` or `${file:...}` rather than writing it in the file |
| `verify_ssl` | No | `true` | Validate the server's TLS certificate; `false` for a self-signed certificate |
| `interval` | No | `300` | Seconds between collections. An integer |
| `hours_to_check` | No | `24` | Job history window in hours; a job with no run inside it counts as stale. An integer |

The three integer parameters must be written as plain numbers (`300`, not `"300"` or `300s`); any other form is ignored and the default applies.

## Collected Metrics

### Job Overview

Aggregated job counts by type (VSphereBackup, WindowsAgentBackup, BackupCopy, etc.):

| Metric | Unit | Description |
|--------|------|-------------|
| `veeam_jobs_total` | Count | Total enabled jobs with activity in the time window |
| `veeam_jobs_success` | Count | Jobs with last run successful |
| `veeam_jobs_warning` | Count | Jobs with last run warnings |
| `veeam_jobs_failed` | Count | Jobs with last run failed |
| `veeam_jobs_running` | Count | Jobs currently running |

### Job Details

Per-job metrics for each active backup job:

| Metric | Unit | Description |
|--------|------|-------------|
| `veeam_job_status` | Lookup | Last run result (None/Success/Warning/Failed/Running) |
| `veeam_job_seconds_since` | Seconds | Time since last run |
| `veeam_job_running_seconds` | Seconds | Elapsed running time of a currently-running job (0 when not running). The supervisor sets the "too long" alert limit; the probe applies no threshold |
| `veeam_job_objects_count` | Count | Number of objects processed |
| `veeam_job_bottleneck` | Lookup | Bottleneck type (None/Source/Proxy/Network/Target) |
| `veeam_job_processed_bytes` | Bytes | Total disk size processed |
| `veeam_job_read_bytes` | Bytes | Data read from source |
| `veeam_job_transferred_bytes` | Bytes | Data transferred after dedup/compression |

### Repositories

Per-repository capacity metrics:

| Metric | Unit | Description |
|--------|------|-------------|
| `veeam_repo_capacity` | Bytes | Total repository capacity |
| `veeam_repo_used` | Bytes | Used space |
| `veeam_repo_free` | Bytes | Free space |
| `veeam_repo_free_pct` | Percent | Free space percentage |

### License

| Metric | Unit | Description |
|--------|------|-------------|
| `veeam_license_status` | Lookup | Valid / Expired / Invalid |
| `veeam_license_days_left` | Days | Days until license expiration |
| `veeam_license_instances_total` | Count | Licensed instances |
| `veeam_license_instances_used` | Count | Used instances |
| `veeam_license_instances_remaining` | Count | Available instances |

### Proxies

Per-proxy status:

| Metric | Unit | Description |
|--------|------|-------------|
| `veeam_proxy_status` | Lookup | Disabled / Offline / Online |
| `veeam_proxies_total` | Count | Total proxies |
| `veeam_proxies_enabled` | Count | Enabled proxies |
| `veeam_proxies_disabled` | Count | Disabled proxies |

### Protected Objects

Per-object backup status:

| Metric | Unit | Description |
|--------|------|-------------|
| `veeam_object_restore_points` | Count | Number of restore points |
| `veeam_object_last_run_failed` | Count | 1 if last backup failed, 0 otherwise |
| `veeam_objects_total` | Count | Total protected objects |
| `veeam_objects_failed` | Count | Objects whose last backup failed |

### Agent Backups

Veeam Agent Backup jobs (physical Windows and Linux machines protected by the Veeam Agent) are not exposed by the Veeam REST `/jobs` endpoint — only as protected objects. The probe surfaces each agent-managed object as an additional `veeam_job_status` channel tagged `metric_type:jobs_status`, so it appears and is coloured by the same **Job Status** sensor as the VM jobs.

| Metric | Unit | Description |
|--------|------|-------------|
| `veeam_job_status` | Lookup | Agent backup status: Failed (last run failed), Never Run (no restore point yet), otherwise Success. The `job_type` tag is `WindowsAgentBackup` or `LinuxAgentBackup` |

Because a protected object carries no session timestamp, the Stale and Running states do not apply to agent backups; the restore-point count stays on the `veeam_object_restore_points` channel above.

### Infrastructure

Managed server availability:

| Metric | Unit | Description |
|--------|------|-------------|
| `veeam_server_status` | Lookup | Available / Unavailable |
| `veeam_servers_total` | Count | Total managed servers |
| `veeam_servers_available` | Count | Available servers |
| `veeam_servers_unavailable` | Count | Unavailable servers |

## PRTG Integration

The probe includes PRTG lookups for status fields. When creating PRTG sensors, use the **REST Custom** sensor type pointing to:

```
http://<agent-ip>:<port>/api/<agent-key>/prtg/metrics/<probe-name>?tags=metric_type:<category>
```

Available categories: `overview`, `jobs`, `repositories`, `license`, `proxies`, `protected_objects`, `infrastructure`.

Example for job details:
```
http://192.168.1.100:8056/api/550e8400-.../prtg/metrics/veeam-prod?tags=metric_type:jobs
```

Download the PRTG lookup files from the console (link "Download PRTG lookups" in the Sensor URLs tab of the HTTP output).

## Troubleshooting

### HTTP 403 on startup

The Veeam account must have the **Backup Administrator** role. The REST API does not work with Backup Viewer.

### HTTP 500 on job collection

Some Veeam installations have job types (e.g., HyperVBackup) that cause server-side errors. The agent handles this automatically by querying jobs per type.

### Job type shows "CustomPlatform"

Plugin-managed backups (Veeam Backup for **Nutanix AHV** and other external
modules) are reported by Veeam's `/jobs/states` endpoint without a standard job
type. The agent labels these as `CustomPlatform` (Veeam's own term) so they are
grouped and filterable, rather than left as "Unknown".

These jobs also carry less information than a native one, which surprises
operators filtering by tag:

- **The status is not in `jobs_detail`.** The job status channel is tagged
  `metric_type:jobs_status`; a query filtering on `metric_type:jobs_detail`
  deliberately excludes it and returns only Time Since Last Run, Running
  Duration and Objects Count. Query `metric_type:jobs_status` for the status.
- **`Time Since Last Run` is `-1`, and that is not a failure.** Veeam reports
  no session timestamp for plugin-managed jobs, so the channel carries the
  "no measurement" sentinel rather than a duration. Do not alert on it for
  this job type.
- **No transferred bytes and no bottleneck channels.** For the same reason
  (no session progress from VBR), those channels are not emitted at all.
- **The status itself is meaningful.** When Veeam reports no last run, the
  probe falls back to the job's last result, so these jobs show their real
  Success / Warning / Failed instead of "Never Run".

### No job metrics

If `hours_to_check` is too short, jobs that ran outside the time window are excluded. Increase the value (default: 24 hours).
