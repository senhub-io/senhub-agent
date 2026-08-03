<img src="https://cdn.simpleicons.org/microsoftazure" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The Azure AD Connect Health probe monitors the health of your **hybrid identity directory synchronization** — the process that keeps an on-premises Active Directory in sync with Microsoft Entra ID (Azure AD). It polls the Azure AD Connect Health service through the Azure management API, using an Entra ID app registration for authentication, and reports whether directory sync is healthy, whether the sync agents are online, and how recently each agent last reported.

The probe authenticates as a service principal (app registration) with the client-credentials flow — no user sign-in and no software installed on the sync servers themselves. One probe instance covers one Entra tenant; add more instances for additional tenants.

**Collected data:**

- Reachability of the Azure AD Connect Health API
- Per sync-service health status (Healthy / Warning / Error)
- Healthy vs total registered sync agents, per sync service
- Directory-sync export error counts, bucketed by error type
- Per-agent liveness — seconds since each sync agent last reported to Azure AD Connect Health

All metrics are emitted under the `senhub.ad_hybrid.*` namespace. Sync-service and per-agent series carry attributes (`service.name`, `agent.server`, `error.bucket`) that keep instances distinct in OTLP/Prometheus and act as filters in the Web UI Sensor Builder.

# Quick Start

## Basic Configuration

```yaml
# probes.d/40-ad-hybrid.yaml — each file under probes.d/ is a YAML array of probes
- name: ad-hybrid-prod
  type: ad_hybrid
  params:
    tenant_id: "00000000-0000-0000-0000-000000000000"
    client_id: "11111111-1111-1111-1111-111111111111"
    client_secret: "${secret:ad-hybrid.client_secret}"   # OS secret store; inline plaintext is auto-sealed on install
    interval: 300
```

The `${secret:...}` reference resolves the app registration's client secret from the OS-native secret store (see [Configuration](../configuration.md)). The `tenant_id`, `client_id` and `client_secret` come from the Entra ID app registration (see [Requirements](#requirements)).

## Multiple Tenants

Monitor several tenants with separate probe instances:

```yaml
# probes.d/40-ad-hybrid.yaml
- name: ad-hybrid-corp
  type: ad_hybrid
  params:
    tenant_id: "00000000-0000-0000-0000-000000000000"
    client_id: "11111111-1111-1111-1111-111111111111"
    client_secret: "${secret:ad-hybrid-corp.client_secret}"
    interval: 300

- name: ad-hybrid-lab
  type: ad_hybrid
  params:
    tenant_id: "22222222-2222-2222-2222-222222222222"
    client_id: "33333333-3333-3333-3333-333333333333"
    client_secret: "${secret:ad-hybrid-lab.client_secret}"
    interval: 600
```

# Configuration Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `tenant_id` | string | Yes | - | Entra ID (Azure AD) tenant ID (directory GUID) |
| `client_id` | string | Yes | - | Application (client) ID of the app registration used to authenticate |
| `client_secret` | string | Yes | - | App registration client secret — reference a stored secret via `${secret:<name>.client_secret}`, `${env:VAR}` or `${file:/path}`. Inline plaintext is auto-sealed into the OS secret store on install. |
| `interval` | integer | No | `300` | Collection interval in seconds |
| `timeout` | integer | No | `30` | Per-request HTTP timeout in seconds for calls to the Azure management API |

# Metrics Collected

Sync and agent series carry attributes identifying the sync service (`senhub.ad_hybrid.service.name`) and, where applicable, the reporting agent server (`senhub.ad_hybrid.agent.server`) or the error bucket (`senhub.ad_hybrid.error.bucket`).

## Overview

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.ad_hybrid.up` | `1` | `1` when the Azure AD Connect Health API answered this cycle, else `0` |

## Sync

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.ad_hybrid.sync.health` | `1` | Sync service health (Healthy=2, Warning=1, Error/other=0), per `service.name` |
| `senhub.ad_hybrid.sync.agents.healthy` | `{agent}` | Number of sync agents reporting a healthy state, per `service.name` |
| `senhub.ad_hybrid.sync.agents.total` | `{agent}` | Total number of registered sync agents, per `service.name` |
| `senhub.ad_hybrid.sync.export_errors` | `{error}` | Directory-sync export error count, per `service.name` and `error.bucket` |

## Agents

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.ad_hybrid.agent.last_seen` | `s` | Seconds since the sync agent last reported to Azure AD Connect Health, per `service.name` and `agent.server` |

# Requirements

- **Azure AD Connect Health** enabled for the tenant (part of Microsoft Entra ID P1/P2 licensing), with the Health agents installed and reporting on your directory-sync servers.
- An **Entra ID app registration** (service principal) with a **client secret**, used for the client-credentials flow.
- The app registration must have **read access to Azure AD Connect Health** — grant it a directory read role (for example the *Global Reader* or *Reports Reader* role, or an equivalent role with read access to the Azure AD Hybrid Health service resource) so it can list sync services and agents through the Azure management API.
- Outbound HTTPS (port 443) from the agent host to the Azure management endpoint (`management.azure.com`) and the Entra ID login endpoint (`login.microsoftonline.com`).

# Outputs

Azure AD Connect Health metrics are available through every configured output — OTLP, Prometheus, and the pull formats (PRTG, Nagios, Web UI). For PRTG and Nagios, query the probe by its configured `name`:

```bash
curl "http://localhost:8080/api/{agentkey}/prtg/metrics/ad-hybrid-prod"
curl "http://localhost:8080/api/{agentkey}/nagios/metrics/ad-hybrid-prod"
```
