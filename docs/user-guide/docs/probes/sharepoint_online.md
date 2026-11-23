<img src="../../assets/probe-logos/azure_container_apps.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The SharePoint Online probe reports the health of SharePoint Online in one **Microsoft 365 tenant**, read through Microsoft Graph with Application permissions: the state of the service as Microsoft publishes it, the service issues that are still open, and whether each collection step of the probe itself succeeded.

One probe instance covers one tenant; declare one instance per tenant. The data is read in **lots**, each with its own switch and cadence. This release ships the **health** lot. The probe never writes and never reads the content of a site or a document.

| Lot | Reads | Status |
|---|---|---|
| `health` | Service state, open service issues, state of the collection step | Available |

Availability and certificate expiry of the SharePoint address are not measured here: add an `http_check` instance on the site URL.

# Quick Start

```yaml
# probes.d/44-sharepoint-online.yaml — each file under probes.d/ is a YAML array of probes
- name: sharepoint-prod
  type: sharepoint_online
  params:
    tenant_id: "00000000-0000-0000-0000-000000000000"
    client_id: "11111111-1111-1111-1111-111111111111"
    client_secret: "${secret:sharepoint.client_secret}"   # OS secret store
    health:
      interval: 300
```

With a certificate instead of a secret:

```yaml
- name: sharepoint-prod
  type: sharepoint_online
  params:
    tenant_id: "00000000-0000-0000-0000-000000000000"
    client_id: "11111111-1111-1111-1111-111111111111"
    client_certificate_path: /etc/senhub/sharepoint.crt
    client_key_path: /etc/senhub/sharepoint.key
```

# Requirements

- An **Entra ID app registration** with a **client secret** or a **certificate**.
- In *App registrations > API permissions*, the Microsoft Graph **Application** permission (type *Application*, not *Delegated*), then **Grant admin consent**:

| Lot | Application permission |
|---|---|
| `health` | `ServiceHealth.Read.All` |

The permission is read-only. Without admin consent Graph answers 403; the probe then reports permission denied (check result 7) with a message that names the permission and the consent step, and keeps running.

- Outbound HTTPS from the agent host to `login.microsoftonline.com` and `graph.microsoft.com`.

## Authentication

The probe authenticates with the first credential it finds, in this order:

1. **Certificate.** `client_certificate` (PEM, inline) or `client_certificate_path` (file), with `client_key` or `client_key_path`. One file may hold the certificate and the key. The key must be an unencrypted RSA key and must match the certificate; a mismatch is refused when the probe is configured. Upload the public certificate to the app registration (*Certificates & secrets*). The probe signs a short-lived assertion with the key (RS256) and never sends the key.
2. **Client secret.** `client_secret`, `client_secret_file`, `SENHUB_AZURE_CLIENT_SECRET` or `SENHUB_AZURE_CLIENT_SECRET_FILE`.

When a certificate is configured, the client secret is not read. `SENHUB_AZURE_TENANT_ID`, `SENHUB_AZURE_CLIENT_ID`, `SENHUB_AZURE_CLIENT_CERTIFICATE_PATH` and `SENHUB_AZURE_CLIENT_KEY_PATH` serve every Azure probe of the agent; a probe that sets a key itself keeps its own value. Files are read once, when the probe is configured.

# Configuration Parameters

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `tenant_id` | Yes | - | Entra ID tenant (directory) ID; not needed when `SENHUB_AZURE_TENANT_ID` is set |
| `client_id` | Yes | - | Application (client) ID of the app registration; not needed when `SENHUB_AZURE_CLIENT_ID` is set |
| `client_secret` | One credential | - | Client secret. A secret: reference it with `${secret:…}` |
| `client_secret_file` | No | - | File holding the client secret |
| `client_certificate` | No | - | Certificate as PEM |
| `client_key` | No | - | Private key as PEM. A secret |
| `client_certificate_path` | No | - | File holding the certificate PEM |
| `client_key_path` | No | - | File holding the private key PEM |
| `health.enabled` | No | `true` | Collect the health lot |
| `health.interval` | No | `300` | Seconds between health collections |

# Metrics

Every series carries `azure.tenant.id`. Names follow the `m365.sharepoint.*` namespace.

## Service health (`health`)

| Metric | Unit | Attributes | Meaning |
|---|---|---|---|
| `m365.sharepoint.service.status` | 1 | `m365.service.state` | One point per state, 1 for the current state and 0 for the others |
| `m365.sharepoint.service.issue.count` | {issue} | `m365.service.issue.classification` | Unresolved issues: `incident`, `advisory`, `other` |

`m365.service.state` takes the status of the service as Microsoft publishes it, one for one, in snake_case: `service_operational`, `service_degradation`, `service_interruption`, `investigating`, `restoring_service`, `verifying_service`, `service_restored`, `extended_recovery`, `post_incident_review_published`, `false_positive`, `investigation_suspended`, `resolved`. Any other value, including the ones Microsoft reserves for future use, is published as `other` and logged once as a warning.

## The probe itself

| Metric | Unit | Attributes | Meaning |
|---|---|---|---|
| `m365.sharepoint.collection.status` | 1 | `m365.sharepoint.collection.step`, `m365.sharepoint.collection.state` | One point per state (`ok`, `failed`, `stale`) for each step; this release has the step `health` |
| `senhub.probe.check.result` | {status} | `senhub.sharepoint_online.lot` | 0 ok, 1 target down, 2 auth denied, 3 timeout, 6 protocol error, 7 permission denied |

A failed read publishes no service series: the previous values are not repeated.

# PRTG

Read the probe through `/api/{key}/prtg/metrics/<probe name>` with an HTTP Data Advanced sensor. Suggested limits:

| Channel | Limit |
|---|---|
| Service status | any state other than `service_operational`: error |
| Open service issues | more than 0: warning |
| Collection status | any state other than `ok`: error |

# Throttling and errors

Graph answers 429 and 5xx are retried, honouring `Retry-After`. Paged answers are followed only on the Graph host. A 403 names the missing Application permission and the admin consent.
