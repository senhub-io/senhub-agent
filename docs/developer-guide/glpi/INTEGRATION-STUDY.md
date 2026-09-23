# GLPI Inventory Integration Study — scope and decision

Status: **study, 2026-09-22.** The direction is the maintainer's:
inventory built as a projection of what the agent already knows, the
record filled as completely as the existing readers allow, software
inventory deferred. The lots, the sequencing and the estimates below are
this document's proposal. **Nothing here is scheduled or approved for
build** — the study exists so that the decision can be taken with the
constraints known, not to stand in for it.

GLPI is the reference open-source asset and ticketing platform in
France. Its server accepts an inventory document over HTTP, in a
published JSON format, from any producer — not only from its own agent.
That entry point is open and, as of this study, unused by any monitoring
agent: the existing Zabbix and Centreon integrations with GLPI create
tickets and embed views, they do not feed the inventory.

## Decision

1. **Inventory is a second rail, not an entity type.** Installed
   software, BIOS strings, DIMM lists and the rest do not become
   entities. Three reasons, in order of weight:
   - *Cardinality and liveness.* The entity rail is an append-only log
     with liveness expiry, re-keying and orphan guards. A thousand to
     three thousand packages per host across a fleet drowns the signal
     the rail exists to carry. Worse, liveness expiry means "the
     producer went silent", which for an inert row would manufacture
     false deletions.
   - *Nothing traverses it.* The rail already admits telemetry-less
     types — `network.address`, `network.route`, `network.endpoint` are
     `graph-only` in `ENTITY-TELEMETRY-CONTRACT.md`. They earn their
     place by sitting *on a path between two things*. An installed
     package is a leaf attached to one host that nothing will ever
     traverse: a list dressed as a graph.
   - *The vocabulary is not ours.* `entity.AllTypes` aliases
     `wire.EntityTypes()` from the Toise SDK. Adding a type is a
     cross-team contract change, not a local decision.

2. **The inventory document joins the entity rail by identity, never by
   content.** It is keyed by the same host identity the entity rail
   uses (`host.id`, with `hw.serial_number` as the asset-side join). A
   GLPI asset id is then a `same_as` facet of the host, exactly as the
   serial number already glues a host to its BMC facet. The document's
   *contents* stay out of the graph.

3. **Lot A fills the record as completely as the readers we already
   open allow.** The objective is not a minimal record: an operator
   opening a GLPI computer page should find it populated, not a name and
   three fields. That admits three tiers — what we already know, what
   our existing readers traverse and discard, and a short list of cheap
   new readers worth their keep on a server. Scope is fixed in "Lot A —
   filling the record" below.

4. **Lot B, software inventory, is deferred** until a prospect or a
   tender asks for it. Rationale and the one defensible angle are
   recorded in "Lot B — deferred, and why" so they are not rediscovered
   from scratch.

5. **Positioning: we are a source, not a CMDB.** Four established
   families occupy that word — ITSM CMDBs, SAM/licence tooling, pure
   discovery, and CAASM. All four are aggregators, and an aggregator is
   only as complete as its sources. We are an agent already on the
   machine producing first-hand facts. That framing is compatible with
   GLPI, iTop, ServiceNow and a CAASM platform at the same time, and it
   is what the writer-per-sink design below preserves.

6. **Licence guardrail.** `glpi-project/glpi-agent` is **GPL-2.0** and
   `glpi-project/glpi` is **GPL-3.0** — never port code from either.
   `glpi-project/inventory_format`, which holds `inventory.schema.json`,
   is **MIT**: the schema may be vendored and used to validate what we
   emit. The protocol itself is published and freely implementable.

## The rail

A second rail alongside metrics, logs, traces and entities. It carries
**documents**, not points: one payload per subject, versioned, emitted
on change.

```
Entity rail (identity, attributes, relations)
        │  identity only
        ▼
Inventory document  (neutral, sink-agnostic)
        │
   ┌────┴───────────────┬──────────────────┐
   ▼                    ▼                  ▼
GLPI JSON writer   Zabbix inventory   (iTop, OCS XML, …)
                   fields
```

Design constraints, in the order they bind:

