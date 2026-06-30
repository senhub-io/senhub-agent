package otlp

import (
	"context"
	"sync"
	"time"

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
	log      *logger.ModuleLogger

	mu         sync.Mutex
	subscribed <-chan entity.Event
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func newEntityPump(p *logsPipeline, bufSize int, log *logger.ModuleLogger) *entityPump {
	if bufSize <= 0 {
		bufSize = 256
	}
	return &entityPump{pipeline: p, bufSize: bufSize, log: log}
}

// start subscribes to the entity-event channel and launches the drain
// goroutine. Idempotent.
func (p *entityPump) start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.subscribed != nil {
		return
	}
	ch := entity.SubscribeEvents(p.bufSize)
	p.subscribed = ch
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.wg.Add(1)
	go p.drain(ctx, ch)
}

func (p *entityPump) drain(ctx context.Context, ch <-chan entity.Event) {
	defer p.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			scope, rec, err := buildEntityRecord(ev)
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

// stop cancels the drain goroutine and unsubscribes. Honors the caller's
// deadline; idempotent.
func (p *entityPump) stop(ctx context.Context) {
	p.mu.Lock()
	ch := p.subscribed
	cancel := p.cancel
	p.subscribed = nil
	p.cancel = nil
	p.mu.Unlock()

	if cancel == nil {
		return
	}

	cancel()
	if ch != nil {
		entity.UnsubscribeEvents(ch)
	}

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
	}
}
