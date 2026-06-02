package entity

import (
	"context"
	"time"
)

// HostIdentityFunc resolves the current host identity. It may fail (e.g. the
// OS host info is briefly unavailable); the detector skips that cycle.
type HostIdentityFunc func() (HostIdentity, error)

// AgentIdentityFunc resolves the agent's own identity. It does not fail —
// the agent key and build version are known once configured.
type AgentIdentityFunc func() AgentIdentity

// Detector drives Lot 1 emission: on a fixed cadence it observes the host
// and the agent and publishes the foundation events (host + service.instance
// + runs_on). Re-emitting the same state each tick is the heartbeat — the
// consumer coalesces identical states (at-least-once is idempotent), and the
// carried Interval lets it expire the entity if heartbeats stop.
//
// Lot 1 has no disappearing entities (the host and the agent are always
// present while the agent runs), so no delete logic is needed yet; the full
// state/delete lifecycle tracker arrives with Lot 2 (probe targets that come
// and go).
type Detector struct {
	host     HostIdentityFunc
	agent    AgentIdentityFunc
	interval time.Duration
	publish  func(Event)
	now      func() time.Time
}

// NewDetector builds a Detector. interval is the heartbeat cadence and is
// also carried on each event as the liveness backstop hint. publish defaults
// to PublishEvent and now to time.Now when nil (overridable in tests).
func NewDetector(host HostIdentityFunc, agent AgentIdentityFunc, interval time.Duration) *Detector {
	return &Detector{host: host, agent: agent, interval: interval}
}

// Run emits the foundation once immediately, then on every interval tick,
// until the context is cancelled. Blocks; run it in its own goroutine.
func (d *Detector) Run(ctx context.Context) {
	publish := d.publish
	if publish == nil {
		publish = PublishEvent
	}
	now := d.now
	if now == nil {
		now = time.Now
	}

	d.emitOnce(now(), publish)

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.emitOnce(now(), publish)
		}
	}
}

// emitOnce observes the host + agent at instant ts and publishes the
// foundation events. A failure to read host identity skips this cycle
// rather than emitting a host entity with an empty id.
func (d *Detector) emitOnce(ts time.Time, publish func(Event)) {
	h, err := d.host()
	if err != nil || h.ID == "" {
		return
	}
	a := d.agent()
	if a.InstanceID == "" {
		return
	}
	for _, ev := range DetectFoundation(h, a, ts, d.interval) {
		publish(ev)
	}
}