- **Identity before content.** A document with no stable subject
  identity is worse than no document: it creates a duplicate asset
  record on every restart. The `deviceid` must derive from the agent's
  own durable identity, not from a hostname.
- **Sink-agnostic at the centre.** The mapping from a fact to a sink
  field lives in the neutral package, declaratively, in the manner of
  `transformers/definitions/*.yaml`. Adding a sink must not require
  editing Go tables by hand.
- **Emit on change, plus a floor.** Inventory is not telemetry. A full
  document on start and on a daily floor; a document on entity change
  in between. No periodic resend of unchanged content.
- **Keep the raw value.** Where a projection normalises or truncates a
  value to fit a sink's field, the neutral document keeps what the
  machine actually reported. The sink chooses; the document remembers.
- **An absent section is not an empty one.** A section that could not be
  collected must be left out of the document, never sent empty. The two
  are indistinguishable to the sink and they mean opposite things: GLPI
  reads an empty array as "this host has none of these" and clears what
  it held, so one failed collection blanks a record that was right. The
  Zabbix nameplate already applies this at field level — a fact the
  agent did not find is not sent, and its inventory field keeps its
  previous value. The same rule has to hold for a whole section.

  This is not hypothetical, and it is worse when the source is a third
  party. An Azure API asked by a role that lacks one read permission
  answers **HTTP 200 with an empty list**, not 403 — measured on our own
  tenant while building the Container Apps jobs probe. A source that is
  silenced reads as "nothing to declare" rather than "I am not allowed",
  and an inventory that trusts it will report a machine as having no
  disks, no interfaces or no software. Every collector feeding this
  document must therefore distinguish *collected and empty* from *not
  collected*, and only the first may be sent.

### Reading an identity and detecting a foundation are two things

They are easy to conflate because the host nameplate reaches a sink
through either, and the distinction decides how much of this rail stands
on its own.

**Reading an identity** is a direct call to
`common.GetHostIdentity()`. It needs nothing else running. The Zabbix
output already does exactly this in `readNameplate()`, which is why a
bench with only the HTTP and Zabbix outputs fills its inventory fields.

**Detecting a foundation** is `entity.DetectFoundation` driven by
`entity.NewDetector` — the host *as an entity*, the agent's service
instance, the `runs_on` between them, and every source registered
around them. That detector is instantiated in exactly one place:
`strategies/otlp/strategy.go`. Outside its two declarations, there is no
other caller.

The consequence for this rail is precise rather than sweeping. The
sections built from nameplate facts — `hardware`, `bios`,
`operatingsystem`, `cpus` — need only the identity read, so they work in
an agent that feeds a CMDB and nothing else. The sections built from
entities — `networks`, from `network.interface` and `network.address`,
and `virtualmachines`, from `compute.vm`, `container` and `pod` — do
not, and neither would any later use of relations.

So an agent configured for GLPI alone would today produce a record with
its identity and none of its topology. Tracked as **#932**.

The change is smaller than that reads, because everything around the
detector is already neutral. `entity.SubscribeEvents` keeps a
copy-on-write slice of subscriber channels, so a second consumer costs
nothing to add, and `model.go` states in its own header that the model
carries no OTel SDK dependency — the OTLP strategy is a consumer of the
entity events, not their owner. What is OTLP-bound is one constructor
call, not the rail.

Two possible answers: lift the detector out of the OTLP strategy into
the data store where every sink can see it, or give the inventory rail
its own foundation detection. The first is the right shape, and given
the neutrality above it is the smaller change of the two rather than the
larger. It needs deciding before A.1 starts, not during — and the
decision should keep identity reading and foundation detection apart
rather than merge them, since one is already usable without the other.

### Where the code goes, and what moves

The nameplate table that today lives in
`strategies/zabbix/template/nameplate.go` — nine facts, each tying an
entity attribute to a Zabbix inventory field — is the seed of this
model, in the wrong package. It moves into the neutral inventory
package, gains a GLPI column beside its Zabbix one, and the Zabbix
template generator reads it from there. One table, two sinks, one place
to add a third.

