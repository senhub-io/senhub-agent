<img src="../../assets/probe-logos/azure_container_apps.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The Microsoft Entra ID probe reports the health of one **Microsoft Entra ID tenant** (the directory behind Microsoft 365 and Azure): when application secrets and certificates expire, whether directory synchronisation with on-premises Active Directory is alive, how many users, groups, devices and licences the directory holds, the state of Microsoft's identity services, and the security posture of the tenant. It reads **Microsoft Graph** with an Entra ID app registration (client credentials): no user sign-in and nothing installed on any machine.

One probe instance covers one tenant; declare one instance per tenant. The collection is split into **13 modules**, each reading a different part of the directory and each needing its own Application permission. Five modules run by default; the others are asked for by name in `modules`, because they read more of the tenant and need further permissions.

| Module | Reads | Default |
|---|---|---|
| `sp` | Secrets and certificates of app registrations (and, optionally, enterprise applications): count by time to expiry, or time left of each one | Yes |
| `sync` | Directory synchronisation with on-premises Active Directory: whether it is enabled and how long ago it last succeeded | Yes |
| `users` | Users by account state, type and licence; users by time since last sign-in | Yes |
| `licenses` | Licences assigned and purchased per paid user SKU | Yes |
| `health` | State of the Microsoft Entra services and unresolved service issues | Yes |
| `syncapp` | The provisioning job of Microsoft Entra Connect: schedule state, outcome, duration and age of the last execution, entries processed | No |
| `security` | Directory roles and their assignments, Conditional Access policies, users flagged by Identity Protection | No |
| `groups` | Groups by type, membership and Teams enablement | No |
| `apps` | App registrations by sign-in audience, credential composition and worst credential state | No |
| `devices` | Devices by operating system, compliance, management, trust type and activity | No |
| `authmethods` | Users by registered authentication method, MFA, passwordless and self-service password reset capability | No |
| `apppermissions` | OAuth2 permission grants by consent, category and risk; service principals holding them | No |
| `pim` | Privileged Identity Management role assignments, eligible and active | No |

# Quick Start

```yaml
# probes.d/43-entra-id.yaml — each file under probes.d/ is a YAML array of probes
- name: entra-prod
  type: entra_id
  params:
    tenant_id: "00000000-0000-0000-0000-000000000000"
    client_id: "11111111-1111-1111-1111-111111111111"
    client_secret: "${secret:entra.client_secret}"   # OS secret store; inline plaintext is auto-sealed on install
    modules: [sp, sync, users, licenses, health, security]
    auth_mode: certsecret          # one series per secret / certificate
    exclude: ["test-"]
```

Without `modules` the probe runs `sp`, `sync`, `users`, `licenses` and `health`.

# Requirements

- An **Entra ID app registration** with a **client secret**.
- In *App registrations > API permissions*, the Microsoft Graph **Application** permissions (type *Application*, not *Delegated*) of the modules you enable, then **Grant admin consent**. A permission without admin consent is refused like a missing one.

| Module | Application permission | Notes |
|---|---|---|
| `sp` | `Application.Read.All` | |
| `sync` | `Organization.Read.All` | |
| `syncapp` | `Application.Read.All` and `Synchronization.Read.All` | The first finds the provisioning service principal (`sp_id` skips the lookup) |
| `users` | `User.Read.All` | Optional parts: `AuditLog.Read.All` for the sign-in activity (needs Microsoft Entra ID P1 or P2), `Organization.Read.All` for the licence of each user |
| `security` | `Directory.Read.All` for directory roles, `Policy.Read.All` for Conditional Access, `IdentityRiskyUser.Read.All` for risky users | Each part is optional: a refused part is dropped and the others still collect. Risky users need Microsoft Entra ID P2 |
| `groups` | `Group.Read.All` | |
| `apps` | `Application.Read.All` | |
| `devices` | `Device.Read.All` | |
| `licenses` | `Organization.Read.All` | |
| `health` | `ServiceHealth.Read.All` | The service issues part is optional |
| `authmethods` | `UserAuthenticationMethod.Read.All` or `AuditLog.Read.All` | To be confirmed: the permission this report requires is documented differently in two places. Grant the first; if the refusal stays, grant the second |
| `apppermissions` | `Directory.Read.All` | |
| `pim` | `RoleManagement.Read.Directory` | Needs Microsoft Entra ID P2 |

