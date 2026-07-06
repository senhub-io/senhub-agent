# SNMP Trap Probe

The `snmp_trap` probe runs a UDP receiver for **SNMP v2c / v3 traps and
informs** emitted by network equipment (switches, UPS, industrial gear),
decodes them, and ships each as an OTel **log** record. It is the push
counterpart of the `snmp_poll` probe (asynchronous events vs polling) and
reuses the same gosnmp engine. Part of the **Free tier**.

## Configuration

```yaml
# probes.d/30-snmp_trap.yaml — each file under probes.d/ is a YAML array of probes
# v2c
- type: snmp_trap
  name: trap_receiver
  params:
    bind_address: "0.0.0.0:162"
    version: "v2c"
    community: "${file:/etc/senhub/snmp_community}"

# v3 (USM)
- type: snmp_trap
  name: trap_receiver_v3
  params:
    bind_address: "0.0.0.0:162"
    version: "v3"
    v3:
      users:
        - username: "trap_user"
          auth_protocol: "SHA"      # MD5|SHA|SHA224|SHA256|SHA384|SHA512
          auth_password: "${file:/etc/senhub/trap_auth}"
          priv_protocol: "AES"      # DES|AES|AES192|AES256
          priv_password: "${file:/etc/senhub/trap_priv}"
```

| Key | Type | Default | Notes |
|---|---|---|---|
| `bind_address` | string | `0.0.0.0:162` | UDP listen `host:port`. |
| `version` | string | `v2c` | `v2c` or `v3`. |
| `community` | string | — | v2c community (reference from a chmod-600 file, never inline). |
| `v3.users` | list | — | USM credentials; v3 requires at least one. |
| `mib_paths` | list | — | Local directories/files of operator MIBs, loaded at startup to resolve trap + varbind OIDs to names. |

> **Port 162 is privileged** (<1024): binding the default needs root or
> `CAP_NET_BIND_SERVICE` (see [#223](https://github.com/senhub-io/senhub-agent/issues/223)).
> For an unprivileged setup, bind a high port (e.g. `0.0.0.0:16200`) and
> have senders/forwarders target it.
>
> Never put the community or v3 passwords inline — reference a chmod-600
> file via `${file:...}`.

## Output

OTel logs only — no metrics. Enable the OTLP logs signal so records are
consumed:

```yaml
# strategies.d/40-otlp.yaml
otlp:
  signals:
    logs: true
```

Each record carries:

- **Body** — e.g. `SNMP trap linkDown (1.3.6.1.6.3.1.1.5.3) from 10.0.0.9 with 1 varbind(s)`.
- **Severity** — heuristic from the trap kind (`linkDown` / `authenticationFailure` / `egpNeighborLoss` → WARN; others INFO).
- **Attributes** — `trap_oid`, `trap_name`, `source_ip`, `snmp_version`, `sysuptime`, and one `varbind.<oid>` per binding, plus the `senhub.probe.*` producer identity.

## Trap name resolution (local MIBs, never fetched)

OID → name resolution has two layers:

1. **Built-in generic traps** — the six SNMPv2-MIB traps (`coldStart`,
   `warmStart`, `linkDown`, `linkUp`, `authenticationFailure`,
   `egpNeighborLoss`) resolve from a compiled-in table, always available.
2. **Operator-supplied local MIBs** — point `mib_paths` at directories
   (or files) of your vendor MIBs. They are parsed at startup and used to
   resolve both the trap OID (`trap_name`) and varbind OIDs (the varbind
   key becomes e.g. `varbind.ifOperStatus.3` instead of
   `varbind.1.3.6.1.2.1.2.2.1.8.3`).

```yaml
params:
  bind_address: "0.0.0.0:162"
  version: "v2c"
  community: "${file:/etc/senhub/snmp_community}"
  mib_paths:
    - "/etc/senhub/mibs"          # drop your vendor .mib/.txt files here
```

The agent **NEVER fetches MIBs over the network** — it only loads the
local files the operator provides (the abandoned runtime-fetch approach
is a documented anti-pattern). Files name themselves by their module
(`IF-MIB`, `IF-MIB.txt`, …); imports between modules resolve within the
configured paths, so include any standard MIBs your vendor MIBs import
(e.g. SNMPv2-SMI, SNMPv2-TC). An OID with no matching loaded MIB stays
numeric (`trap_name=unknown`, `varbind.<oid>`). MIB loading is shared
infrastructure (`snmpmib`) so other SNMP probes can adopt it.

## Notes / limitations

- **SNMPv3**: wired best-effort. The receiver carries a single USM
  identity (the first configured user) and upstream gosnmp flags v3 trap
  handling as not fully reliable. **v2c is the solid path.**
- **Informs**: v2c informs are acknowledged (the receiver replies with
  the matching `GetResponse`), so a sender stops after one send and no
  duplicate records are produced. v3 informs are logged but **not**
  acknowledged — their scoped PDU may be encrypted — so a v3 inform
  sender may retransmit. v2c is the solid path.
- Malformed packets do not crash the receiver — they are logged at debug
  and the loop continues.
