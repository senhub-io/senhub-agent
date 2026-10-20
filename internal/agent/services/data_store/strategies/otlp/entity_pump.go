package otlp

import (
	"context"

	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

// entityPump consumes the neutral entity-event channel, encodes each Event
// to an OTel log Record and forwards it to the entity Logger. One pump per
// strategy instance. Mirrors logsPump's lifecycle (subscribe → drain until
// done/closed → unsubscribe), with the same drop-oldest backpressure on the
// subscription buffer.
type entityPump struct {
	pipeline *logsPipeline
	bufSize  int
	redact   map[string]struct{}
	log      *logger.ModuleLogger

	worker subscriptionWorker[entity.Event]
}

func newEntityPump(p *logsPipeline, bufSize int, redact map[string]struct{}, log *logger.ModuleLogger) *entityPump {
	if bufSize <= 0 {
		bufSize = 256
	}
	return &entityPump{pipeline: p, bufSize: bufSize, redact: redact, log: log}
}

// start subscribes to the entity-event channel and launches the drain
// goroutine. Idempotent.
func (p *entityPump) start() {
	p.worker.start(func() <-chan entity.Event {
		return entity.SubscribeEvents(p.bufSize)
	}, p.drain)
}

func (p *entityPump) drain(ctx context.Context, ch <-chan entity.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			scope, rec, err := buildEntityRecord(redactEntityEvent(ev, p.redact))
			if err != nil {
				// Surface, never silently drop — a malformed event is a
				// producer bug worth seeing.
				p.log.Warn().Err(err).Msg("dropping malformed entity event")
				continue
			}
			p.pipeline.emitEntityRecord(ctx, scope, rec)
		}
	}
}

// redactEntityEvent returns ev with the keys in redact removed from the
// entity's descriptive Attributes (signals.entities.redact_attributes,
// #682). The shared entity.Event fans out to every subscriber of
// entity.SubscribeEvents, so it is NEVER mutated: when at least one key
// matches, the entity struct is shallow-copied and given a fresh filtered
// Attributes map, and the copy rides only this strategy's returned Event.
// Entity.ID and relationship descriptors are identity, not description —
// they pass through untouched. No matching key means no copy.
func redactEntityEvent(ev entity.Event, redact map[string]struct{}) entity.Event {
	if len(redact) == 0 || ev.Entity == nil || len(ev.Entity.Attributes) == 0 {
		return ev
	}
	filtered := make(map[string]any, len(ev.Entity.Attributes))
	for k, v := range ev.Entity.Attributes {
		if _, drop := redact[k]; drop {
			continue
		}
		filtered[k] = v
	}
	if len(filtered) == len(ev.Entity.Attributes) {
		return ev
	}
	e := *ev.Entity
	e.Attributes = filtered
	ev.Entity = &e
	return ev
}

// stop cancels the drain goroutine and unsubscribes. Honors the caller's
// deadline; idempotent.
func (p *entityPump) stop(ctx context.Context) {
	p.worker.stop(ctx, entity.UnsubscribeEvents)
}
