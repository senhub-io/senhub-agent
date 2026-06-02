# snmp_poll — OID → OTel mapping design (#156)

How the `snmp_poll` probe turns polled SNMP OIDs into OTel-first metrics
without hand-mapping thousands of OIDs, by reusing the SNMP MIB corpus.

> **Status:** design (pre-implementation). Tracks #156. The probe itself is
> not yet built — branch `feat/issue-156-snmp-poll`, gosnmp v1.43.2 confirmed
> fetchable.

## The problem

SNMP metrics are dynamically named (the OID set depends on the device). But
the OTel-first principle + the metric-mapping guard test (#198, #137) require
every emitted metric to resolve to an OTel name + type. Hand-writing OID→OTel
for every device is unbounded. The MIB corpus solves the hard part.

## The MIB corpus

SenHub mirrors a LibreNMS-derived **raw MIB repository** at
`eu-west-1.intake.senhub.io/mibs/`: standard RFC MIBs at root (IF-MIB, IP-MIB,
ENTITY-SENSOR-MIB, LLDP-MIB, BRIDGE-MIB…), ~300 vendor subdirectories
(cisco/, juniper/, dell/…), and an `index.json` catalogue (~86 KB). A MIB
resolves an OID to a **symbolic name + SNMP type (SYNTAX) + units + enums** —
e.g. `1.3.6.1.2.1.31.1.1.1.6 → ifHCInOctets, Counter64`. It does **not**
carry OTel semantics — that is our layer.

## Two layers

1. **MIB resolution** (reuses the corpus): resolve each polled OID to
   `(symbolicName, snmpType, units)` using the standard MIBs the agent embeds
   plus any vendor-MIB directory the operator configures.

2. **OTel mapping** (our small, finite layer):
   - **Standard MIB objects** → a curated symbolic-name → OTel semconv
     dictionary (`ifHCInOctets → system.network.io`, `ifOperStatus →` a state
     gauge, `hrProcessorLoad → system.cpu.utilization`, …). Dozens of entries,
     hand-curated, lives with the probe (YAML / Go map).
   - **Everything else** → deterministic derivation:
     `senhub.snmp.<symbolic_name>` with the OTel type derived from the SNMP
     SYNTAX (Counter32/64 → counter, Gauge32 → gauge, Integer → gauge).

Result: **every polled OID gets a valid OTel name + type automatically** —
the standard set via the dictionary, the long tail via the `senhub.snmp.*`
fallback. OTel-first holds and the guard test is satisfied without enumerating
the universe of OIDs.

## Vendor-neutrality (hard constraint)

The agent MUST NOT call `intake.senhub.io/mibs/` at runtime — that would
couple the OSS, vendor-neutral agent to SenHub infrastructure
(`feedback_agent_vendor_neutral`). Therefore:

- The agent **embeds** the standard MIB set + the curated OTel dictionary.
- For vendor MIBs, the operator points `snmp_poll` at a **local MIB
  directory** (their own, or downloaded from the intake repo) → resolved to
  `senhub.snmp.*`.
- `intake.senhub.io/mibs/` is the **source** the embedded set is built from
  and where operators fetch vendor MIBs — never a runtime dependency.

## Lot breakdown
- **Lot 1a** — package + config + gosnmp client wrapper + register + skeleton
  Collect + the registration cluster + tests (no MIB resolution yet; poll an
  explicit OID list with config-supplied names/types).
- **Lot 1b** — embedded standard MIB resolution + the curated OTel dictionary
  (IF-MIB, HOST-RESOURCES, ENTITY-SENSOR) + the `senhub.snmp.*` fallback.
- **Lot 2** — local vendor-MIB directory loading (parse MIBs → resolution).
- **Lot 3** — device discovery (sysObjectID → vendor) + device profiles.
- **Lot 5 (topology)** — LLDP/BRIDGE-FDB/ipCidrRoute → entity-detection (#185).

## Open sub-questions
- MIB parsing in Go: parse raw MIBs at load (a Go SMI parser) vs. a build-time
  tool that compiles MIBs → a compact embedded resolution table (smaller
  runtime, no parser dep). Lean: build-time compile of the standard set; local
  parse only for operator vendor dirs.
- How `senhub.snmp.*` names sanitize symbolic names to OTel-valid metric names.
- Counter rate vs cumulative (emit cumulative counter; let the backend rate).
