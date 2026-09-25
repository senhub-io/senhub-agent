<img src="../../assets/probe-logos/exchange_online.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The Exchange Online probe monitors a Microsoft 365 tenant's Exchange Online service through the Microsoft 365 reporting and Graph APIs, providing mail-flow volumes, mailbox counts and storage, service-health status, and tenant reachability. Authentication uses an Azure AD app registration (client credentials); no user account or password is stored. One probe instance monitors one tenant; add more instances for additional tenants.

**Collected data:**

- Tenant reachability (did the reporting API answer this cycle)
- Exchange service-health status, per reported service
- Mail flow over the reporting window: messages sent, received, delivered and failed
- Mailbox counts (total and active)
- Aggregate mailbox storage consumed, in bytes
- Mailboxes over their warning quota

All metrics are emitted under the `senhub.exchange_online.*` namespace. Mail-flow
metrics are cumulative counters over the reporting window; mailbox storage is
reported in bytes.

# Quick Start

## Basic Configuration

```yaml
# probes.d/40-exchange-online.yaml — each file under probes.d/ is a YAML array of probes
- name: exchange-online-prod
  type: exchange_online
  params:
    tenant_id: "00000000-0000-0000-0000-000000000000"
    client_id: "11111111-1111-1111-1111-111111111111"
    client_secret: "${secret:exchange-online.client_secret}"   # OS secret store; inline plaintext is auto-sealed on install
    interval: 300
```

The `${secret:...}` reference resolves the client secret from the OS-native secret store (see [Configuration](../configuration.md)). `tenant_id` and `client_id` identify the Azure AD tenant and the app registration used for authentication.

## Multiple Tenants

Monitor several tenants with separate probe instances:

```yaml
# probes.d/40-exchange-online.yaml
- name: exchange-online-contoso
  type: exchange_online
  params:
    tenant_id: "00000000-0000-0000-0000-000000000000"
    client_id: "11111111-1111-1111-1111-111111111111"
    client_secret: "${secret:exchange-online-contoso.client_secret}"
    interval: 300

- name: exchange-online-fabrikam
  type: exchange_online
  params:
    tenant_id: "22222222-2222-2222-2222-222222222222"
    client_id: "33333333-3333-3333-3333-333333333333"
    client_secret: "${secret:exchange-online-fabrikam.client_secret}"
    interval: 600
```

# Configuration Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:f60ac9e2103cbfe7c914e25f2b2e303a02b505bf74d5a40ccc078530724ca256 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `tenant_id` | Yes | - | Entra ID tenant (directory) ID |
| `client_id` | Yes | - | Application (client) ID of the app registration |
| `client_secret` | Yes | - | Client secret of the app registration. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `interval` | No | `300` | Seconds between collections |

<!-- schema:params:end -->

# Metrics Collected

## Overview

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.exchange_online.up` | `1` | `1` when the Microsoft 365 reporting API answered this cycle, else `0` |
| `senhub.exchange_online.service.health` | `1` | Exchange service-health status (Healthy=2, Degraded=1, Error/other=0), split by `senhub.exchange_online.service.display_name` |

## Mail Flow

Mail-flow metrics are **cumulative counters** reported over the Microsoft 365 reporting window.

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.exchange_online.mail.sent` | `{mail}` | Messages sent over the reporting window |
| `senhub.exchange_online.mail.received` | `{mail}` | Messages received over the reporting window |
| `senhub.exchange_online.mail.delivered` | `{mail}` | Messages delivered over the reporting window |
| `senhub.exchange_online.mail.failed` | `{mail}` | Messages that failed delivery over the reporting window |

## Mailboxes

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.exchange_online.mailboxes` | `{mailbox}` | Total number of mailboxes |
| `senhub.exchange_online.mailboxes.active` | `{mailbox}` | Number of active mailboxes |
| `senhub.exchange_online.mailbox.storage.used` | `By` | Total storage consumed across all mailboxes, in bytes |
| `senhub.exchange_online.mailbox.quota_exceeded` | `{mailbox}` | Mailboxes that have exceeded their warning quota |

# Requirements

- An **Azure AD app registration** in the monitored tenant, used for client-credentials (application) authentication.
- A **client secret** issued for that app registration (referenced via `client_secret`).
- **Application API permissions** granted to the app registration, with tenant admin consent:
    - `Reports.Read.All` — mail-flow, mailbox counts and storage from the Microsoft 365 reporting API.
    - `ServiceHealth.Read.All` — Exchange service-health status.
- Outbound HTTPS (port 443) from the agent host to the Microsoft 365 endpoints (`login.microsoftonline.com` and `graph.microsoft.com`).

!!! note "Reporting API delay"
    The Microsoft 365 reporting API aggregates mail-flow and mailbox usage over a
    reporting window and publishes with a delay; values reflect the most recent
    window Microsoft has finalized rather than the current instant.

# Outputs

Exchange Online metrics are available through every configured output — OTLP, Prometheus, and the pull formats (PRTG, Nagios, Web UI). For PRTG and Nagios, query the probe by its configured `name`:

```bash
curl "http://localhost:8080/api/{agentkey}/prtg/metrics/exchange-online"
curl "http://localhost:8080/api/{agentkey}/nagios/metrics/exchange-online"
```

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
| `senhub.exchange_online.up` | `exchange_online_up` | Service Reachable | # | 1 when the Microsoft 365 reporting API answered this cycle, else 0 |
| `senhub.exchange_online.service.health` | `exchange_online_service_health` | Service Health ({service_display_name}) | # | Exchange service-health status (Healthy=2, Degraded=1, Error/other=0) |
| `senhub.exchange_online.mail.sent` | `exchange_online_mail_sent` | Mail Sent | # | Messages sent over the reporting window |
| `senhub.exchange_online.mail.received` | `exchange_online_mail_received` | Mail Received | # | Messages received over the reporting window |
| `senhub.exchange_online.mail.delivered` | `exchange_online_mail_delivered` | Mail Delivered | # | Messages delivered over the reporting window |
| `senhub.exchange_online.mail.failed` | `exchange_online_mail_failed` | Mail Failed | # | Messages that failed delivery over the reporting window |
| `senhub.exchange_online.mailboxes` | `exchange_online_mailboxes` | Mailboxes Total | # | Total number of mailboxes |
| `senhub.exchange_online.mailboxes.active` | `exchange_online_mailboxes_active` | Mailboxes Active | # | Number of active mailboxes |
| `senhub.exchange_online.mailbox.storage.used` | `exchange_online_mailbox_storage_used` | Mailbox Storage Used | Bytes | Total storage consumed across all mailboxes |
| `senhub.exchange_online.mailbox.quota_exceeded` | `exchange_online_mailbox_quota_exceeded` | Mailboxes Over Quota | # | Number of mailboxes that have exceeded their warning quota |

<!-- schema:metrics:end -->
