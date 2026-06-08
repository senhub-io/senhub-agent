package entity

import "time"

// HostIdentity is the identity + descriptive facts of the machine the agent
// runs on. ID is the stable machine identifier (machine-id / UUID), not the
// hostname — the hostname is descriptive and may change.
type HostIdentity struct {
	ID     string // host.id — stable across rename/reboot
	Name   string // host.name — descriptive
	OSType string // os.type — descriptive
}

// AgentIdentity is the identity + descriptive facts of the agent process.
// InstanceID is the persisted agent key (not the pid, not the hostname).
type AgentIdentity struct {
	InstanceID     string // service.instance.id — persisted agent key
	ServiceName    string // service.name — descriptive
	ServiceVersion string // service.version — descriptive
}

// DetectFoundation builds the Lot 1 entity events: the host the agent runs
// on, the agent's own service.instance, and the runs_on edge between them.
//
// It always returns the COMPLETE current descriptive attribute set per
// entity (entity_state is a full state, never a delta). now is the
// observation time and becomes each event's event_time. interval is the
// heartbeat period, carried so a consumer can use it as a liveness backstop.
//
// Order matters: both endpoint entities precede the relation so a single
// batch carries endpoints before the edge that references them.
func DetectFoundation(h HostIdentity, a AgentIdentity, now time.Time, interval time.Duration) []Event {
	host := &Entity{
		Type:       "host",
		ID:         map[string]any{"host.id": h.ID},
		Attributes: map[string]any{},
	}
	if h.Name != "" {
		host.Attributes["host.name"] = h.Name
	}
	if h.OSType != "" {
		host.Attributes["os.type"] = h.OSType
	}

	svc := &Entity{
		Type:       "service.instance",
		ID:         map[string]any{"service.instance.id": a.InstanceID},
		Attributes: map[string]any{},
	}
	if a.ServiceName != "" {
		svc.Attributes["service.name"] = a.ServiceName
	}
	if a.ServiceVersion != "" {
		svc.Attributes["service.version"] = a.ServiceVersion
	}

	runsOn := &Relation{
		Type:     "runs_on",
		FromType: "service.instance",
		FromID:   map[string]any{"service.instance.id": a.InstanceID},
		ToType:   "host",
		ToID:     map[string]any{"host.id": h.ID},
	}

	return []Event{
		{Kind: EntityState, Entity: host, Time: now, Interval: interval},
		{Kind: EntityState, Entity: svc, Time: now, Interval: interval},
		{Kind: RelationState, Relation: runsOn, Time: now, Interval: interval},
	}
}
