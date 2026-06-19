<img src="https://cdn.simpleicons.org/ubiquiti" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# UniFi Controller

The `unifi` probe monitors a Ubiquiti UniFi Controller via its REST API (cookie
session authentication, stdlib HTTP), reporting device inventory, per-device
CPU and memory utilization, AP client counts and satisfaction scores, WAN
throughput and connected-client totals.

## Quick start

```yaml
probes:
  - name: unifi
    type: unifi
    params:
      endpoint: https://localhost:8443
      username: readonly
      password: changeme
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `https://localhost:8443` | UniFi Controller base URL |
| `username` | required | Controller local user username |
| `password` | required | Controller local user password |
| `site` | `default` | Controller site name to monitor |
| `verify_tls` | `true` | Set to `false` to accept self-signed certificates (lab use only) |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.unifi.up` | 1 | 1 when the controller answered login and stat endpoints |
| `unifi.devices.total` | {device} | Known devices by type (uap/usw/ugw), tagged with `device_type` |
| `unifi.devices.adopted` | {device} | Adopted devices by type |
| `unifi.clients.connected` | {client} | Currently connected wireless and wired clients |
| `unifi.device.cpu.utilization` | 1 | CPU utilization ratio per device, tagged with `device_name` / `device_type` |
| `unifi.device.memory.utilization` | 1 | Memory utilization ratio per device |
| `unifi.ap.satisfaction` | 1 | AP user experience satisfaction score (0–100 normalized to 0–1), per AP |
| `unifi.ap.clients.connected` | {client} | Clients associated per AP |
| `unifi.wan.bytes.received` | By | WAN bytes received (gateway devices, monotonic) |
| `unifi.wan.bytes.sent` | By | WAN bytes sent |

## Operational notes

- Create a read-only local user in the UniFi Controller under **Settings → Admins**. The "Read Only" role is sufficient.
- For UniFi OS (UDM/UDR), use `https://<controller>/proxy/network` as the endpoint, not the legacy `:8443` port.
- `verify_tls: false` should only be used for home lab controllers with self-signed certificates. In production, install a valid certificate.
