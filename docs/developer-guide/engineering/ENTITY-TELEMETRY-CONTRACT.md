# Entity ↔ telemetry correlation contract

**Status:** proposed, 2026-08-04. Completes and makes enforceable the rule
already stated in [`ENTITY-DETECTION.md`](./ENTITY-DETECTION.md) §5.
**Audience:** SenHub agent maintainers, and the Toise team as the consumer.

The SenHub agent is the single producer of the infrastructure graph. Every
entity in the production graph today — all 567 of them — was emitted by an
agent. It is therefore the only component that knows, at the same instant,
both *that an entity exists* and *what telemetry describes it*. Nothing
downstream can reconstruct that link if the agent does not stamp it, because
downstream sees two independent streams and no way to prove they are about
the same thing.

That makes correlation an emission-side obligation, not a consumer-side
heuristic. This document states it as a contract.

---

## 1. What was measured

Production graph and metric backend, 2026-08-04.

| Fact | Value |
|---|---|
| Entities in the graph | 567, across 10 types |
| Entity types whose telemetry can be located by their own key | 3 of 10 |
| `senhub_*` metric series carrying `host_id` (OTLP-delivered) | **100 %** |
| `senhub_*` metric series carrying `service_instance_id` (OTLP-delivered) | **100 %** |
| Rails with a shared enrichment code path | **0 — there are three disjoint ones** |
| Sink families carrying `host.id` at all | **1 of 4** (OTLP only) |
| `db_instance_id` present as a metric label | **no** |
| `service_endpoint` present as a metric label | **no** |
| `entity.attribute_updated` events in 6 h | 122, dominated by two entities flapping |

Two readings matter more than the rest.

**The foundation already works.** Every single metric series the agent
produces carries `host_id` and `service_instance_id`. The agent already
answers *where* a measurement was taken and *who* reported it, universally,
with no per-probe effort. The gap is not the foundation.

**What is missing is the subject.** For a series that describes something
other than the host — a database, a listener, an SNMP device — nothing says
*what the series is about*. That is the whole gap.

## 2. The defect is not a missing label

It is tempting to read the gap as "some labels were forgotten". The
production graph says otherwise.

Four PostgreSQL instances run on four different machines, all listening on
`127.0.0.1:5432`. They appear as **four distinct entities**, because
PostgreSQL reports a server-side unique id (`system_identifier`) and the
agent uses it:

```
db.instance.id = postgresql:7459423122218342138
db.instance.id = postgresql:7248935625745506335
db.instance.id = postgresql:7260015372444151839
db.instance.id = postgresql:7459423139354528763
```

One MariaDB and one Redis appear as **one entity each**, shared by two
machines, because neither reports such an id and the agent falls back to
`address:port`:

```
db.instance.id = 127.0.0.1:3306     ← two different servers
db.instance.id = 127.0.0.1:6379     ← two different servers
```

Same probe family, same address, opposite outcome. The variable is not the
label — it is whether the identity is a property **of the thing** or a
property **of the path to the thing**. `127.0.0.1:3306` does not name a
database; it names "port 3306 as seen from wherever I happen to be
standing". It is a relative address wearing the costume of an absolute
identity.

The consequences are observable, not theoretical:

- The collapsed MariaDB entity carries **two incoming `monitors` edges from
  two different agents**, and **no `runs_on` edge to any host**. The graph
  itself records that two observers disagree about one node.
- `telemetry_keys` on it returns `service.instance.id` and
  `service.name = shop-preprod` — the identity of **the observer**, not of
  the database. When a subject has no identity of its own, the graph
  silently substitutes whoever last spoke about it. A consumer that trusts
  the answer queries the wrong machine's metrics.
- 122 of the last 6 hours' change events are these two entities alternating
  their `db.system.version` every ~5 minutes (`10.3.39-MariaDB` ↔
  `11.8.6-MariaDB`, `7.0.11` ↔ `8.0.5`). The real change signal is buried.

### 2b. It violates a rule Toise already froze

This is not a grey area. Two decisions frozen with Toise cover it exactly:

- **ADR 0018 — identity is observer-independent.** Two agents observing the
  same thing must derive byte-identical ids
  (`ENTITY-DETECTION.md:221-224`, `SNMP-OTEL-MAPPING.md:127-129`). The
  converse is the same requirement read backwards, and it is what breaks
  here: two agents observing *different* things derive *identical* ids.
