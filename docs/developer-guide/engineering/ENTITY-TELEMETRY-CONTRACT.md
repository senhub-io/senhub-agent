# Entity ↔ telemetry correlation contract

**Status:** reviewed with the Toise team 2026-08-07; §6 and the appendix
record what was agreed. Completes and makes enforceable the rule already
stated in [`ENTITY-DETECTION.md`](./ENTITY-DETECTION.md) §5.
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

One MariaDB and one Redis appear as **one entity each**, shared by several
machines, because neither reports such an id and the agent falls back to
`address:port`:

```
db.instance.id = 127.0.0.1:3306     ← two different servers
db.instance.id = 127.0.0.1:6379     ← three different servers
```

Five real databases, two nodes. The count is read off the incoming
`monitors` edges — see §6, where Toise measured the same thing
independently.

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
  the database. A consumer that trusts the answer queries the wrong
  machine's metrics.

  This symptom has **two causes, one on each side**, and the Toise review
  found the second. Their `telemetry_keys` enriched an entity by following
  *every* relation one hop and inheriting the neighbours' join keys, so an
  entity with no key of its own inherited the agent's `service.name`
  through `monitors`. The intent was ownership (`runs_on`); `monitors` is
  an observation relation and `depends_on` a peer relation, and inheriting
  through either is wrong. They restrict inheritance to ownership
  relations. **The agent-side identity repair alone would not have removed
  the symptom** — both fixes were required.
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

The rationale documents this repository used to cite —
`docs/audit/ENTITY-CONTRACT-DISCUSSION-TOISE.md` and
`docs/audit/LOT5-TOISE-DISCUSSION.md` — **do not exist**; `docs/audit/` is
gitignored and they were never committed. Citing them was citing nothing.

**The canonical references are ADR 0018 and ADR 0032**, committed and
versioned in the Toise repository, which the Toise team has undertaken not
to change without telling us (their standing contract-stability policy).
Cite those. A frozen decision needs a durable address, and the one on our
side was a dead link.

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

The distinction matters enough to state plainly, because the Toise review
asked for it directly — *which resource attribute do the db metrics
carry?* — and the answer is **neither**. `db.instance.id` exists only in
the probes' `entity_source.go` files, that is, on the entity rail. It is
not a per-datapoint attribute, and it is not on the resource either: the
resource always describes the agent's own host, never a remote target. So
`db_instance_id` is not a label lost in transit — it is never emitted.

The practical consequence for the consumer: adding `db.instance.id` to a
join-key set is a no-op until the agent ships it.

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

**Two tiers, not one rule with an exception** (frozen with Toise,
2026-08-07 — both sides state it in these terms, because the two halves
read as contradictory when quoted separately):

1. the identity of the subject **described by the resource** — the agent's
   own host — lives on the resource and never descends into a per-datapoint
   label;
2. the identity of a **remote target the agent observes** lives in a
   per-datapoint attribute, because no resource describes it.

A single `snmp_interface_*` series shows both tiers at once: `host_id` from
the resource, naming the **observer**, and `network_device_id` per
datapoint, naming the **observed**. The second tier is not an exception to
the first; it is its other half.

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
scope and stay so.

**Where the round trip is guaranteed (agreed with Toise, 2026-08-07).**
"Round trip" means: read an entity, obtain a label set, query it, get its
series. The guarantee is rail-dependent, and saying so is the contract —
not a gap in it.

| Path | Round trip | Realised by |
|---|---|---|
| OTLP rail → collector → VictoriaMetrics / VictoriaLogs | **guaranteed** | the resource, carried on every signal |
| Prometheus-family backend fed by a collector | holds in practice | the collector's `resource_to_telemetry_conversion`, a **deployment choice** of the consumer — not a property of the wire |
| The agent's own Prometheus scrape endpoint | **not guaranteed** | nothing today; `target_info` is the answer (#745) |

The rule that identity keys never become per-datapoint labels **stands**,
and Toise explicitly asked for it to stand: promoting them would impose
cardinality on every operator, including those whose output is PRTG. The
consumer that wants flat labels performs the flattening in its own
collector, where the cost is paid knowingly and the decision is reversible.

