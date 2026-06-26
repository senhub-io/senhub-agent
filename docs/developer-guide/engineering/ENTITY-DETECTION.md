# Entity detection & topology discovery

Design for the agent's **entity-event emitter**: turning what the agent
knows and discovers about infrastructure into OpenTelemetry **entity
events** (and, as an explicit extension, relation events). Tracks issue
[#185](https://github.com/senhub-io/senhub-agent/issues/185).

> **Vendor-neutral.** The agent emits standard OTel entity events (OTel
> Entity Data Model). The SenHub *Toise* platform is one consumer that
> ingests them to build a temporal infrastructure graph — nothing here is
> Toise-specific. Relations are **embedded** in the source entity's state via
> the standard `entity.relationships` array (merged OTel entity-events spec),
> not a `senhub.*` / `toise.*` extension. See `feedback_agent_vendor_neutral`.

## 0. Contract status (frozen 2026-06-01)

Producer↔consumer contract negotiated with the Toise team over two rounds.
**Executable source of truth:** the Toise conformance fixture
`toise-dev/toise:internal/ingest/testdata/conformance/entity-events.json`;
specs in `docs/data-model/otel-mapping.md` + `senhub-agent-contract.md`. The
agent's encoder MUST reproduce that fixture's shapes.

Agreed:
- **Nodes = pure standard OTel entity events.** `entity_state` /
  `entity_delete`.
- **Relations = embedded `entity.relationships`** on the source entity's
  state (merged OTel entity-events spec). Bare descriptors (target only),
  retired by absence — no separate edge record, no edge delete.
- **Exact, immutable identity** — no fuzzy matching. Never put a mutable
  value (pid, leased IP) in identity; those are descriptive attributes. A
  restart is an `attribute` update, not an identity change.
- **Liveness = explicit `entity_delete` (primary) + `Interval` TTL backstop.**
  A re-emitted `entity_state` is the heartbeat. We emit `Interval`; Toise
  uses it as a sweeper safety net against lost deletes (and lost producers).
- **Flat attribute maps, scalar leaves only** — dotted keys, no sub-maps.
- **Bi-temporal:** `event_time` = LogRecord timestamp; `recorded_at` set by
  the consumer (never sent).

Open micro-points: (1) the agent→its-own-host edge: `runs_on`, `monitors`,
or both. (2) `network.device` id — frozen at Lot 5. (The earlier relation
discriminant point is closed: edges are embedded in `entity.relationships`,
lot 0b, so there is no separate relation record to discriminate.)

## 1. Two detection planes

Detection is not just *enumerating what is configured* — it is **asset and
topology discovery** from everything the probes can read.

| Plane | What it produces | Sources |
|---|---|---|
| **Inventory** | The entities the agent *is* and *targets*: host, the agent process, each configured probe target. | host self-info, agent config |
| **Discovery / topology** | Assets the agent does **not** monitor directly, plus the **relations** between assets. | host routing / ARP / neighbour tables (available now); LLDP/CDP, bridge FDB, route tables via **SNMP** (#156, later) |

The value for a temporal graph is in the **edges** (relations). Topology
discovery makes the agent a *sensor of the infrastructure graph*, not just a
reporter of its own configuration. Edges ride embedded on the source entity's
state (§4, `entity.relationships`).

## 2. Data model

### Entity
- `type` — `host`, `service.instance`, `db`, `network.device` (agreed
  vocabulary). OTel semconv type where one exists.
- `id` — the **identifying** attribute map. **Exact + immutable.** Use a
  single globally-unique key, or a composite unique key — never a tuple that
  a peer could collide on by differing in one field.
- `attributes` — descriptive (mutable) attributes.
- `event_time`, optional `interval`.

Agreed identities:
| Entity | `id` key(s) | Notes |
|---|---|---|
| `host` | `host.id` | machine-id/UUID (gopsutil HostID). `host.name`, `os.type` are descriptive. |
| `service.instance` | `service.instance.id` | the agent key. `service.name`, `service.version` descriptive. |
| `db` | `db.instance.id` | **single composite string** e.g. `pg@10.0.1.5:5432`. `db.system.name`, `server.address`, `server.port` descriptive. |
| `network.device` | `network.device.id` | LLDP chassis-id, fallback mgmt IP. Frozen at Lot 5. |

### Relation (embedded)
A typed directed edge, embedded in the source entity's state. Internally a
producer reports a flat `Relation` (`type` + `from.{type,id}` +
`to.{type,id}`); the detector folds it onto the source entity as a bare
`entity.relationships` descriptor (`relationship.type` + target `entity.type`
+ target `entity.id`). Agreed types: `runs_on`, `monitors`, `has_interface`,
`has_route`, `connected_to` (topology-as-entities, ADR 0022). The device-level
`routes_via` / `adjacent_to` / `forwards_to` are **legacy/not emitted** —
superseded by `network.route` + `has_route`, port-to-port `connected_to`, and
`connected_to` to the learned port respectively (the frontier still accepts
them, but producers emit the entity form). **Endpoints resolved by exact identity** — the
producer MUST emit the source-endpoint entity in the same observation (the
fold drops an edge with no source entity, with a warning); the target entity
is emitted before/with the edge and Toise reconciles out-of-order arrivals.
Edge attributes are **not** carried on the wire (bare embed) — a persistent
fact belongs on an entity.

## 3. Detection sources
1. **Host self-info** → the `host` entity (`common.GetHostTags()` + `host.id`).
2. **Agent config** → the `service.instance` entity + one entity per
   configured remote probe target, known at load → emitted immediately.
   Relations `service.instance --runs_on--> host` and `--monitors--> target`.
3. **Datapoint-stream resource tags** → sub-component entities. The
   transformer `tag_metadata` `type: resource` set already *is* an entity
   identity → generic synthesis, no per-probe code.
4. **Host routing table** (topology, now — #212) → the host's routes as
   `network.route` entities `{host.id, route.destination}`, attached by
   `has_route` (host → route). The gateway is a shared `network.address` node
   the route reaches via `next_hop_via` (plus a scalar `next_hop.ip`).
5. **SNMP topology MIBs** (with #156) → ports as `network.interface` entities
   (`has_interface`), link adjacency as port-to-port `connected_to`, routing as
   `network.route` + `has_route`, interface IPs as `network.address` entities
   (`bound_to`). **Host↔device join:** the host's `next_hop_via` and the
   device's `bound_to` reference the **same** `network.address {ip}` node, so a
   host's gateway resolves to the polled device's interface — the two topology
   planes reconcile by exact IP.

## 4. Encoding — LogRecords

Events travel as OTLP `LogRecord`s on the existing log rail:
`agentstate.PublishLog(rec)` → `logsPump` → `logsPipeline.emit()` → exporter.
**No new transport wiring.** One isolated encoder owns all wire shape so OTel
spec churn is contained.

**Entity event** — merged OTel entity-events spec, frozen with Toise
(#222, lots 0a + 0b). The event kind is the **LogRecord `EventName`**, not a
payload attribute; node attributes use **bare keys** (no `otel.entity.*`):
- `EventName` = `entity.state` | `entity.delete`
- `entity.type` = string
- `entity.id` = kvlist (scalar leaves, dotted keys) — **self-contained**
  (identity lives here, not referenced from the resource)
- `entity.description` = kvlist (scalar leaves; omitted on delete) — the
  descriptive attributes (formerly `otel.entity.attributes`)
- `entity.report.interval` = int **seconds** (TTL backstop; state only)
- `entity.relationships` = array of bare descriptors (state only; see below)
- LogRecord timestamp = `event_time`
- Scope flag `otel.entity.entity_event=true` still set (contrib fast-path
  filter convention; Toise ignores it). Its removal is tracked separately
  from the record encoding.

**Instrumentation scope carries provenance (#253).** Provenance — *how* a
fact was discovered — rides the OTel **instrumentation scope**, not an edge
attribute (the bare embed drops edge attributes anyway). Each entity declares
its discovery method via `entity.Entity.Scope` (an `entity.Scope*` constant);
the detector preserves it through the relationship fold and the synthesized
absence-delete, and the OTLP pump emits each method under its own `Logger`, so
a batch produces one `ScopeLogs` per method. Every method-scoped Logger keeps
the `otel.entity.entity_event=true` scope attribute, so the contrib fast-path
filter still matches. Frozen scope names (aligned with Toise):

| Scope name | Discovery method |
|---|---|
| `senhub-agent/snmp-ifmib` | polled device identity + IF-MIB ports/addresses |
| `senhub-agent/snmp-lldp`  | LLDP-discovered neighbours + `connected_to` adjacency |
| `senhub-agent/snmp-route` | device routing table (`network.route`) |
| `senhub-agent/host-route` | host kernel routing table |
| `senhub-agent/host-iface` | host interface/address inventory |
| `senhub-agent/host-svc`   | host listening-service inventory |
| `senhub-agent/host-dep`   | host outbound dependency flows |

The foundation (host + service.instance) declares no method and rides the
generic `senhub-agent/otlp-entities` scope. Toise currently ignores the entity
scope, so this is a provenance refinement, not a functional dependency.

**Relationships are embedded, not separate records** (#222, lot 0b). The
source entity's `entity.state` carries its outgoing edges in
`entity.relationships`, each a bare kvlist naming **only the target** (the
source is the carrying entity):
- `relationship.type` = string (`runs_on`, `monitors`, `routes_via`, …)
- `entity.type` = target entity type
- `entity.id` = target entity identity (kvlist, scalar leaves)

There are **no** `entity.relation.*` records and **no edge delete** on the
wire: the set is full on every heartbeat, so an edge the source stops listing
is **retired by absence**. The embedded descriptor is **bare** — no edge
attributes; a fact that must persist (a route metric, a port) belongs on an
entity, never on the edge. The detector folds the flat `Relation`s a `Source`
reports onto their source entity (matched by the `from` endpoint); a relation
whose source entity is absent this cycle is logged, never silently dropped.

Values restricted to scalars (`string`/`int64`/`double`/`bool`); maps are
flat (dotted keys), never nested (relationship descriptors are the one fixed
two-level shape). The encoder rejects/warns on a non-scalar leaf — never
silently drops.

## 5. host.id — stable host identity + telemetry correlation
gopsutil `HostID` is the host entity's `host.id`. It is **mandatory on the
entity-event record** as `entity.id = {host.id: …}` — the consumer reads
identity off the record, not the resource (a record with no `entity.id` is
rejected).

**It is also emitted on the OTLP resource** of every signal the agent produces,
via `common.GetHostResourceAttributes()` (`host.id` + `host.name` / `host.arch`
/ `os.type` / `os.description` / `os.name` / `os.version` / `os.build_id`, OTel
resource semconv) → `buildResource`. This is **not optional**: it is the join
that lets a backend correlate the host **node** in the infra graph with the
host's **metrics and logs** (same `host.id` on the entity and on the telemetry
resource). Operator `global_tags` / `resource:` Extra of the same key override
the detected value (host attrs are lowest precedence). Never emit `host.id` as a
per-datapoint tag — it belongs on the resource, not on every metric (it would
only pollute PRTG/Prometheus labels).

Remote targets (SNMP devices, DBs) do **not** ride the agent's host resource:
their identity (`network.device.id`, `db.instance.id`) is a **per-metric**
attribute, so a device's interface metric joins to its `network.interface`
entity by those metric attributes, not by the resource (which always describes
the agent's own host).

## 6. Lifecycle tracker (required)
Because liveness is delete-driven, the emitter keeps a store of currently-known
entities/relations and their last-emitted descriptive state. It emits:
- `entity_state` on first detection and on descriptive change (heartbeat on a
  configurable cadence aligned with the OTLP push interval);
- `entity_delete` when an entity is no longer detected (config removed,
  target unreachable, instance gone);
- relationships need no separate lifecycle: they ride embedded on the source
  entity's heartbeat and are retired by absence (dropped from one state to the
  next), so the tracker reconciles **entities only**.
`Interval` is emitted so a consumer can expire entities if a delete is lost
(crash, kill -9, partition); an edge expires with its source entity.

## 6b. Multiple producers, same entity

Two agents observing the **same** entity reconcile because identity is exact
and **observer-independent** (this is why `db.instance.id` is a source id like
`system_identifier`, never a network address — every agent derives the same
id). They converge on one node in the graph.

Two consequences, frozen with the Toise team:

- **Attribute discipline.** `entity_state` is a *complete* state, so two
  producers writing *different* descriptive attributes for the same entity
  would flap (last-writer-wins). Rule: a shared entity carries only
  **observer-independent** attributes (system name, version…). Any
  per-observer fact ("*this* agent monitors this db") is a separate
  `monitors` **relation** per agent — never an attribute of the entity.

- **Delete is per-identity, not per-producer (phase-1 limitation).** An
  explicit `entity_delete` from one agent clears the entity even if another
  still observes it; that other agent recreates it on its next heartbeat
  (~one-cadence flap). The `Interval` backstop already handles the
  *crash-without-delete* case (the entity survives while any agent
  heartbeats). The clean fix is **per-producer reference counting** on the
  consumer, keyed on `service.instance.id` (already carried on every export's
  resource, so it is a Toise-only change). Deferred; documented as a known
  phase-1 limitation.

Relations carry **no** interval and no explicit delete: an embedded edge
expires when its source entity expires or is deleted (consumer-side cascade),
and is retired by absence when the source's heartbeat stops listing it (e.g.
an edge whose target was removed while the source stays alive).

## 7. Phasing (rollout aligned with Toise)
- **Lot 1** — `host` + `service.instance` entities + `runs_on`. host.id
  activated. Exercises the full chain with a real producer (replaces Toise
  M4's synthetic producer).
- **Lot 2** — `db` entity + `monitors`.
- **Lot 3** — datapoint-stream sub-components (generic from `resource` tags).
- **Lot 4** — host routing table → `network.route` entities owned by the host
  (`has_route`, scalar `next_hop.ip`); #212. Interfaces (`network.interface` +
  `has_interface`) follow as a sub-lot.
- **Lot 5** — `network.device` + SNMP topology in the entity form
  (`network.interface`/`connected_to`, `network.route`/`has_route`); depends on
  #156. Freezes `network.device.id`.

## 8. Open questions
- agent→own-host edge: `runs_on` vs `monitors` vs both.
- De-dup / change-detection store internals for the lifecycle tracker.
- Re-homing the dropped edge attributes (host-route `source`, SNMP
  `local_port`/`remote_port`/route `metric`) onto entities — the bare embed
  drops them today; tracked in #239 (carried by the topology remodel #212/#156).
