<img src="../../assets/probe-logos/azure_container_apps.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The SharePoint Online probe reports SharePoint Online in one **Microsoft 365 tenant**, read through Microsoft Graph with Application permissions: the state of the service as Microsoft publishes it, the service issues that are still open, how many sites the tenant has, how much storage and how many files they hold, the largest sites and the sites close to their quota, and whether each collection step of the probe itself succeeded.

One probe instance covers one tenant; declare one instance per tenant. The data is read in **lots**, each with its own switch and cadence. This release ships the **health** and **capacity** lots. The probe never writes and never reads the content of a site or a document.

| Lot | Reads | Status |
|---|---|---|
| `health` | Service state, open service issues, state of the collection step | Available |
| `capacity` | Site count, storage, files, recycle bins and sharing-link counters, tenant-wide and for the largest sites | Available |

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

With a certificate instead of a secret, and only the sites the application was granted with `Sites.Selected` (see [Granted sites](#granted-sites-sitesselected)):

```yaml
- name: sharepoint-prod
  type: sharepoint_online
  params:
    tenant_id: "00000000-0000-0000-0000-000000000000"
    client_id: "11111111-1111-1111-1111-111111111111"
    client_certificate_path: /etc/senhub/sharepoint.crt
    client_key_path: /etc/senhub/sharepoint.key
    capacity:
      sites:
        - https://contoso.sharepoint.com/sites/finance
        - https://contoso.sharepoint.com/sites/projects
```

# Requirements

- An **Entra ID app registration** with a **client secret** or a **certificate**.
- In *App registrations > API permissions*, the Microsoft Graph **Application** permission (type *Application*, not *Delegated*), then **Grant admin consent**:

| Lot | Application permission | Reads |
|---|---|---|
| `health` | `ServiceHealth.Read.All` | Service health and its issues |
| `capacity` | `Reports.Read.All` | The SharePoint site usage report: every tenant figure |
| `capacity` | `Sites.Read.All`, or `Sites.Selected` with `capacity.sites` | Names and addresses of the sites, and the recycle bin of the sites published one by one |

Every permission is read-only. Without admin consent Graph answers 403; the probe then reports permission denied (check result 7) with a message that names the permission and the consent step, and keeps running. If only the site list is refused (`Sites.Read.All` missing), the tenant figures are still published and the log says once that `Sites.Read.All` is missing or that `capacity.sites` must be set.

- Outbound HTTPS from the agent host to `login.microsoftonline.com` and `graph.microsoft.com`.

## Authentication

The probe authenticates with the first credential it finds, in this order:

1. **Certificate.** `client_certificate` (PEM, inline) or `client_certificate_path` (file), with `client_key` or `client_key_path`. One file may hold the certificate and the key. The key must be an unencrypted RSA key and must match the certificate; a mismatch is refused when the probe is configured. Upload the public certificate to the app registration (*Certificates & secrets*). The probe signs a short-lived assertion with the key (RS256) and never sends the key.
2. **Client secret.** `client_secret`, `client_secret_file`, `SENHUB_AZURE_CLIENT_SECRET` or `SENHUB_AZURE_CLIENT_SECRET_FILE`.

When a certificate is configured, the client secret is not read. `SENHUB_AZURE_TENANT_ID`, `SENHUB_AZURE_CLIENT_ID`, `SENHUB_AZURE_CLIENT_CERTIFICATE_PATH` and `SENHUB_AZURE_CLIENT_KEY_PATH` serve every Azure probe of the agent; a probe that sets a key itself keeps its own value. Files are read once, when the probe is configured.

## Granted sites (`Sites.Selected`)

`Sites.Read.All` lets the application read every site of the tenant. When that is more than the tenant wants to grant, give the application `Sites.Selected` instead (an Application permission, with admin consent) and list the sites it may read in `capacity.sites`. The probe then reads those sites only and never lists the tenant.

`Sites.Selected` grants nothing by itself: each site needs a grant, made once by an administrator, not by the probe.

1. Find the site id: `GET https://graph.microsoft.com/v1.0/sites/contoso.sharepoint.com:/sites/finance` answers `id`, in the form `contoso.sharepoint.com,<siteCollectionId>,<webId>`.
2. Grant the application read access, with an identity that may manage site permissions (an application holding `Sites.FullControl.All`, or a SharePoint administrator through Graph Explorer):

```http
POST https://graph.microsoft.com/v1.0/sites/{site-id}/permissions
Content-Type: application/json

{
  "roles": ["read"],
  "grantedToIdentities": [
    { "application": { "id": "<client_id of the probe>", "displayName": "SenHub SharePoint probe" } }
  ]
}
```

3. Put the site URL (or the id from step 1) in `capacity.sites`.

In this mode:

- the **tenant figures** (site count, storage, files, report age) still come from the usage report, which needs `Reports.Read.All` and covers the whole tenant;
- the **per-site series** are the granted sites that the report also lists, joined on the site id, still limited to the `top_n` largest plus the over-threshold sites;
- a granted site the application cannot read, or that the report does not list yet (a new site shows up in the report after a day or two), is named once in the log and skipped.

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
| `capacity.enabled` | No | `true` | Collect the capacity lot |
| `capacity.interval` | No | `21600` | Seconds between inventories (usage report and site list) |
| `capacity.top_n` | No | `10` | Largest sites published one by one; up to as many more over-threshold sites are added, so at most 2 x `top_n` sites per cycle |
| `capacity.quota_warning_percent` | No | `90` | A site is over-threshold when its used storage is at least this percent of its storage limit, only when it has a limit |
| `capacity.max_sites` | No | `20000` | Most sites read from the site list; beyond it the per-site series stop at the cap with a warning, the tenant figures stay complete |
| `capacity.sites` | No | - | Site URLs or Graph site ids to read, for an application granted `Sites.Selected`. When set, the tenant is never listed |
| `capacity.sites_include` | No | - | Per-site series only for sites whose web address matches one of these globs (`*` is any run of characters, case-insensitive) |
| `capacity.sites_exclude` | No | - | No per-site series for sites whose web address matches; wins over `sites_include` |

The globs apply to the web address given by the site list, never to the report, and to the per-site series only: the tenant figures always cover every site of the report.

# Metrics

Every series carries `azure.tenant.id`. Names follow the `m365.sharepoint.*` namespace.

## Service health (`health`)

| Metric | Unit | Attributes | Meaning |
|---|---|---|---|
| `m365.sharepoint.service.status` | 1 | `m365.service.state` | One point per state, 1 for the current state and 0 for the others |
| `m365.sharepoint.service.issue.count` | {issue} | `m365.service.issue.classification` | Unresolved issues: `incident`, `advisory`, `other` |

`m365.service.state` takes the status of the service as Microsoft publishes it, one for one, in snake_case: `service_operational`, `service_degradation`, `service_interruption`, `investigating`, `restoring_service`, `verifying_service`, `service_restored`, `extended_recovery`, `post_incident_review_published`, `false_positive`, `investigation_suspended`, `resolved`. Any other value, including the ones Microsoft reserves for future use, is published as `other` and logged once as a warning.

## Capacity (`capacity`)

The inventory reads the site usage report (`getSharePointSiteUsageDetail`, period `D7`) and joins it with the site list on the site id. Microsoft refreshes the report once a day, a day or two behind: `m365.sharepoint.report.age` shows how old the figures are.

| Metric | Unit | Attributes | Meaning |
|---|---|---|---|
| `m365.sharepoint.site.count` | {site} | `m365.sharepoint.site.state` or `m365.sharepoint.site.inactivity` | Two facets, each summing to the number of sites: state (`active`, `deleted`), and time since the last activity (`under_30d`, `30d_to_90d`, `over_90d`, `never`) |
| `m365.sharepoint.storage.used` | By | | Storage of every site of the report; deleted sites and OneDrive excluded |
| `m365.sharepoint.file.count` | {file} | `m365.sharepoint.file.state` | Files of those sites, `active` (touched in the period) or `inactive` |
| `m365.sharepoint.report.age` | s | | Time since the report's refresh date |
| `m365.sharepoint.report.concealed` | 1 | | 1 when the tenant conceals site addresses in reports (a tenant setting, for the whole report), else 0. Provisional rule: a site that is not deleted and has an empty address in the report marks the report as concealed; the exact format Microsoft uses is still to be measured |
| `m365.sharepoint.site.storage.used` | By | `m365.sharepoint.site.id`, `.url`, `.name` | Largest sites and over-threshold sites only |
| `m365.sharepoint.site.storage.limit` | By | same | Only for a site with a limit |
| `m365.sharepoint.site.recycle_bin.used` | By | same | Recycle bin of the default library, read through `/sites/{id}/drive` |
| `m365.sharepoint.site.file.count` | {file} | same, `m365.sharepoint.file.state` | Files of the site |
| `m365.sharepoint.site.sharing.link.count` | {link} | same, `m365.sharepoint.sharing.link.scope` | Sharing links of the site (`anonymous`, `company`, `secure_guest`, `secure_member`), only for the columns the report carries |

There is no tenant-wide storage limit series: no tenant-level quota source is confirmed. A concealed tenant shows names from the site list; without the site list (no `Sites.Read.All`, no `capacity.sites`) there are no per-site series. The sharing-link columns are not documented for the v1.0 report; when the report has none, no link series is published.

When the inventory fails, or the site list does, the series are still published from the last good inventory, with `report.age` showing its age. A failed read of the site list alone keeps the previous per-site series; no series is ever published with only a site id.

## The probe itself

| Metric | Unit | Attributes | Meaning |
|---|---|---|---|
| `m365.sharepoint.collection.status` | 1 | `m365.sharepoint.collection.step`, `m365.sharepoint.collection.state` | One point per state (`ok`, `failed`, `stale`) for each step; the steps are `health` and `inventory`. `inventory` is `failed` when the last read failed, and `stale` when it failed and the figures still published are older than two inventory intervals |
| `senhub.probe.check.result` | {status} | `senhub.sharepoint_online.lot` | 0 ok, 1 target down, 2 auth denied, 3 timeout, 6 protocol error, 7 permission denied |

A failed read publishes no service series: the previous values are not repeated.

# PRTG

Read the probe through `/api/{key}/prtg/metrics/<probe name>` with an HTTP Data Advanced sensor. Suggested limits:

| Channel | Limit |
|---|---|
| Service status | any state other than `service_operational`: error |
| Open service issues | more than 0: warning |
| Collection status | any state other than `ok`: error |
| Report age | more than 96 hours: warning |

# Throttling and errors

Graph answers 429 and 5xx are retried, honouring `Retry-After`. Paged answers are followed only on the Graph host. A 403 names the missing Application permission and the admin consent.