That move is lot A's first commit and is behaviour-preserving: the
Zabbix output must emit byte-identical items before and after. Three
things bind it, and none is optional.

**The table has three consumers, not one.** The template generator
(`template/template.go`), the output itself (`strategy.go`,
`nameplateItems`) and a guard test (`agent_items_test.go`). That test is
the only thing proving that every fact has an item, that the item
carries the right inventory field and that it is of type CHAR. It moves
with the table or keeps asserting across the new boundary — otherwise
the move quietly removes the guarantee that makes it safe.

**The sink column is a format constraint, not a label.** Zabbix
inventory fields are written *by name* — `OS`, `NAME`, `SERIALNO_A` — as
the export format spells them, never by number. A neutral package that
generalised them into "an inventory field" would lose that, and the loss
would only surface when a customer imports a template.

**Sequencing.** The Zabbix work that created the nameplate is in flight
and unpushed. This move waits until it has been regrouped, so that a
stable file is moved once rather than a moving one rebased twice.

## Lot A — filling the record

Three tiers, in the order they should be built. A.1 proves the chain
end to end and requires no collection change; A.2 and A.3 are what turn
a thin record into a full one.

## A.1 — projection of what we already know

Sections populated from the entity rail and the existing probes.
Nothing here requires a new collector.

### `hardware`

| GLPI field | Source |
|---|---|
| `name` | `host.name` |
| `chassis_type` | `host.chassis.type` |
| `memory` | `host.memory.total` |
| `vmsystem` | `host.virtualization` |

### `bios`

| GLPI field | Source |
|---|---|
| `smanufacturer` | `hw.vendor` |
| `smodel` | `hw.model` |
| `ssn` | `hw.serial_number` |

The remaining BIOS and baseboard fields come in A.2.

### `operatingsystem`

| GLPI field | Source |
|---|---|
| `name` | `os.name` |
| `version` | `os.version` |
| `full_name` | `os.description` |
| `arch` | `host.arch` |
| `kernel_version` | `os.build_id` |
| `fqdn`, `dns_domain` | host identity resolution |

### `cpus`

| GLPI field | Source |
|---|---|
| `name`, `model` | `host.cpu.model.name` |
| `manufacturer` | `host.cpu.vendor.id` |
| `corecount` | `host.cpu.physical.count` |
| `thread` | `host.cpu.logical.count` |
| `speed` | `host.cpu.frequency.nominal` |

`name` and `model` are empty on Linux **aarch64**, and no amount of
mapping will fill them: the ARM kernel publishes no `model name` line in
`/proc/cpuinfo` — only `CPU implementer`, `CPU part` and `CPU
revision`, which are numbers, not a commercial name. Verified on the
bench: zero matching lines. An x86_64 host fills the field normally, and
macOS fills it from sysctl.

This is an architecture difference, not a gap to close. A mixed fleet
will show a processor on its Intel and AMD machines and nothing on its
ARM ones, and no CMDB can invent the value. It matters here because it
is the kind of absence an operator reads as a broken integration.

### `networks`

One entry per `network.interface` entity, with its `network.address`
children folded in.

| GLPI field | Source |
|---|---|
| `mac` | interface `mac` |
| `mtu` | interface `mtu` |
| `speed` | interface `speed` |
| `type` | interface type |
| `status` | interface `oper_state` |
| `virtualdev` | interface type |
| `ipaddress`, `ipaddress6` | `network.address` entities |

This is the richest section we can fill and the one where the entity
rail pays for itself.

### `drives`

One entry per filesystem from the `logicaldisk` probe: `filesystem`,
`total`, `free`, `letter`, `type`.

Not `label`: the probe emits `device`, `mount_point` and `fs_type` on
Unix and `device` and `drive` on Windows, and a volume label is in none
of them. Filling it would mean reading `blkid` or `e2label`, which is a
new reader and therefore not A.1. It is small enough to fold into A.3 if
anyone asks for it, and nobody has.

### `virtualmachines`

From the `compute.vm`, `container` and `pod` entities emitted by the
`hyperv`, `proxmox`, `docker` and `kubernetes` probes: `name`, `uuid`,
`status`, `vcpu`, `memory`, `vmtype`.

