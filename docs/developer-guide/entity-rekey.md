# Announcing an identity change

When a producer corrects the identity of an entity, the consumer sees the old
node stop being fed and a new one appear, with nothing linking them. Left
alone, the old node expires by liveness, which the change feed reports as
"the producer went quiet": a fleet-wide correction then looks like a mass
outage.

The producer therefore says it explicitly, with two announcements:

- an `entity.delete` on the **old** identity, with the reason `rekeyed`, so
  the retirement reads as a decision and not as silence;
- a `same_as` relation from the **new** identity to the old one, carrying
  `basis=rekey` and `confidence=1.0` (a `same_as` without a confidence is
  inert for the consumer), so both timelines stay joinable.

Both are unconditional: they are also sent where the old entity never
existed, which the consumer absorbs.

## Shared behaviour

Both announcers (package `internal/agent/services/entity`) behave the same way
over time.

- **Repetition.** `PublishEvent` is best effort and drops under backpressure,
  and at process start the entity pump may not be subscribed yet. The delete is
  therefore repeated over the first `RekeyAnnounceCycles` (3) observation
  cycles. The consumer's delete is idempotent.
- **Marker.** Once the window has closed with its announcements done, a small
  marker file is written in the state directory (`EnableRekeyMarkers`). A later
  start that finds it stays silent, because repeating the delete at every start
  would retract the old node for good. A failure to write the marker is
  reported once through the warn callback and the announcement is then repeated
  at the next start. Without a state directory the announcers are stateless.
- **Nil-safe.** A constructor returns `nil` when there is nothing to announce,
  and every method accepts a `nil` receiver.

## RekeyAnnouncer: one fixed pair

`NewRekeyAnnouncer` and `NewRekeyAnnouncerWithIDs` take one old identity and one
new identity, both known when the source starts. The source calls `Announce()`
once per cycle and adds the relation returned by `SameAs()` to its observation.
The marker is one file per pair.

Use it when the set of re-keyed entities is a single entity, or is known up
front and small. Examples: the identity of a database instance, or of the
probe's own `service.instance`.

## SetRekeyAnnouncer: a set discovered at runtime

`NewSetRekeyAnnouncer(name, derive, maxEntities, warn)` serves a source whose
re-keyed entities are only known as it discovers them: routes, neighbours,
virtual machines. It is built with a function that derives the legacy identity
(type and id map) from an entity as the source now emits it.

The source calls `BeginCycle()` at the start of each `Observe()`, then
`Announce(entity)` for each entity it emits. For each distinct entity, `Announce`
publishes the delete on the derived legacy identity and returns the `same_as`
relation to add to the observation.

Rules worth knowing:

- **Window.** A cycle counts toward the `RekeyAnnounceCycles` only if it
  announced at least one entity, so a source that has nothing to report before
  its first discovery finishes does not use the window up. An entity first seen
  late in the window is announced for the cycles that remain; one first seen
  after the window has closed is not announced.
- **Deduplication.** At most one delete per legacy identity per cycle. If two
  entities derive the same legacy identity, the first one seen keeps it and the
  others are ignored: an alias from two entities to one legacy node would merge
  them.
- **Bound.** At most `maxEntities` distinct entities are tracked, which bounds
  memory. Beyond it entities are dropped and the warn callback runs once.
- **Marker.** One marker per `name`, written once the window has closed with at
  least one entity announced. The window is advanced by the next
  `BeginCycle()`; `Close()` does the same for the last pending cycle, so a
  source that is shutting down can finish it.
- **Concurrency.** All state is behind one mutex.

Use it when the number of entities is not known at construction. When it is,
and it is one, prefer `RekeyAnnouncer`.
