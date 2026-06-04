# SNMP Trap Probe

The `snmp_trap` probe runs a UDP receiver for **SNMP v2c / v3 traps and
informs** emitted by network equipment (switches, UPS, industrial gear),
decodes them, and ships each as an OTel **log** record. It is the push
counterpart of the `snmp_poll` probe (asynchronous events vs polling) and
reuses the same gosnmp engine. Part of the **Free tier**.

## Configuration

```yaml
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
storage:
  - type: otlp
    signals:
      logs: true
```

Each record carries:

- **Body** — e.g. `SNMP trap linkDown (1.3.6.1.6.3.1.1.5.3) from 10.0.0.9 with 1 varbind(s)`.
- **Severity** — heuristic from the trap kind (`linkDown` / `authenticationFailure` / `egpNeighborLoss` → WARN; others INFO).
- **Attributes** — `trap_oid`, `trap_name`, `source_ip`, `snmp_version`, `sysuptime`, and one `varbind.<oid>` per binding, plus the `senhub.probe.*` producer identity.

## Trap name resolution (no MIB runtime fetch)

The six generic SNMPv2-MIB traps (`coldStart`, `warmStart`, `linkDown`,
`linkUp`, `authenticationFailure`, `egpNeighborLoss`) resolve to a
friendly `trap_name` from a compiled-in table. The probe **does not load
or runtime-fetch MIB files** (a deliberate anti-pattern for this agent);
enterprise/vendor trap OIDs surface by their numeric `trap_oid` with
`trap_name=unknown`, and the operator maps them downstream.

## Notes / limitations

- **SNMPv3**: wired best-effort. gosnmp's trap listener carries a single
  USM identity (the first configured user) and upstream flags v3 trap
  handling as not fully reliable. **v2c is the solid path.**
- Malformed packets do not crash the receiver — a record is still emitted
  (empty `trap_oid`, `trap_name=unknown`).