One correction the review produced: the agent *does* expose a native
Prometheus endpoint (`http` strategy — `http_prometheus.go` and the
`prometheus/` package). It has no resource concept, hence no host identity,
so the third row above is a present gap rather than a hypothetical one. It
is not filled with identity labels; `target_info` — one series per
resource, joined on `(job, instance)` — is the low-cardinality mechanism
for it, implemented by neither side today.

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
- **graph-only** — it has no telemetry of its own, and **no structural
  edge reaches any** (`network.address`, `network.route`,
  `network.endpoint`).

`graph-only` is a statement about what the **producer** emits and what the
**graph** carries. It does not mean no telemetry is reachable by any means:
Toise's read overlay resolves a `network.endpoint` to the listener bound to
it, or failing that to the host owning the address — 61 of 74 resolve on
their production — and reaches the host's telemetry from there. That
resolution is computed on the consumer side; it is not an edge we emit, and
nothing in this contract obliges them to keep it.

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

### C6a — A check must run in the direction where the defect can hide

Stated as a rule of its own because twice now the fact was present and the
verification looked the wrong way.

`network.interface` (#748) was declared shipped by reading the emitter that
writes the label, never the join it is meant to serve. `process` and
`compute.vm` (#753) were emitted for months while the vocabulary claimed ten
types, because the test walked the list and confronted it with the
declaration — nothing confronted the **code** with the list. Both times a
claim was verified against itself, and both times the check passed.

So every declaration in this contract names the direction of its check:

| Declared | Checked against | Not against |
|---|---|---|
| the type vocabulary | the emitting code | a second list |
| a subject key | emitted output | the emitter's source |
| an identity value | the entity's own identity | a sibling label |

The test for the second direction is the one worth writing, because the first
is the one someone will write by reflex.

Toise reached the same conclusion independently on their side: their
accessors were checked against a list written in the test — two lists
answering each other — while the direction that held was the comparison with
the registry map the engine actually consults to accept or refuse. They now
read their own package source for declared constants and fail when one is
absent from the accessors.

**A corollary, learned the expensive way.** A test is a guarantee only once
it has been seen to fail without its fix. Three tests written for #748 were
green and guarded nothing: one collected a single cycle and skipped on every
host, one compared against a sibling tag that only agrees on Unix, one
demanded a tag the probe deliberately omits. The code under test was correct
throughout. Run the negative before believing the positive.

### C6 — The identity string has one source, and equality is enforced

The value stamped on the telemetry must be the **same string, byte for
byte**, as the entity's identity. This is the silent failure mode: an
entity keyed `postgresql:7459423122218342138` and a metric labelled
`127.0.0.1:5432` never join, and nothing reports it — the query simply
returns empty. A presence check does not catch it; only an equality check
does.

Two mechanisms, and the first is what makes the second cheap.

**One resolution site.** The identity is resolved once, in the type's
entity source, stored there, and *read* by the metric path — never
recomputed. This is what makes `network.device` work today:
`snmppoll/entity_source.go:194` pins `s.deviceID`, exposes it through a
getter (`:155`), and `collect` receives it as a parameter and stamps it
verbatim in `baseTags` (`collector.go:187-195`). Both the entity identity
and the metric attribute derive from that one string, so divergence is not
prevented by discipline — it is structurally impossible. Any type
recomputing its identity on the metric path is a defect regardless of
whether the two expressions currently agree.

**One table, enforced.** A table maps every registered entity type to its
C4 status and, for own-key types, to the attribute that must appear on its
telemetry. A test walks the registry and fails when a type is missing from
the table, when a declared attribute is absent from the type's emitted
telemetry, **or when its value differs from the emitted entity identity**.

This is the load-bearing rule. §5 was correct for fourteen months and drifted
anyway, because nothing failed when it was ignored. A new entity type must
not be able to ship without declaring where its telemetry is — and a type
whose label drifts from its identity must fail in CI, not in a dashboard
that quietly returns nothing.

## 5. Application

| Type | Count | C4 status | Subject key | State |
|---|---|---|---|---|
| `host` | 21 | own-key | `host.id` (resource) | shipped |
| `container` | 61 | own-key | `container.id` | shipped |
| `service.instance` | 30 | own-key | `service.instance.id` (resource) | shipped |
| `network.device` | 1 | own-key | `network.device.id` (per-metric) | shipped |
| `network.interface` | 50 | own-key | `interface.name` (per-metric) | **half shipped — SNMP only (#748)** |
| `db` | 7 | own-key | `db.instance.id` (per-metric) | **missing — needs C2/C3 first** |
| `service.listener` | 305 | inherited via `runs_on` | — | works, undeclared |
| `network.address` | 55 | graph-only | — | undeclared; bare IP kept **by design** (#743) — it is the host-route ↔ SNMP-device join point |
| `network.route` | 31 | graph-only | — | undeclared |
| `network.endpoint` | 6 | graph-only | — | undeclared |

### `network.interface` — why the row moved from "shipped" to "half shipped"

Toise verified the shipped rows against their backend and found this one
false. It is worth recording here rather than only in #748, because it is
the best available illustration of C6 — better than any constructed
example.

The same notion travels under two labels: `interface_name` carries the 63
`snmp_interface_*` series, `network_interface_name` carries the 256
`system_network_*` host series. The entity is keyed `interface.name`, so a
consumer following the entity's own key finds the SNMP interfaces and
misses every host interface.

Underneath sits a second divergence, on values this time. On Windows the
entity carries the connection name (`Ethernet`, `Ethernet 2`) while the
metric carries the PDH instance name — the adapter description with PDH's
dedup suffix (`Red Hat VirtIO Ethernet Adapter _2`). On Unix both derive
from the same interface name and match. **The defect is invisible on Linux
and total on Windows.**

Two lessons the contract absorbs:

- **A presence check would have passed.** A label named
  `network_interface_name` exists, is populated, and joins nothing. This is
  why C6 asserts equality, not presence.
- **The row was declared shipped by reading the emitter, not the join.** A
  declaration verified only against the code that writes it is not
  verified. C6 must run against emitted output.

### Ordering constraint

`db.instance.id` must **not** be emitted as a label before C2/C3 make it
unique. Publishing `127.0.0.1:3306` as a join key would turn a gap a
consumer can detect into a wrong answer a consumer cannot. Fix the identity
first, stamp the label second.

## 6. What was agreed with Toise

Reviewed 2026-08-07. Toise measured the same production data independently
and confirmed the findings: four distinct `postgresql:<system_identifier>`
entities against a single `127.0.0.1:6379` carrying **three** incoming
`monitors` edges and a single `127.0.0.1:3306` carrying two — five real
databases collapsed into two nodes.

**Re-keys are absorbed, without reservation and without consumer-side
code changes** (`db`, and the constant identities of C2's critical row). A
new identity is a new entity; the old one expires by liveness and leaves
with `deleteSource=liveness_expiry`. The same transition was proven in
production last month with the four-key loopback batch.

**One requirement of form, which the agent already satisfies:** the new
identity must be emitted *alongside*, never by mutating the identity of an
existing entity. Their model forbids the second — identity is immutable,
which is what makes history readable. Our lifecycle tracker is keyed on
identity, so a re-key is structurally a new entity plus an extinction; the
mutating case does not exist in our code.

**The cutover point needs no freeze on their side** — the journal
materialises it, and everything stays queryable `as_of`. What they offer is
to annotate the outgoing nodes so a later reader knows their pre-cutover
history describes several interleaved machines. That requires one thing
from us: **the cutover date**.

**Churn is not a concern**, including at fleet scale: a node splitting into
N is N creations and one extinction, which their journal absorbs and
heartbeat compaction settles. Their one piece of advice: **switch in a
single cutover rather than in waves**, which is far more readable after the
fact.

**`network.address` is decided against scoping** — see §5 and the closed
issue #743.

What was **not** asked and did not change: no new entity type, no change to
the type vocabulary, no change to any identity that is already
technology-derived, and no change to the relation model or the wire shape.

### 6b. Division of responsibility (agreed 2026-08-10)

Recorded here, and in Toise's API stability policy on their side, because
two statements that answer each other outlive an email thread.

| Owned by | What | Why there |
|---|---|---|
| **Toise** | the type vocabulary and the wire form of an entity event | they apply it at ingest; their own boundary derives from the same registry |
| **The agent** | the transport, and the verification that what is declared is what is emitted | our operational guarantees — batching, backpressure, retry, tenant propagation — live here, and only we can observe our own output |

The principle: **responsibility follows the ability to check.** A party
that cannot observe a fact cannot be accountable for it.

Concretely, the agent consumes `pkg/emit/wire`, the SDK's stdlib-only
spelling of the vocabulary, rather than spelling the literals locally. A
literal written twice is how `network.interface` came to be named two ways
(#748); a shared constant makes that spelling impossible. `wire` carries
the event names and attribute keys today, and from `pkg/emit/v0.6.0` the
entity and relation type vocabularies as well — at which point the agent's
local `AllTypes` is deleted in favour of the SDK's, and a unilaterally
invented type stops compiling on both sides.

What the agent does **not** adopt is the SDK's runtime encoder. Its client
owns its own gRPC connection and works in collector `pdata`, while entity
events ride this agent's log rail; adopting it would put a second export
path outside the batching, backpressure and tenant headers the strategy
owns. The absence of drift that motivated #455 is obtained instead by
comparing the two encoders in a test, which breaks the build on divergence
without coupling the runtime.

**What the pin guarantees, and what it does not.** `pkg/emit` is Toise's
supported Go surface (their `internal/` packages are not): additive within
a series, deprecation before removal. Before their 1.0, their policy still
permits a break on a stable surface provided it is announced in the
preceding release and journaled — so we are never surprised, but we are not
promised immobility. Pin to an explicit version rather than tracking a
branch.

## 7. References

- [`ENTITY-DETECTION.md`](./ENTITY-DETECTION.md) §5 — the rule this
  completes
- `.claude/rules/probes.md` — entity-type table, `db` vs `service.instance`
  boundary, identity precedence
- #713 — host-scoped loopback identity, the agreed precedent
- #740 — db identity collapse (production evidence)
- #741 — correlation coverage measurement (production evidence)
- #742 — constant `service.instance.id` on `winservices` / `chrony` (critical)
- #743 — `network.address` on RFC1918 — **closed, by design** (§5)
- #745 — `target_info` on the native Prometheus endpoint
- #748 — `network.interface`: host interfaces unreachable from their entity
- **ADR 0018** (observer-independent identity) and **ADR 0032**
  (host-scoping) — committed and versioned in the Toise repository; the
  canonical address for both frozen decisions

## Appendix — outcome of the Toise review

Four items were raised in one conversation, because they were the same
question asked about four types. Their disposition:

| # | Item | Outcome |
|---|---|---|
| 1 | **`db` re-key** (#740) | Absorbed. Emit the new identity alongside; give them the cutover date for annotation. |
| 2 | **`service.instance` re-key** (#742) | Absorbed, same form. Single cutover, not waves. They cannot observe it — neither probe runs on their tenant. |
| 3 | **`network.address` scope** (#743) | Decided: keep the bare IP. Tenant isolation makes the cross-customer join impossible; ADR 0032's criterion does not apply to an address that is shared *by construction*; and scoping would destroy the host-route ↔ SNMP-device join the node exists for. Limitation documented instead: a tenant must not span disjoint LANs with overlapping RFC1918 ranges. |
| 4 | **Retire the stale half of our spec** | Ours to do — `ENTITY-DETECTION.md` §2 still describes `db.instance.id` as a network-derived composite. |

Two findings the review added that neither side had before:

- **The observer-identity symptom was half theirs** (§2). Our repair alone
  would not have removed it.
- **The transport guarantee is rail-dependent** (C1), and the agent's own
  Prometheus endpoint — whose existence the review initially missed on both
  sides — is a present gap, tracked as #745.

Still not on the table: no new entity type, no change to the type
vocabulary, no change to any technology-derived identity, no change to the
relation model or the wire shape.
