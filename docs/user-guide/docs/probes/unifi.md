<img src="../../assets/probe-logos/unifi.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# UniFi Controller

The `unifi` probe monitors a Ubiquiti UniFi Controller via its REST API (cookie
session authentication, stdlib HTTP), reporting device inventory, per-device
CPU and memory utilization, AP client counts and satisfaction scores, WAN
throughput and connected-client totals.

## Quick start

```yaml
# probes.d/10-unifi.yaml — each file under probes.d/ is a YAML array of probes
- name: unifi
  type: unifi
  params:
    endpoint: https://localhost:8443
    username: readonly
    password: ${secret:unifi.password}   # OS secret store; inline plaintext is auto-sealed on install
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:d5c3491ce8301ad201ef6bd26de955b36f5c9213b954d8697f9c3b06c5b047f4 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `https://localhost:8443` | Base URL of the controller |
| `username` | Yes | - | Controller local user |
| `password` | Yes | - | Controller local user's password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `site` | No | `default` | Controller site to watch |
| `verify_tls` | No | `true` | Verify the controller certificate; false accepts a self-signed one |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `15` | Request timeout in seconds |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.unifi.up` | 1 | 1 when the controller answered login and stat endpoints |
| `unifi.devices.total` | {device} | Known devices by type (uap/usw/ugw), tagged with `device_type` |
| `unifi.devices.adopted` | {device} | Adopted devices by type |
| `unifi.clients.total` | # | Connected clients, wired and wireless |
| `unifi.device.cpu` | 1 | CPU utilization ratio per device, tagged with `device_name` / `device_type` |
| `unifi.device.memory` | 1 | Memory utilization ratio per device |
| `unifi.ap.satisfaction` | 1 | AP user experience satisfaction score (0–100 normalized to 0–1), per AP |
| `unifi.ap.clients` | # | Clients associated to this access point, tagged with `device_name` |
| `unifi.network.io` | bytes | WAN byte rate reported by the controller, tagged with `direction` |

## Operational notes

- Create a read-only local user in the UniFi Controller under **Settings → Admins**. The "Read Only" role is sufficient.
- For UniFi OS (UDM/UDR), use `https://<controller>/proxy/network` as the endpoint, not the legacy `:8443` port.
- `verify_tls: false` should only be used for home lab controllers with self-signed certificates. In production, install a valid certificate.

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
| `senhub.unifi.up` | `senhub.unifi.up` | UniFi Controller Up | # | 1 when the controller answered login and the stat endpoints this cycle, 0 otherwise |
| `unifi.devices.total` | `unifi.devices.total` | UniFi {device_type} Devices | # | Number of devices of this type known to the controller |
| `unifi.devices.adopted` | `unifi.devices.adopted` | UniFi {device_type} Adopted | # | Number of adopted devices of this type |
| `unifi.devices.disconnected` | `unifi.devices.disconnected` | UniFi {device_type} Disconnected | # | Number of devices of this type not in the connected state |
| `unifi.clients.total` | `unifi.clients.total` | UniFi Clients | # | Total connected clients (wired + wireless) |
| `unifi.clients.wifi` | `unifi.clients.wifi` | UniFi WiFi Clients | # | Connected wireless clients |
| `unifi.network.io` | `unifi.network.io` | UniFi WAN IO {direction} | bytes | WAN byte rate reported by the controller, discriminated by direction (transmit/receive) |
| `unifi.device.cpu` | `unifi.device.cpu` | UniFi {device_name} CPU | % | Per-device CPU utilization percentage |
| `unifi.device.memory` | `unifi.device.memory` | UniFi {device_name} Memory | % | Per-device memory utilization percentage |
| `unifi.ap.clients` | `unifi.ap.clients` | UniFi AP {device_name} Clients | # | Clients associated to this access point |
| `unifi.ap.satisfaction` | `unifi.ap.satisfaction` | UniFi AP {device_name} Satisfaction | # | Access point experience score as a 0..1 ratio |

<!-- schema:metrics:end -->
