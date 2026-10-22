package extension

// OverlayTarget is the loaded configuration as an overlay may change it.
// It exposes only what an overlay is allowed to touch: whether a probe
// runs, its interval and the entity detection switches. Connection
// parameters, credentials and outputs stay out of reach.
type OverlayTarget interface {
	// Probes lists the probes of the local configuration, in file order.
	Probes() []OverlayProbe
	// SetProbeEnabled turns the named probe on or off. It returns false
	// when no probe has that name.
	SetProbeEnabled(name string, enabled bool) bool
	// SetProbeInterval replaces params.interval of the named probe. It
	// returns false when no probe has that name.
	SetProbeInterval(name string, seconds int) bool
	// Entities returns the entity detection switches.
	Entities() EntitySwitches
	// SetEntities replaces the entity detection switches.
	SetEntities(EntitySwitches)
}

// OverlayProbe is one probe of the local configuration.
type OverlayProbe struct {
	Name string
	Type string
	// DisabledLocally is true when the local file says `enabled: false`.
	DisabledLocally bool
	// Interval is params.interval in seconds; zero when unset.
	Interval int
}

// EntitySwitches are the entity detection switches an overlay may set.
type EntitySwitches struct {
	Enabled     bool
	DependsOn   bool
	Neighbors   bool
	Aggregation bool
}

// ApplyOverlays runs the overlay of every valid extension that declares
// one, in registration order.
func ApplyOverlays(t OverlayTarget) {
	for _, e := range Registered() {
		if e.Overlay != nil {
			e.Overlay(t)
		}
	}
}
