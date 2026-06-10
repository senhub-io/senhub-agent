# snmp_poll — SNMP collection & OTel mapping design (#156)

How the `snmp_poll` probe turns polled SNMP data into OTel-first output —
both **metrics** (interfaces, hardware) and **entities/relations**
(topology) — without hand-mapping thousands of OIDs, by reusing the SNMP
MIB corpus and the existing entity rail (#185).

> **Status:** design. Lot 1a (skeleton: explicit OID list, config-supplied
> names/types) landed on `feat/issue-156-snmp-poll`. gosnmp v1.43.2 in
> go.mod. This doc now also covers the hardware and topology scope and the
> two-rail architecture they require.

## What snmp_poll collects — four data classes

The agent is not only a network-traffic poller. A managed device exposes
four distinct classes of information, and **two of them are not numeric
metrics** — they are inventory and relationships:

| Class | MIBs / tables | Data shape | Output rail |
|---|---|---|---|
| **Traffic / interfaces** | IF-MIB (ifTable, ifXTable) | counters, gauges, status enums | **metric** |
| **Hardware state** | ENTITY-SENSOR-MIB (temp/fan/voltage), HOST-RESOURCES-MIB (cpu/mem/storage), ENTITY-MIB (physical inventory) | numeric values + health enums; inventory | **metric** (+ device **entity** attributes) |
| **Activated features beyond the primary role** | sysServices, sysObjectID → device profile, "which standard MIBs answer" | capability / presence | **discovery / profile** → device entity attributes |
| **Topology** | ipNetToMediaTable (ARP: IP↔MAC↔ifIndex), ipCidrRouteTable / ipForwardTable (routing), dot1dTpFdbTable + dot1qTpFdbTable (bridge FDB: MAC↔port), lldpRemTable (LLDP neighbours) | **relations** keyed by MAC / IP / port | **entity / relation** (#185 → Toise) |

The strategic value of the topology class is the **edges**: the agent
becomes a sensor of the infrastructure graph (ENTITY-DETECTION.md §1), not
just a reporter of counters. That graph is what Toise consumes.

## The two rails (core architectural decision)

A polled OID row is routed to one of two rails. A single abstraction must
support both from day one, or the topology class gets painted out (a
metric-only model drops the non-numeric varbinds that *are* the topology).

1. **Metric rail** — `datapoint.DataPoint` through the existing data_store
   → otelmapper → PRTG / Nagios / Prometheus / OTLP. Numeric values:
   interface counters, sensor readings, host-resources, status enums.

2. **Entity rail** — `entity.Event` through the existing entity emitter
   (`internal/agent/services/entity/`): `entity.PublishEvent(ev)` →
   `entityPump` → OTLP log signal, frozen against the Toise contract
   (ENTITY-DETECTION.md §4). Non-numeric, relationship-shaped data: the
   polled device as a `network.device` entity, and ARP/route/FDB/LLDP rows
   as typed relations. **No new transport or encoding** — snmp_poll
   implements `entity.Source` and returns a snapshot each cycle; the
   tracker handles state/delete liveness.

The probe therefore has a metric collector (rail 1) and registers an
`entity.Source` (rail 2). They share the gosnmp client and poll cycle but
emit on independent rails.

**The two rails are correlated by shared identity.** The entity source resolves
the device id once per topology sweep and caches it (+ the ifIndex→ifName map);
the metric collector tags every datapoint with `network.device.id` and, on
interface metrics, `interface.name` (resolved from `if_index`). So a device's
interface-traffic metric carries the **same identity** as its
`network.interface` entity — a backend joins the traffic to the topology node.
The device id / interface names are empty until the first sweep (the tags are
omitted, never empty-valued); the sweep runs before `collect` so the metrics of
the same cycle carry them.

## The MIB-module registry

To keep the probe extensible across the four classes without rewriting the
core, collection is organised as a registry of **MIB modules**. A module
declares the OID columns it reads and, per column, how the value is mapped
**and onto which rail**:

```
type oidColumn struct {
    OID        string      // base OID, dotted, no leading dot
    Walk       bool        // BulkWalk a table column vs Get a scalar (.0)
    IndexLabel string      // row-index attribute name for walked columns

    Rail       rail        // railMetric | railEntity
    // metric rail:
    Metric     string      // internal name → snmp_poll transformer YAML
    Kind       metricKind  // counter | gauge (drives probe-side tagging)
    // entity rail:
    Relation   string      // adjacent_to | routes_via | forwards_to | …
    // (+ how the row's key columns become from/to entity ids)
}
```

Built-in modules are selected by name in config (`mibs: [if-mib, entity-sensor,
lldp]`); `custom_mappings:` covers operator OIDs without a built-in module.
Each built-in module embeds its OID→name/type table at build time — never
fetched at runtime (see Vendor-neutrality).

## Layer 1 — MIB resolution (reuses the corpus)

SNMP metrics are dynamically named (the OID set depends on the device). A
MIB resolves an OID to a **symbolic name + SNMP type (SYNTAX) + units +
enums** — e.g. `1.3.6.1.2.1.31.1.1.1.6 → ifHCInOctets, Counter64`. It does
**not** carry OTel semantics — that is our layer.

SenHub mirrors a LibreNMS-derived **raw MIB repository** at
`eu-west-1.intake.senhub.io/mibs/`: standard RFC MIBs at root (IF-MIB,
IP-MIB, ENTITY-SENSOR-MIB, LLDP-MIB, BRIDGE-MIB…), ~300 vendor
subdirectories (cisco/, juniper/, dell/…), and an `index.json` catalogue.
The agent **embeds** the standard set (built-time compiled into modules);
operators point at a **local** directory for vendor MIBs.

## Layer 2 — OTel mapping, metric rail

- **Standard MIB objects** → a curated symbolic-name → OTel semconv
  dictionary (`ifHCInOctets → system.network.io`, `hrProcessorLoad →
  system.cpu.utilization`, ENTITY-SENSOR temp/fan/voltage → `hw.*`). Each
  built-in module's `Metric` names are declared in the `snmp_poll`
  transformer YAML with full `otel:` blocks → fixed names that export on
  OTLP/Prometheus immediately and satisfy the #189 mapping guard.
- **Everything else** (custom OIDs, long-tail objects) → deterministic
  derivation: `senhub.snmp.<symbolic_name>`, OTel type from the configured
  `type:` (counter/gauge). **Done (#207):** the probe emits each custom
  mapping under its canonical `senhub.snmp.*` name and tags it with
  `otel_type`; `otelmapper.Resolve` recognises that tag and passes the
  metric through with the declared type instead of dropping it for lack of
  an exact-match YAML row. The pass-through is probe-neutral (any probe
  pre-shaping a name + type can opt in via `otel_type`).

## Layer 2′ — entity mapping, entity rail (FROZEN with Toise 2026-06-03)

The entity vocabulary and identity are **frozen** with the Toise team
(ENTITY-DETECTION.md §0/§2, ADR 0018: identity is exact, byte-by-byte,
observer-independent, no fuzzy merge). snmp_poll reuses it, never extends it.
Toise does NOT normalize or merge ids — the producer canonicalizes and
emits exact ids per source; convergence happens because two observers of the
same device derive byte-identical ids.

- **`network.device.id` — single key, subtype-prefixed, frozen precedence:**
  `serial:<PEN>:<entPhysicalSerialNum>` > `engine:<snmpEngineID>` >
  `mac:<LLDP chassis-id, only when subtype is MAC>` > `name:<sysName>` >
  `mgmt:<ip>`. Identity does **not** anchor on LLDP (often disabled); it
  anchors on the SNMP-readable serial/engine id. Two identity-semantics
  rules on the serial rung (Toise Q1/Q2): a serial is vendor-scoped, so it is
  namespaced by the vendor **IANA PEN** read from `sysObjectID`
  (`serial:<PEN>:<serial>`); without a PEN it falls through to `engine`
  (globally unique by RFC 3411). The serial is taken only when there is
  **exactly one** `entPhysicalClass=chassis` row — a **stack** (N chassis)
  leaves it empty and uses the stack-wide `engineID` (one logical device per
  SNMP management entity, failover-stable; a master-member serial would flap),
  and this also avoids latching onto a swappable module/PSU serial. Everything
  not chosen as the id goes in **descriptive attributes** (dotted lowercase,
  frozen casing — `sys.name`, `mgmt.ip`, `device.role` from the sysServices
  bitmask, `vendor` from the PEN), never as a second identity key. These make
  the device readable in a backend instead of just its cryptic id; neighbours
  carry `sys.name`.
  Canonicalization (producer side): `mac` = lowercase hex `:`-separated;
  `engine`/`PEN` = lowercase hex / decimal; `serial`/`name` = trimmed (case
  preserved); `mgmt` = `net.IP` canonical form. All in one function:
  `resolveDeviceID` (lldp.go); identity reads in `readSelfIdentity`/
  `chassisSerial` (entity_source.go).
- **Interfaces → `network.interface` entities** (topology-as-entities, ADR
  0022, pinned with Toise #87): IF-MIB ifXTable `ifName` → one
  `network.interface` entity `{network.device.id, interface.name}` the device
  **owns** via `has_interface`, with `oper.state` (ifOperStatus) and `speed`
  (ifHighSpeed Mbit/s → bit/s) descriptive. The port inventory that anchors
  `connected_to`; `notPresent` and unnamed rows are skipped. Bounded by the
  device's port count. **DONE (#156).**
- **Interface IPs → `network.address` entities** (topology-as-entities, ADR
  0022): IP-MIB `ipAdEntIfIndex` (ipAddrTable) → one `network.address` entity
  `{network.address}` per non-loopback interface IP, `bound_to` the
  `network.interface` it sits on. The **same** `network.address {ip}` node is
  the one a host's `next_hop_via` reaches, so a host's gateway resolves to this
  device's interface — the host↔device topology join, by exact IP. **DONE
  (#156).**
- **Routing → `network.route` entities** (topology-as-entities, ADR 0022,
  pinned with Toise #87): ipCidrRouteTable / ipForwardTable → one
  `network.route` entity `{network.device.id, route.destination}` (CIDR from
  the entry index) that the device **owns** via `has_route` (mirror of
  `has_interface`), the next hop carried as a scalar `next_hop.ip` attribute
  (+ `metric`). The gateway is **not** a node — `network.address` is deferred,
  so no `mgmt:`/`mac:` device is synthesized for it. This supersedes the legacy
  `routes_via` device→next-hop edge; ARP convergence (which existed only to
  give that edge a device-typed next-hop) is therefore gone. **DONE (#156).**
- **LLDP adjacency → bare `connected_to`** (topology-as-entities, ADR 0022):
  `lldpRemTable` → one `connected_to` edge between the **local** port
  `network.interface` (named via the IF-MIB ifName for `lldpLocPortNum`,
  falling back to lldpLocPortTable) and the **remote** port
  `{remote network.device.id, remote interface.name}`. The neighbour is still
  emitted as a discovered `network.device`; the remote port entity is referenced
  (the neighbour's own poll emits it). The edge is **skipped** when a port can't
  be named by exact identity — an unanchored local port, an unresolvable
  neighbour, or a **MAC-only remote port** (no phantom port, point 7). Supersedes
  `adjacent_to` + `local_port`/`remote_port` attributes. **DONE (#156).**
- **`forwards_to` (bridge FDB) — RETIRED** (#156). The legacy device-to-device
  edge is no longer emitted, and the FDB walk is removed. The FDB gives the
  local learned port and the remote *MAC*, but **not the remote port**, so it
  cannot form a proper port-to-port `connected_to` (LLDP already gives both
  ports and is the canonical adjacency source). A future FDB use — filling in a
  `connected_to` local port where LLDP is sparse — would need remote-port
  resolution the FDB does not provide; deferred.

**Cross-source convergence (no Toise merge):** LLDP chassis MAC == FDB/ARP
MAC == `mac:<addr>` matches automatically. Without LLDP, the polled device's
`ifPhysAddress` is the bridge from `name:`/`mgmt:` to the canonical `mac:`;
promote provisional `mgmt:` ids to canonical and let the old node expire
(cascade + interval). `host` ↔ `network.device` stay distinct (different
type/id) — linked by a relation later, never merged.

**Cadence (frozen):** poll topology slower than metrics (~5–15 min); set
`otel.entity.interval` to ~3× the *topology* cadence (not the metric
cadence) so the GC doesn't expire devices between sweeps. No sampling
(partial snapshot → false deletes) — emit the complete snapshot in one OTLP
export. Relations carry no interval (edge expires with its endpoint).

## Vendor-neutrality (hard constraint)

The agent MUST NOT call `intake.senhub.io/mibs/` at runtime — that couples
the OSS, vendor-neutral agent to SenHub infrastructure
(`feedback_agent_vendor_neutral`). An earlier abandoned snmptrap WIP
(commit `da9daf5`) did exactly this (runtime MIB download + 24h cache) and
is the anti-pattern this constraint exists to prevent. Therefore:

- The agent **embeds** the standard MIB modules + the curated OTel
  dictionary at build time.
- For vendor MIBs, the operator points at a **local** directory.
- `intake.senhub.io/mibs/` is the **source** the embedded set is built from
  and where operators fetch vendor MIBs — never a runtime dependency.

## Lot breakdown

Reconciled with ENTITY-DETECTION.md §7 (where SNMP topology is its Lot 5).

- **Lot 1a** *(landed)* — package + config + gosnmp wrapper + register +
  skeleton Collect + registration cluster + tests. Explicit OID list,
  config-supplied names/types, `senhub.snmp.up` self-metric. No MIB
  resolution, metric rail only.
- **Lot 1b** — MIB-module registry + embedded IF-MIB module (BulkWalk of
  ifTable/ifXTable → fixed OTel metric names) + the `senhub.snmp.*`
  typed pass-through on OTLP/Prometheus *(landed, #207)*: custom mappings
  export under their canonical name + configured type instead of being
  dropped. Metric rail, fully OTel-exported.
- **Lot 2** — hardware modules: ENTITY-SENSOR-MIB + HOST-RESOURCES-MIB →
  `hw.*` / `system.*` metrics; ENTITY-MIB inventory → device entity
  attributes. Local vendor-MIB directory loading.
- **Lot 3** — device discovery (sysObjectID → vendor) + device profiles +
  the "activated features" class → `network.device` entity attributes.
- **Lot 5 (topology → entity rail)** — emitting `network.device` +
  `network.route` (`has_route`) + `network.interface` ports (`has_interface`) +
  `connected_to` port-to-port adjacency (superseding `adjacent_to`; `forwards_to`
  retired) + interface IPs as `network.address` (`bound_to`) via `entity.Source`
  (**done #156**). This is the vendor-neutral infrastructure-graph wedge.

Tiering (`project_tiering_strategy`): interface collection + basic system +
topology/entity are the **FREE** universal-collection wedge; deep
vendor-specific infra stays paid.

## Open sub-questions
- MIB parsing in Go: build-time compile of the standard set into modules
  (lean) vs. a runtime SMI parser for operator vendor dirs (gosmi).
- Counter rate vs cumulative (emit cumulative; let the backend rate).
- `network.device.id` resolution order when LLDP is absent (sysName vs
  mgmt IP) — must stay observer-independent.
- Relation cardinality at scale: a large FDB/ARP table can be thousands of
  edges per cycle; needs the tracker's state/delete + emission throttling.