### `versionclient`

The agent name and version, so a GLPI operator can tell which producer
wrote the record.

## A.2 — what our readers already traverse and discard

None of this is a new source. Each item is a field the agent's existing
readers walk past on their way to something else, and the cost is
keeping it rather than finding it.

| Section | Fields | Where it already comes from |
|---|---|---|
| `hardware` | `uuid` | SMBIOS table 1. **Worth doing first**: the system UUID is one of the keys GLPI reconciles an asset on, so it directly reduces duplicate records. |
| `bios` | `bversion`, `bdate`, `mmanufacturer`, `mmodel`, `msn`, `assettag` | SMBIOS tables 0 and 2. The reader already traverses them to reach the three fields A.1 keeps. |
| `memories` | `capacity`, `speed`, `type`, `formfactor`, `manufacturer`, `serialnumber`, `numslots` | SMBIOS table 17, one entry per slot. Same reader, a list where we keep a scalar today. |
| `storages` | `model`, `serial`, `firmware`, `disksize`, `interface`, `type` | The `smart` probe already runs `smartctl`; these fields are in the same output and are dropped. |
| `operatingsystem` | `boot_time`, `timezone`, `install_date`, `ssh_key` | Host identity resolution and the uptime the agent already computes. |

On Windows the SMBIOS equivalents come through the path the nameplate
reader already uses, so the shape is the same; the difference is the
source, not the design.

## A.3 — short new readers, worth their keep on a server

Three collectors that do not exist today, kept in scope because each is
small, stable, and asked for in practice.

| Section | What it reads |
|---|---|
| `local_users`, `local_groups` | `/etc/passwd` and `/etc/group`; the local account API on Windows. |
| `antivirus` | Windows Security Center. No Linux equivalent worth the name — the section stays empty there rather than being invented. |
| `firewalls` | The state of `firewalld`, `ufw` or `nftables`; the Windows firewall profile state. |

## What stays empty, and why

Naming these is part of the scope: an empty section is a decision, not
an oversight.

| Section | Why not |
|---|---|
| `softwares` | Lot B. See below. |
| `monitors`, `printers`, `usbdevices`, `videos`, `sounds`, `inputs`, `slots`, `ports`, `controllers`, `batteries`, `modems` | Desk-side inventory. This is a **target-market decision, not a cost one**: our fleet is servers and network equipment, where none of these exist or matter. A workstation-facing product would want them all, and that is not what this agent is. |
| `processes` | We have the data; a CMDB has no use for a process list. |
| `cartridges`, `consumables`, `pagecounters` | Printer-side, reachable only through the SNMP path and only for printers we are not asked to manage. |

The four administrative tasks of the reference agent — software
deployment, arbitrary file and registry collection, wake-on-LAN, remote
inventory — are **permanently out of scope**. They turn a collection
agent into a remote-execution agent, which is a security posture change
rather than a feature, and it is the posture our customers' security
teams accept today.

## Lot B — deferred, and why

Software inventory is what customers pay for: licence compliance, and
the publisher audits that give it a price. It is also the section we
cannot fill from what we already know, and the one where the field is
crowded.

What the study established, so it is not re-derived later:

- **osquery is the reference for collection**, not the reference agent.
  It exposes `deb_packages`, `rpm_packages`, `snap_packages`,
  `programs`, `chocolatey_packages`, plus language and extension
  ecosystems we would not attempt. It carries per-distribution version
  collation (`version_dpkg`, `version_rhel`) so version comparison is
  correct in SQL, and its scheduled queries already emit differentials.
  It is Apache-2.0 under the Linux Foundation. We would not beat it on
  collection, and we should not claim to.
- **What osquery does not do** is write an inventory document or feed a
  CMDB. It is a query engine and needs a separate control plane.
- **The duplicate problem is not where it looks.** GLPI's software
  schema has a `guid` field and the reference agent fills it with the
  Windows uninstall subkey — the MSI ProductCode. But the ProductCode
  changes with each major version by Microsoft convention; the stable
  cross-version key is the UpgradeCode, which osquery exposes as
  `programs.upgrade_code`, which the reference agent does not send and
  which the GLPI schema has no slot to receive. GLPI reconciles its
  `Software` objects on the **display name**, which is why the official
  answer to duplicates is a dictionary of hand-written regular
  expressions replayed over the database.
