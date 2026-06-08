# Entity detection & topology discovery

Design for the agent's **entity-event emitter**: turning what the agent
knows and discovers about infrastructure into OpenTelemetry **entity
events** (and, as an explicit extension, relation events). Tracks issue
[#185](https://github.com/senhub-io/senhub-agent/issues/185).

> **Vendor-neutral.** The agent emits standard OTel entity events (OTel
> Entity Data Model). The SenHub *Toise* platform is one consumer that
> ingests them to build a temporal infrastructure graph — nothing here is
> Toise-specific. Relations use a **neutral** `entity.relation.*` namespace
> (not `senhub.*`, not `toise.*`, not the reserved `otel.*`). See
> `feedback_agent_vendor_neutral`.

## 0. Contract status (frozen 2026-06-01)

Producer↔consumer contract negotiated with the Toise team over two rounds.
**Executable source of truth:** the Toise conformance fixture
`toise-dev/toise:internal/ingest/testdata/conformance/entity-events.json`;
specs in `docs/data-model/otel-mapping.md` + `senhub-agent-contract.md`. The
agent's encoder MUST reproduce that fixture's shapes.

Agreed:
- **Nodes = pure standard OTel entity events.** `entity_state` /
  `entity_delete`.
- **Relations = neutral `entity.relation.*` extension** (`relation_state` /
  `relation_delete`), migrate to the OTel standard when the relationship
  spec lands (it's OTel Future Work today).
- **Exact, immutable identity** — no fuzzy matching. Never put a mutable
  value (pid, leased IP) in identity; those are descriptive attributes. A
  restart is an `attribute` update, not an identity change.
- **Liveness = explicit `entity_delete` (primary) + `Interval` TTL backstop.**
  A re-emitted `entity_state` is the heartbeat. We emit `Interval`; Toise
  uses it as a sweeper safety net against lost deletes (and lost producers).
- **Flat attribute maps, scalar leaves only** — dotted keys, no sub-maps.
- **Bi-temporal:** `event_time` = LogRecord timestamp; `recorded_at` set by
  the consumer (never sent).

Open micro-points: (1) relation discriminant — we recommend the strict
`entity.relation.event.type=state|delete` over the fixture's current
`otel.entity.event.type=relation_state` (keeps relation records free of any
`otel.entity.*`, so a standard OTel consumer never sees a malformed entity
event); pending Toise regen. (2) the agent→its-own-host edge: `runs_on`,
`monitors`, or both. (3) `network.device` id — frozen at Lot 5.

## 1. Two detection planes

Detection is not just *enumerating what is configured* — it is **asset and
topology discovery** from everything the probes can read.

| Plane | What it produces | Sources |
|---|---|---|
| **Inventory** | The entities the agent *is* and *targets*: host, the agent process, each configured probe target. | host self-info, agent config |
| **Discovery / topology** | Assets the agent does **not** monitor directly, plus the **relations** between assets. | host routing / ARP / neighbour tables (available now); LLDP/CDP, bridge FDB, route tables via **SNMP** (#156, later) |

The value for a temporal graph is in the **edges** (relations). Topology
discovery makes the agent a *sensor of the infrastructure graph*, not just a
reporter of its own configuration. Note relations are a non-standard
extension today (§4) — emitted deliberately, isolated, migratable.

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

### Relation (neutral extension)
A typed directed edge: `type` + `from.{type,id}` + `to.{type,id}` +
optional `attributes`. Agreed types: `runs_on`, `monitors`, `routes_via`,
`forwards_to`, `adjacent_to`. **Endpoints resolved by exact identity** — the
encoder MUST emit both endpoint entities (current identity) before/with the
edge; Toise reconciles out-of-order arrivals within a window.

## 3. Detection sources
1. **Host self-info** → the `host` entity (`common.GetHostTags()` + `host.id`).
2. **Agent config** → the `service.instance` entity + one entity per
   configured remote probe target, known at load → emitted immediately.
   Relations `service.instance --runs_on--> host` and `--monitors--> target`.
3. **Datapoint-stream resource tags** → sub-component entities. The
   transformer `tag_metadata` `type: resource` set already *is* an entity
   identity → generic synthesis, no per-probe code.
4. **Host network tables** (topology, now) → discovered devices + relations
   (routing → `routes_via`, ARP/neighbour → `adjacent_to`).
5. **SNMP topology MIBs** (with #156) → LLDP/CDP (`adjacent_to`), BRIDGE-MIB
   FDB (`forwards_to`), ipCidrRouteTable (`routes_via`).

## 4. Encoding — LogRecords

Events travel as OTLP `LogRecord`s on the existing log rail:
`agentstate.PublishLog(rec)` → `logsPump` → `logsPipeline.emit()` → exporter.
**No new transport wiring.** One isolated encoder owns all wire shape so OTel
spec churn is contained.

**Entity event** — merged OTel entity-events spec, frozen with Toise
(#222, lot 0a). The event kind is the **LogRecord `EventName`**, not a
payload attribute; node attributes use **bare keys** (no `otel.entity.*`):
- `EventName` = `entity.state` | `entity.delete`
- `entity.type` = string
- `entity.id` = kvlist (scalar leaves, dotted keys) — **self-contained**
  (identity lives here, not referenced from the resource)
- `entity.description` = kvlist (scalar leaves; omitted on delete) — the
  descriptive attributes (formerly `otel.entity.attributes`)
- `entity.report.interval` = int **seconds** (TTL backstop; state only)
- LogRecord timestamp = `event_time`
- Scope flag `otel.entity.entity_event=true` still set (contrib fast-path
  filter convention; Toise ignores it). Its removal is tracked separately
  from the record encoding.

**Relation event** (`relation_state` / `relation_delete`), neutral namespace
— still the extension form until lot 0b folds relations into an embedded
`entity.relationships` array on the source's `entity.state` event:
- `entity.relation.event.type` = `state` | `delete`  *(recommended; pending
  Toise — fixture currently overloads `otel.entity.event.type`)*
- `entity.relation.type`, `entity.relation.from.{type,id}`,
  `entity.relation.to.{type,id}`, optional `entity.relation.attributes`
- carries **no** `otel.entity.*` attribute → invisible to standard OTel
  entity consumers

Values restricted to scalars (`string`/`int64`/`double`/`bool`); maps are
flat (dotted keys), never nested. The encoder rejects/warns on a non-scalar
leaf — never silently drops.

## 5. host.id — stable host identity (prerequisite)
`common.GetHostTags()` emits `host`/`os`/`platform` only; gopsutil `HostID`
is commented out. Activate it as the host entity's `host.id`. It is
**mandatory on the entity-event record** as `otel.entity.id = {host.id: …}` —
the consumer reads identity off the record, not the resource (a record with
no `otel.entity.id` is rejected). Putting `host.id`/`host.name` in the **OTLP
resource** too is optional (OTel resource semantics). Never emit it as a
per-datapoint tag (a constant tag on every metric would only pollute
PRTG/Prometheus labels).

## 6. Lifecycle tracker (required)
Because liveness is delete-driven, the emitter keeps a store of currently-known
entities/relations and their last-emitted descriptive state. It emits:
- `entity_state` on first detection and on descriptive change (heartbeat on a
  configurable cadence aligned with the OTLP push interval);
- `entity_delete` when an entity is no longer detected (config removed,
  target unreachable, instance gone);
- the same state/delete model for relations.
`Interval` is emitted so a consumer can expire entities/edges if a delete is
lost (crash, kill -9, partition).

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

Relations carry **no** interval: an edge expires when one of its endpoints
expires or is deleted (consumer-side cascade); `relation_delete` removes an
edge whose endpoints are still alive.

## 7. Phasing (rollout aligned with Toise)
- **Lot 1** — `host` + `service.instance` entities + `runs_on`. host.id
  activated. Exercises the full chain with a real producer (replaces Toise
  M4's synthetic producer).
- **Lot 2** — `db` entity + `monitors`.
- **Lot 3** — datapoint-stream sub-components (generic from `resource` tags).
- **Lot 4** — host network tables → discovered devices + `routes_via`/`adjacent_to`.
- **Lot 5** — `network.device` + SNMP topology (`routes_via`/`forwards_to`/
  `adjacent_to`); depends on #156. Freezes `network.device.id`.

## 8. Open questions
- Relation discriminant final form (see §0/§4) — pending Toise regen.
- agent→own-host edge: `runs_on` vs `monitors` vs both.
- De-dup / change-detection store internals for the lifecycle tracker.
