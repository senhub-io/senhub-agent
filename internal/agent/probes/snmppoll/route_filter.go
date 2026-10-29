package snmppoll

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// Routes the system-route filter leaves out may have been published before:
// by an earlier agent version, or by this process while include_system_routes
// was set. Left alone they would die of liveness expiry with no reason; they
// are retired explicitly as excluded_by_config instead, since the route still
// exists and only the configuration stops it being reported.

// maxRouteFilterAnnounced bounds the retired route identities a retirer
// remembers. Past it, further excluded routes leave by liveness expiry.
const maxRouteFilterAnnounced = 16384

type routeFilterBatch struct {
	entities []entity.Entity
	left     int
}

type routeFilterRetirer struct {
	mu        sync.Mutex
	deviceID  string
	announced map[string]bool
	batches   []routeFilterBatch
	publish   func(entity.Event)
}

func newRouteFilterRetirer(publish func(entity.Event)) *routeFilterRetirer {
	return &routeFilterRetirer{announced: map[string]bool{}, publish: publish}
}

// routeFilterEntity is the identity appendRouteEntities gives a route row
// normalized by eligibleRoute.
func routeFilterEntity(deviceID string, nr routeRow) entity.Entity {
	return entity.Entity{
		Type: entityTypeNetworkRoute,
		ID: map[string]any{
			idKeyNetworkDevice:    deviceID,
			idKeyRouteDestination: nr.Destination,
			attrNextHopIP:         nr.NextHop,
		},
		Scope: entity.ScopeSNMPRoute,
	}
}

// arm queues the retirement of the filtered routes not retired yet and
// returns how many it queued. A kept route is forgotten, so it is retired
// again if a later sweep filters it. resetOnChange clears the memory when
// the device id changes (the polled device); batches already queued for the
// previous id are still announced.
func (r *routeFilterRetirer) arm(deviceID string, filtered, kept []routeRow, resetOnChange bool) int {
	if r == nil || deviceID == "" {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if resetOnChange && r.deviceID != deviceID {
		r.announced = map[string]bool{}
		r.deviceID = deviceID
	}
	for _, nr := range kept {
		delete(r.announced, entityIdentity(routeFilterEntity(deviceID, nr)))
	}
	var batch []entity.Entity
	for _, nr := range filtered {
		e := routeFilterEntity(deviceID, nr)
		key := entityIdentity(e)
		if r.announced[key] {
			continue
		}
		if len(r.announced) >= maxRouteFilterAnnounced {
			break
		}
		r.announced[key] = true
		batch = append(batch, e)
	}
	if len(batch) > 0 {
		r.batches = append(r.batches, routeFilterBatch{entities: batch, left: entity.RekeyAnnounceCycles})
	}
	return len(batch)
}

// announce publishes every queued retirement once per observation cycle, for
// entity.RekeyAnnounceCycles cycles: a delete can be dropped under
// backpressure, and the consumer's delete is idempotent. A queued batch is
// never changed, so it is published outside the lock.
func (r *routeFilterRetirer) announce() {
	if r == nil {
		return
	}
	r.mu.Lock()
	batches := make([][]entity.Entity, 0, len(r.batches))
	var kept []routeFilterBatch
	for _, b := range r.batches {
		batches = append(batches, b.entities)
		if b.left--; b.left > 0 {
			kept = append(kept, b)
		}
	}
	r.batches = kept
	r.mu.Unlock()
	for _, entities := range batches {
		for i := range entities {
			e := entities[i]
			r.publish(entity.Event{Kind: entity.EntityDelete, Entity: &e, DeleteReason: entity.ReasonExcludedByConfig})
		}
	}
}