- **Therefore the only lever an agent holds is the name**, because the
  name is the key. Nobody pulls it: the reference agent forwards the
  registry string verbatim, and osquery exposes the raw value on
  purpose, fidelity being a query engine's contract. Normalising once,
  at emission, for a whole fleet is the single defensible differentiator
  — and the neutral document must then carry the raw string beside the
  normalised one, because an audit wants what the machine said.

**Trigger:** a customer or a tender that asks for installed-software
inventory. Not a market intuition. Lot A is a prerequisite either way,
so nothing is lost by waiting.

**A partial answer already exists.** The `os_updates` probe gained
`senhub.os.packages.installed` in the 0.6.0 work — the count of
installed packages, read from `dpkg-query` or `rpm`, Linux only, absent
rather than zero when the backend does not answer. It is not an
inventory, but it is the number a GLPI record shows at the head of its
software tab, and it costs nothing more to send. Worth carrying in the
document as a single fact while the list itself waits for its trigger.

## Transport

Submission is an HTTP POST of the JSON document to the GLPI server's
inventory entry point.

| Field | Value |
|---|---|
| `action` | `inventory` |
| `deviceid` | derived from the agent's durable identity |
| `itemtype` | `Computer` |
| `content` | the sections above |

Open points to settle during lot A, not before:

- **Compression and size.** The document is small without `softwares`;
  it stops being small with it. Whether we compress, and whether the
  server's partial-inventory support is worth using, is a lot B concern.
- **Authentication.** How the endpoint is protected varies by
  deployment. To be established against a real server, not from
  documentation.
- **Version comparison.** Whether we carry distribution-correct version
  semantics as osquery does, or emit strings and leave comparison to the
  sink. Current inclination is the latter — but it must be written down
  rather than defaulted into.

## Validation

`inventory.schema.json` is MIT-licensed and can be vendored. The writer
must be tested against it, and the round trip proven against a real GLPI
instance before the lot is called done — the same rule that applied to
the Zabbix output, for the same reason: a format accepted by a schema is
not a record a server actually creates.

## Effort

- **Rail** — neutral inventory package, nameplate table moved out of the
  Zabbix strategy, Zabbix output unchanged and proven so. ~2 d.
- **Detector** — lifting `entity.NewDetector` out of the OTLP strategy
  into the data store, so every sink sees the foundation (#932). ~3 d, and the
  least certain number here because it touches a component three
  outputs already depend on. **Not optional for a full record**: without
  it, A.1 delivers `hardware`, `bios`, `operatingsystem` and `cpus` and
  neither `networks` nor `virtualmachines` — that is, the identity of
  the machine and none of its topology.
- **A.1** — document builder, the projected sections, schema
  validation, GLPI writer and transport, identity and reconciliation,
  proven against a real instance. ~7 d.
- **A.2** — the discarded fields: SMBIOS tables 0, 1, 2 and 17, the
  `smartctl` identity fields, the operating-system extras. ~4 d.
- **A.3** — local accounts, antivirus, firewall state. ~4 d.

Twenty days for the full record, seventeen for a record without its
topology. Rough, with the Windows half of A.2 and the detector move
carrying the uncertainty. Lot B is not estimated here: it is a product
decision before it is an engineering one.

These numbers are the reason the status line above says what it says.
The study concludes that software inventory is what customers pay for
and then defers it; spending three weeks on what they do not pay for is
defensible — lot A is a prerequisite either way, and a GLPI record
filled by the monitoring agent is a real argument in this market — but
it is a choice, not a consequence of the ordering, and it belongs to
whoever schedules the work.

## References

- `docs/developer-guide/engineering/ENTITY-TELEMETRY-CONTRACT.md` — the
  `graph-only` classification this study leans on.
- `docs/developer-guide/zabbix/INTEGRATION-STUDY.md` — the sibling
  study, and the source of the nameplate table that moves here.
- `inventory.schema.json`, MIT, in `glpi-project/inventory_format`.