- **ADR 0032 — host-local addresses are host-scoped.** Already implemented
  for `network.endpoint` (`entity/hostdep/hostdep.go:334-341`, "Toise
  host-scopes exactly this set on its side"), and the comment there states
  the reason verbatim: so host A's `127.0.0.1` does not collapse onto host
  B's.

So the fix for `db` is not a new rule to negotiate. It is applying a rule
Toise has already frozen, and which our own emitter already applies to a
neighbouring type.

### 2c. Why the code drifted: the spec contradicts itself

The probes are not simply careless. `ENTITY-DETECTION.md` says both of
these, in the same document:

| Line | Says |
|---|---|
| §2, line 75 | `db.instance.id` is a "single composite string e.g. `pg@10.0.1.5:5432`" — i.e. **network-derived by default** |
| §6b, lines 222-224 | `db.instance.id` is "a source id like `system_identifier`, **never a network address**" |

`.claude/rules/probes.md:157` and the second of these are the authoritative
ones. But a reader implementing from §2 would produce exactly the identity
we now have in production. The drift began in the specification, and any
contract that does not retire the stale half will let it happen again.

The primary rationale documents cited for these decisions —
`docs/audit/ENTITY-CONTRACT-DISCUSSION-TOISE.md` and
`docs/audit/LOT5-TOISE-DISCUSSION.md` — **do not exist in the repository**;
`docs/audit/` is gitignored and they were never committed. The frozen
decisions survive only as prose scattered across four files.

### 2d. `db` is one instance of a systemic class

A census of every entity-emitting site in the agent (12 entity types, ~40
sources) shows the same defect in roughly twenty places, at four severities.
`db` is the one production noticed, not the worst one.

| Severity | Sites | Identity emitted | Consequence |
|---|---|---|---|
| **Critical** | `winservices` (`winservices/entity_source.go:21`), `chrony` (`chrony/entity_source.go:25`) | a **constant**: `winservices://localhost`, `chrony://localhost` | Byte-identical on every host in the fleet. All of them collapse onto **one** node, and each still emits `runs_on → host{its own host.id}` — so that single node fans out to every Windows host and transitively joins them |
| **High** | `hostiface:166`, `hostnet:118`, `snmppoll:620` | `network.address` = the bare IP, filtered only for wildcard/loopback/link-local/multicast/docker0 | **RFC1918 is not filtered.** A default gateway `192.168.1.1` is one shared node across every disjoint customer LAN |
| **High** | ~18 service probes' last resort | the bare product name — `"nginx"`, `"kafka"`, `"consul"`, `"proxmox@unknown"` | Fires whenever `host.id` is unresolvable; every such host collapses onto one node per product |
| **High** | db `host:port` fallback, `mssql://`, `oracle://`, `modbus://`, `unifi://`, `kubernetes://`, `systemd://<hostname>/<unit>` | address- or URL-derived | The #740 class. `systemd://` additionally uses raw `os.Hostname()`, which is **mutable** — a rename re-keys every unit entity |
| **Medium** | `process/entity_source.go:66-69` | `process.pid` + `process.creation.time` | Correct pair, but **not host-scoped**: two hosts with the same pid and creation instant collapse |

The pattern is identical every time: when no stable id is available, the
code substitutes *the path the agent used to reach the thing*. That path is
a property of the observer, so every observer standing in the same place
produces the same id.

Two further consequences of the same root, worth stating because they
change how failures present:

- **The redaction guard is incomplete.** `entityIdentityKeys`
  (`otlp/config.go:1164-1171`) enumerates only six identity keys and omits
  `service.endpoint`, `interface.name`, `route.destination`,
  `network.address`, `server.address`/`server.port`/`network.transport`,
  `process.pid`, `process.creation.time`. An operator can put any of them
  in `redact_attributes` and the parser accepts it — destroying the
  identity the guard exists to protect.
- **A failed identity can silence an entity entirely.** `dropOrphanEntities`
  (`entity/source.go:175-196`) drops any entity with no relation. An entity
  whose `monitors` was skipped (agent id unset) and whose `runs_on` was
  refused by the loopback collapse guard never reaches the wire at all — so
  some identity defects present as *missing* entities rather than merged
  ones.

## 3. The rule already exists — it was never enforced

[`ENTITY-DETECTION.md`](./ENTITY-DETECTION.md) §5, frozen 2026-06-01, already
prescribes exactly this:

> Remote targets (SNMP devices, DBs) do **not** ride the agent's host
> resource: their identity (`network.device.id`, `db.instance.id`) is a
> **per-metric** attribute, so a device's interface metric joins to its
> `network.interface` entity by those metric attributes.

One of the two named cases honours it. `snmppoll` stamps every metric of a
polled device with its `network.device.id`
(`internal/agent/probes/snmppoll/collector.go:192-195`, comment: *"ties every
metric of this device to its network.device"*), and `network_device_id` duly
exists as a label in the metric backend.

The db probes never did. They emit `db.system.name`, `server.address` and
`server.port`, and no `db.instance.id`
(`internal/agent/probes/redis/redis_probe.go:197`,
`internal/agent/probes/mysql/probe.go:222`).

So this is not a contract to negotiate. It is a documented rule that half
the code follows, with nothing in CI to notice the other half drifting. The
missing piece is enforcement — which is why the rest of this document is
mostly about making the rule checkable rather than about inventing it.

## 4. The contract

### C1 — Three keys, every signal

Every metric, log record and span the agent emits carries, at minimum:

| Role | Attribute | Where | Guaranteed by |
|---|---|---|---|
| **Location** — where it was observed | `host.id` | OTLP resource | the entity foundation, universal |
| **Observer** — who reported it | `service.instance.id` | OTLP resource | the entity foundation, universal |
| **Subject** — what it is about | the identity attribute of the entity described | **per-datapoint attribute** | the probe, per C2 |

Location and observer stay on the OTLP resource; `host.id` must never be
duplicated as a per-datapoint tag.

**Current reality, measured — this is an objective, not a description.**
The three keys hold on the OTLP rail and nowhere else:

| Sink | `host.id` | `service.instance.id` | Why |
|---|---|---|---|
| OTLP (metrics, logs, own traces) | yes | yes | one shared `*resource.Resource`, built once (`otlp/strategy.go:211`) |
| Prometheus / PRTG / Nagios | **no** | **no** | these paths never see a Resource; the agent key appears only in the URL |
| senhub cloud | **no** | **no** | payload has no slot; the key is the transport credential |
| `/event/insert` logs | **no** | **no** | its `host` field is the *syslog-reported* hostname, a different fact |
| relayed traces | under another key | under another key | `telemetry.relay.host.id` / `.instance.id` |

PRTG and Nagios are display formats with no label slot; they are out of
scope and stay so. Prometheus is **not** — it feeds the same backend Toise
pivots into, and it currently carries no host identity at all.

### C7 — One enrichment point per rail, converging on one

There is today **no single place** where the three signals are stamped. The
rails are structurally disjoint: metrics enrich in `DataStore.GetCallback`,
logs in `agentstate.PublishLog` (which bypasses the DataStore entirely —
`data_store.go:409-413`: *"logs never pass through this router"*), traces in
`otlp/trace_enrich.go` (which bypasses both). The only shared thing is a
Resource *value*, not a code path — and it covers one sink family out of
four.

That is the structural reason coverage is uneven, and why it will drift
again whatever labels we add today. The contract therefore requires:

1. each rail names **one** function that stamps identity, and no probe
   stamps identity outside it;
2. the same fact uses the same key on every rail — today metrics say
   `probe_name`/`probe_type` and logs say `senhub.probe.name`/`.type` for
   the same thing;
3. every path that re-emits must re-stamp. The log replay path
   (`otlp/logs.go:191-216`) currently reconstructs record attributes but
   not the probe identity, so persisted-then-replayed logs lose it.

The subject key is required **only when the subject is not the agent's own
host**. A host CPU metric needs no subject key: the resource already names
it. A database metric does, because the resource describes the machine the
agent runs on, not the database it polled.

### C2 — Identity is a property of the thing

An entity's identity must be derived from the thing itself, in this
precedence:

1. an operator-configured stable name, when provided;
2. an id the technology reports and persists across restarts
   (`system_identifier`, `server_uuid`, node UUID, cluster id…);
3. a **host-scoped** composition — see C3.

A value that is only meaningful relative to the observer is never an
identity on its own.

### C3 — A host-relative value is only an identity when scoped by `host.id`

When the fallback must be used and the address is loopback or link-local,
the identity carries `host.id`. This is not a new rule: it is the pattern
already agreed and shipped in 0.5.3 for `network.endpoint` (PR #713), and
already used by two other types the agent emits:

| Type | Identity form | Host-scoped? | Distinct in production |
|---|---|---|---|
| `service.listener` | `<host.id>:<port>/<transport>` | yes | 305 |
| `service.instance` (fallback) | `<service.name>@<host.id>` | yes | 30 |
| `db` (fallback) | `<address>:<port>` | **no** | collapses |

Three sibling types from the same emitter; one omits the scope. Applying C3
to `db` makes our own emitter self-consistent.

### C4 — Every entity type declares its telemetry status

Each registered entity type declares exactly one of:

- **own-key** — it has telemetry of its own, located by its identity
  attribute stamped per C1 (`host`, `container`, `db`, `network.device`,
  `network.interface`, `service.instance`);
- **inherited** — it has no telemetry of its own, but a structural edge
  reaches one that does, and the declaration names the edge
  (`service.listener` → `runs_on` → `host`; verified working today,
  `telemetry_keys` returns the host's `host.id`/`host.name`);
- **graph-only** — it has no telemetry and none is reachable
  (`network.address`, `network.route`, `network.endpoint`).

`graph-only` is a legitimate answer and must be stated, not left implicit.
The failure mode this contract exists to remove is not the absence of a key;
it is a consumer unable to distinguish *"no key exists"* from *"I have not
found the key yet"*.

### C5 — Identity and readable name travel together

Identity serves the machine join; a name serves the human reading a
dashboard. `host` and `container` already ship both (`host_id`/`host_name`,
`container_id`/`container_name`) and that pair is what makes them usable. A
type declaring **own-key** ships both. For `db`, the readable name is the
operator's configured instance name when set, else `<db.system.name>@<host>`.

### C6 — The declaration is enforced by a test

A table maps every registered entity type to its C4 status and, for
own-key types, to the attribute that must appear on its telemetry. A test
walks the registry and fails when a type is missing from the table.

This is the load-bearing rule. §5 was correct for fourteen months and drifted
anyway, because nothing failed when it was ignored. A new entity type must
not be able to ship without declaring where its telemetry is.

## 5. Application

| Type | Count | C4 status | Subject key | State |
|---|---|---|---|---|
| `host` | 21 | own-key | `host.id` (resource) | shipped |
| `container` | 61 | own-key | `container.id` | shipped |
| `service.instance` | 30 | own-key | `service.instance.id` (resource) | shipped |
| `network.device` | 1 | own-key | `network.device.id` (per-metric) | shipped |
| `network.interface` | 50 | own-key | `network.interface.name` | shipped |
| `db` | 7 | own-key | `db.instance.id` (per-metric) | **missing — needs C2/C3 first** |
| `service.listener` | 305 | inherited via `runs_on` | — | works, undeclared |
| `network.address` | 55 | graph-only | — | undeclared |
| `network.route` | 31 | graph-only | — | undeclared |
| `network.endpoint` | 6 | graph-only | — | undeclared |

### Ordering constraint

`db.instance.id` must **not** be emitted as a label before C2/C3 make it
unique. Publishing `127.0.0.1:3306` as a join key would turn a gap a
consumer can detect into a wrong answer a consumer cannot. Fix the identity
first, stamp the label second.

## 6. What this asks of Toise

One thing only: absorb a re-key of the `db` entities whose identity is
currently a bare `address:port`, when the agent starts scoping them with
`host.id`.

- **Scope:** the collapsed entities only. The four PostgreSQL entities,
  which already carry a technology-reported id, are untouched — as are all
  other types.
- **Precedent:** the same host-scoping was applied to `network.endpoint` in
  0.5.3 (PR #713) and absorbed by the read layer without incident.
- **Effect:** each collapsed entity splits into the two real databases it
  was merging. Their history before the cutover describes two machines
  interleaved and cannot be retroactively separated; the split point should
  be recorded.

What this does **not** ask: no new entity type, no change to the type
vocabulary, no change to any identity that is already technology-derived,
and no change to the relation model.

## 7. References

- [`ENTITY-DETECTION.md`](./ENTITY-DETECTION.md) §5 — the rule this
  completes
- `.claude/rules/probes.md` — entity-type table, `db` vs `service.instance`
  boundary, identity precedence
- #713 — host-scoped loopback identity, the agreed precedent
- #740 — db identity collapse (production evidence)
- #741 — correlation coverage measurement (production evidence)
- #742 — constant `service.instance.id` on `winservices` / `chrony` (critical)
- #743 — `network.address` collides on RFC1918 (needs a Toise decision)

## Appendix — what to raise with Toise, in one sitting

Four items, one conversation, because they are the same question asked
about four types:

1. **`db` re-key** (#740) — apply ADR 0032's host-scoping to the degraded
   fallback. Precedent already absorbed in 0.5.3 for `network.endpoint`.
2. **`service.instance` re-key** (#742) — the constant-identity probes.
   Same shape, no new rule; worth naming because the churn is fleet-wide.
3. **`network.address` scope** (#743) — genuinely open, and the only one
   needing a decision rather than a fix: the node is a deliberate join
   point between host routes and SNMP topology, so scoping it changes a
   contract rather than repairing one.
4. **Retire the stale half of our own spec** — `ENTITY-DETECTION.md` §2
   still says `db.instance.id` is a network-derived composite. Whoever
   implements from it reproduces the bug.

What is **not** on the table: no new entity type, no change to the type
vocabulary, no change to any technology-derived identity, no change to the
relation model or the wire shape.