The permissions are read-only. A minimal setup for the default modules is `Application.Read.All`, `Organization.Read.All`, `User.Read.All` and `ServiceHealth.Read.All`.

- Outbound HTTPS from the agent host to `login.microsoftonline.com` and `graph.microsoft.com`.

## Shared credentials

To avoid repeating the identity of the app registration in every entry, set it once in the agent's environment. It serves every Azure probe. A probe that sets a key itself keeps its own value for that key.

| Variable | Replaces |
|---|---|
| `SENHUB_AZURE_TENANT_ID` | `tenant_id` |
| `SENHUB_AZURE_CLIENT_ID` | `client_id` |
| `SENHUB_AZURE_CLIENT_SECRET` | `client_secret` |
| `SENHUB_AZURE_CLIENT_SECRET_FILE` | path of a file holding the secret (a mounted secret, for example) |

The secret is taken from the first of these that is set: `client_secret`, `client_secret_file`, `SENHUB_AZURE_CLIENT_SECRET`, `SENHUB_AZURE_CLIENT_SECRET_FILE`. A secret file that cannot be read is an error, not a fallback. The variables are read when the probe is configured, so changing them needs a restart. This probe needs no subscription.

# Configuration Parameters

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `tenant_id` | In practice | - | Entra ID tenant (directory) ID; required unless `SENHUB_AZURE_TENANT_ID` is set |
| `client_id` | In practice | - | Application (client) ID of the app registration; required unless `SENHUB_AZURE_CLIENT_ID` is set |
| `client_secret` | In practice | - | Client secret of the app registration; one of `client_secret`, `client_secret_file`, `SENHUB_AZURE_CLIENT_SECRET` or `SENHUB_AZURE_CLIENT_SECRET_FILE` is required, in that order. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` |
| `client_secret_file` | No | - | File holding the client secret; used when `client_secret` is not set |
| `modules` | No | `sp`, `sync`, `users`, `licenses`, `health` | Modules to run: `sp`, `sync`, `syncapp`, `users`, `security`, `groups`, `apps`, `devices`, `licenses`, `health`, `authmethods`, `apppermissions`, `pim`. An unknown name is refused |
| `interval` | No | `300` | Seconds between collections |
| `timeout` | No | `30` | Per-request timeout in seconds for calls to Microsoft Graph |
| `auth_mode` | No | `overview` | What `sp` reports: `overview` counts credentials by type and time to expiry; `cert`, `secret` or `certsecret` publish the time left of each credential |
| `app_scope` | No | `registrations` | Where `sp` reads credentials: `registrations` (app registrations), `enterprise` (enterprise applications, which hold SAML signing certificates) or `both` |
| `include` | No | - | Report only applications whose display name or application ID contains one of these (case-insensitive) |
| `exclude` | No | - | Skip applications whose display name or application ID contains one of these (case-insensitive); wins over `include` |
| `sp_id` | No | - | Object ID of the provisioning service principal read by `syncapp`; empty finds the `ConnectSyncProvisioning` application |

Choose `auth_mode` with care: `cert`, `secret` and `certsecret` publish one series per credential, with its identifier, so a tenant with thousands of credentials gets thousands of series. Narrow them with `include` and `exclude`.

# Metrics Collected

Every metric below is published once, by one module, and carries the attribute `azure.tenant.id`. Counts are published with their state in attributes, and the classes of one attribute are **disjoint**: summing a count over an attribute gives the total of that population. Values of the attributes are lower snake case.

Status metrics (`.status`) publish one point per state: `1` for the current state, `0` for the others.

## Credentials (`sp`) and directory synchronisation (`sync`)

| Metric | Unit | Attributes | Description |
|---|---|---|---|
| `azure.entra.application.credential.time_left` | `s` | `azure.entra.application.id`, `azure.entra.application.name`, `azure.entra.credential.type`, `azure.entra.credential.id`, `azure.entra.credential.usage` | Time left before a secret or certificate expires; negative once expired. With `auth_mode` `cert`, `secret` or `certsecret`. `azure.entra.credential.usage` (`verify` or `sign`) is set on certificates only |
| `azure.entra.application.credential.count` | `{credential}` | `azure.entra.credential.type`, `azure.entra.credential.expiry` | Secrets and certificates by type and time to expiry (`expired`, `under_7d`, `7d_to_30d`, `30d_to_90d`, `over_90d`). With `auth_mode: overview` |
| `azure.entra.directory_sync.age` | `s` | - | Time since the last successful directory synchronisation; absent for a cloud-only tenant |
| `azure.entra.directory_sync.status` | `{status}` | `azure.entra.directory_sync.state` | `enabled` or `disabled` |

## Provisioning job (`syncapp`)

| Metric | Unit | Attributes | Description |
|---|---|---|---|
| `azure.entra.sync_job.status` | `{status}` | `azure.entra.service_principal.id`, `azure.entra.service_principal.name`, `azure.entra.sync_job.state` | Schedule state of the provisioning job |
| `azure.entra.sync_job.execution.status` | `{status}` | same, with `azure.entra.sync_job.execution.state` | Outcome of the last execution |
| `azure.entra.sync_job.entry.count` | `{entry}` | `azure.entra.service_principal.id`, `.name`, `azure.entra.sync_job.entry.operation` | Entries escrowed, exported or imported by the last execution |
| `azure.entra.sync_job.execution.duration` | `s` | `azure.entra.service_principal.id`, `.name` | Duration of the last execution |
| `azure.entra.sync_job.execution.age` | `s` | `azure.entra.service_principal.id`, `.name` | Time since the last execution finished |

## Users (`users`) and licences (`licenses`)

| Metric | Unit | Attributes | Description |
|---|---|---|---|
| `azure.entra.user.count` | `{user}` | `azure.entra.user.state`, `azure.entra.user.type`, `azure.entra.user.license` | Users by account state, type and licence |
| `azure.entra.user.inactive.count` | `{user}` | `azure.entra.user.inactivity` (`under_30d`, `30d_to_90d`, `over_90d`, `never`), `azure.entra.user.type`, `azure.entra.user.license` | Users by time since last sign-in; the classes sum to the user total. Needs `AuditLog.Read.All` |
| `azure.entra.user.newest.age` | `s` | - | Age of the most recently created user |
| `azure.entra.license.usage` | `{license}` | `azure.entra.license.sku`, `azure.entra.license.sku.name` | Licences assigned, per paid user SKU |
| `azure.entra.license.limit` | `{license}` | same | Licences purchased, per paid user SKU |
| `azure.entra.license.utilization` | `1` | same | Licences assigned over licences purchased, 0 to 1 |

## Service health (`health`)

| Metric | Unit | Attributes | Description |
|---|---|---|---|
| `azure.entra.health.status` | `{status}` | `azure.entra.health.service`, `azure.entra.health.state` | `operational` or `degraded` state of a Microsoft Entra service |
| `azure.entra.health.issue.count` | `{issue}` | `azure.entra.health.issue.classification` | Unresolved service health issues by classification |

## Security (`security`)

| Metric | Unit | Attributes | Description |
|---|---|---|---|
| `azure.entra.directory_role.count` | `{role}` | - | Activated directory roles |
| `azure.entra.directory_role.assignment.count` | `{assignment}` | `azure.entra.directory_role.class` (`global_administrator`, `privileged`, `other`) | Members of directory roles |
| `azure.entra.conditional_access_policy.count` | `{policy}` | `azure.entra.conditional_access_policy.state` | Conditional Access policies by state |
| `azure.entra.risky_user.count` | `{user}` | `azure.entra.risky_user.level`, `azure.entra.risky_user.state` | Users flagged by Identity Protection, by risk level and state |

## Groups, applications and devices (`groups`, `apps`, `apppermissions`, `devices`)

| Metric | Unit | Attributes | Description |
|---|---|---|---|
| `azure.entra.group.count` | `{group}` | `azure.entra.group.type` (`security`, `mail_enabled_security`, `distribution`, `microsoft_365`, `other`), `azure.entra.group.membership` (`dynamic`, `assigned`), `azure.entra.group.teams_enabled` | Groups; the classes sum to the group total |
| `azure.entra.application.count` | `{application}` | `azure.entra.application.audience`, `azure.entra.application.credential.composition`, `azure.entra.application.credential.state` (`expired`, `expiring` within 30 days, `valid`, `none`) | App registrations |
| `azure.entra.service_principal.count` | `{service_principal}` | `azure.entra.service_principal.permission.risk` (`high`, `standard`) | Service principals holding OAuth2 permission grants, by the highest risk of their permissions |
| `azure.entra.application.permission.count` | `{permission}` | `azure.entra.permission.type`, `azure.entra.permission.consent`, `azure.entra.permission.category` (`mail`, `directory_write`, `other`), `azure.entra.permission.risk` | OAuth2 delegated permissions granted, one per scope of each grant |
| `azure.entra.device.count` | `{device}` | `azure.entra.device.os`, `azure.entra.device.compliance`, `azure.entra.device.management`, `azure.entra.device.trust_type`, `azure.entra.device.activity` (`active`, or `stale` when the device has not signed in for 30 days) | Devices |

## Authentication methods (`authmethods`) and PIM (`pim`)

| Metric | Unit | Attributes | Description |
|---|---|---|---|
| `azure.entra.user.authentication_method.count` | `{user}` | `azure.entra.authentication_method.type` | Users with a given authentication method registered |
| `azure.entra.user.authentication_capability.count` | `{user}` | `azure.entra.user.mfa`, `azure.entra.user.passwordless`, `azure.entra.user.sspr`, `azure.entra.user.privilege` | Users by MFA state, passwordless capability, self-service password reset registration and privilege |
| `azure.entra.pim.assignment.count` | `{assignment}` | `azure.entra.pim.assignment.state` (`eligible`, `active`), `azure.entra.pim.assignment.expiration` (`permanent`, `time_bound`), `azure.entra.directory_role.class` | Role assignments by PIM state, expiration and role class |

## The probe itself

| Metric | Unit | Attributes | Description |
|---|---|---|---|
| `senhub.probe.check.result` | `{status}` | `senhub.entra_id.module` | Why the last collection of a module failed: `0` ok, `1` target down, `2` authentication denied, `3` timeout, `6` protocol error, `7` permission denied |

# Troubleshooting

A refusal from Microsoft Graph (HTTP 403) is logged with the module, the call that was refused and what to grant, in this form:

> module "users" (sign-in activity): Microsoft Graph refused GET ... The app registration needs the Application permission AuditLog.Read.All (type Application, not Delegated) with admin consent granted in Entra ID (App registrations > API permissions > Grant admin consent)

- **The permission is named, and so is the admin consent.** The most common cause of a 403 with the right permission listed is that consent was not granted: the permission shows in the portal but its status is not *Granted*.
- **Optional parts degrade, they do not fail.** When a refused call is an optional part of a module (sign-in activity, licence SKUs, directory roles, Conditional Access policies, risky users, service issues, PIM eligible or active assignments), the rest of the module is still collected and one warning says which series are not published.
- **Licence-dependent parts.** Sign-in activity needs Microsoft Entra ID P1 or P2, and Identity Protection and PIM need P2. A tenant without the licence is refused even with the permission granted.
- **`authmethods`.** The refusal message for this module says the exact permission is to be confirmed; grant the first one it names and try the second if the refusal stays.
- `senhub.probe.check.result` with the value `7` on a module is the same refusal, as a series you can alert on.

# Outputs

The metrics are available through every configured output. For PRTG and Nagios, query the probe by its configured `name`:

```bash
curl "http://localhost:8080/api/{agentkey}/prtg/metrics/entra-prod"
curl "http://localhost:8080/api/{agentkey}/nagios/metrics/entra-prod"
```

This probe publishes no entities: the metrics carry the tenant.
