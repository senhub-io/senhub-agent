<img src="https://api.iconify.design/mdi/lan-connect.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier.

# SNMP Trap Probe

The `snmp_trap` probe runs an SNMP trap receiver inside the agent:
devices push traps and informs to it over UDP, and each trap becomes
a structured OTel log record shipped through the
[OTLP storage](../otlp.md). SNMPv2c and SNMPv3 (USM) are supported;
informs are acknowledged.

OID-to-name resolution uses operator-supplied MIB files — the agent
never fetches MIBs over the network. The six generic SNMPv2-MIB
traps (coldStart, linkDown, linkUp, ...) resolve out of the box.

## Quick start

```yaml
# probes.d/10-snmp-trap.yaml — each file under probes.d/ is a YAML array of probes
- name: trap-receiver
  type: snmp_trap
  params:
    bind_address: "0.0.0.0:162"
    community: ${secret:trap-receiver.community}   # OS secret store; inline plaintext is auto-sealed on install
    mib_paths:
      - /etc/senhub-agent/mibs
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `bind_address` | No | `127.0.0.1:162` | UDP listen address; port 162 needs root or CAP_NET_BIND_SERVICE |
| `version` | No | `v2c` | A string. One of `v2c`, `v3` |
| `community` | No | - | v2c community check; empty accepts any. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `mib_paths` | No | - | Local MIB files or folders for OID names |
| `v3` | No | - | SNMPv3 users |
| `v3.users` | Yes | - | A list of blocks |
| `v3.users[].username` | Yes | - | A string |
| `v3.users[].auth_protocol` | No | - | A string. One of `MD5`, `SHA`, `SHA224`, `SHA256`, `SHA384`, `SHA512` |
| `v3.users[].auth_password` | No | - | A string. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `v3.users[].priv_protocol` | No | - | A string. One of `DES`, `AES`, `AES192`, `AES256` |
| `v3.users[].priv_password` | No | - | A string. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |

<!-- schema:params:end -->

The listener is on loopback by default; traps from network devices need
an explicit address such as `"0.0.0.0:162"`. When the agent cannot bind a
privileged port, move to a port above 1024 and point the devices at it.
An empty `community` accepts every datagram, so set it on a production
receiver.

### SNMPv3 users

```yaml
params:
  version: v3
  v3:
    users:
      - username: trapuser
        auth_protocol: SHA256
        auth_password: "${env:TRAP_AUTH_PWD}"
        priv_protocol: AES256
        priv_password: "${env:TRAP_PRIV_PWD}"
```

Leave `auth_protocol` empty for no authentication and `priv_protocol`
empty for no privacy.

## Output

Each trap becomes one OTel log record: the trap OID (resolved to a
name when a MIB covers it), the source address, and every varbind as
an attribute. Records flow through the agent log channel like
`syslog`, `filetail` and `linux_logs` records — any storage that
consumes logs ships them.

The probe also emits two self-metrics:

| Metric | Description |
|---|---|
| `senhub.snmp_trap.rejected_community` | Datagrams rejected for community mismatch |
| `senhub.snmp_trap.decode_panics` | Malformed datagrams that crashed the decoder and were recovered |

## Operational notes

- **Event-driven.** No polling: traps arrive when devices send them.
  The bind error (port in use, missing privilege) surfaces at probe
  start, not silently at runtime.
- **Hostile input is survivable.** A datagram that panics the
  decoder is dropped, counted in `decode_panics`, and never takes
  the receiver down.
- **SNMPv3 caveat.** The upstream SNMP library flags v3 trap
  handling as best-effort; the first configured user is used for
  decryption. v2c is the battle-tested path.
- **Set the community.** An empty `community` accepts every
  datagram. The `rejected_community` counter tells you if devices
  are sending with the wrong string.
