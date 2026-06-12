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
probes:
  - name: trap-receiver
    type: snmp_trap
    params:
      bind_address: "0.0.0.0:162"
      community: "${env:SNMP_COMMUNITY}"
      mib_paths:
        - /etc/senhub-agent/mibs
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `bind_address` | `127.0.0.1:162` | UDP listen address. Loopback by default — receiving traps from network devices requires an explicit address (e.g. `"0.0.0.0:162"`). Port 162 is privileged: run as root or grant `CAP_NET_BIND_SERVICE`, or move to a port above 1024 |
| `version` | `v2c` | `v2c` or `v3` |
| `community` | empty | v2c community check. Empty accepts any community — always set it on production receivers |
| `mib_paths` | `[]` | Local directories or files of MIB modules for OID-to-name resolution |
| `v3` | none | SNMPv3 USM users (see below) |

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

| Field | Description |
|---|---|
| `username` | required |
| `auth_protocol` | `MD5`, `SHA`, `SHA224`, `SHA256`, `SHA384`, `SHA512`, or empty for no authentication |
| `auth_password` | Authentication passphrase |
| `priv_protocol` | `DES`, `AES`, `AES192`, `AES256`, or empty for no privacy |
| `priv_password` | Privacy passphrase |

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
