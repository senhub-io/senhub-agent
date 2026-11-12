package extension

// OverlayTarget is the loaded configuration as an overlay may change it.
// It exposes only what an overlay is allowed to touch: whether a probe
// runs, its interval, whole managed probes and the entity detection
// switches. A local probe's connection parameters and credentials, and
// every output, stay out of reach.
type OverlayTarget interface {
	// Probes lists the probes of the local configuration, in file order.
	Probes() []OverlayProbe
	// SetProbeEnabled turns the named probe on or off. It returns false
	// when no probe has that name, or when it would enable a probe the
	// local file disables: the local file wins.
	SetProbeEnabled(name string, enabled bool) bool
	// SetProbeInterval replaces params.interval of the named probe, in
	// seconds. It returns false when no probe has that name or when
	// seconds is outside 10..3600.
	SetProbeInterval(name string, seconds int) bool
	// AddProbe appends a managed probe. It returns false, changing nothing,
	// when the name is empty or already used by any probe, the type is empty,
	// or Interval is non-zero and outside 10..3600, or Params carries
	// "interval" (the interval goes in Interval only).
	AddProbe(p ManagedProbe) bool
	// AdoptProbe replaces, in memory, the local probe that has p.Name with
	// the managed definition p: same name and same type. A parameter p
	// does not carry keeps its local value, secrets included; Interval,
	// when non-zero, replaces the local interval. The files are never
	// touched, and dropping the overlay brings the local probe back. It
	// returns false, changing nothing, when no local probe has that name,
	// the types differ, the local file disables the probe, the probe is
	// declared by SENHUB_PROBE_* environment variables, Interval is
	// non-zero and outside 10..3600, or Params carries "interval".
	AdoptProbe(p ManagedProbe) bool
	// Entities returns the entity detection switches.
	Entities() EntitySwitches
	// SetEntities replaces the entity detection switches.
	SetEntities(EntitySwitches)
}

// ManagedProbe is a whole probe defined by the overlay, not by a local file.
type ManagedProbe struct {
	Name     string
	Type     string
	Enabled  bool
	Interval int            // seconds, 0 = probe default; else 10..3600
	Params   map[string]any // JSON-decoded params, secret refs already resolved to "${secret:<name>}" strings
}

// OverlayProbe is one probe of the local configuration.
type OverlayProbe struct {
	Name string
	Type string
	// DisabledLocally is true when the local file says `enabled: false`.
	DisabledLocally bool
	// Interval is params.interval in seconds; zero when unset.
	Interval int
	// EnvDefined is true when SENHUB_PROBE_* environment variables declare
	// the probe or set one of its parameters.
	EnvDefined bool
	// Params are the probe's parameters as the file writes them, masked
	// for a remote reader: a ${...} reference stays a reference (it is
	// never resolved here), a literal under a secret-like name is "***",
	// and any other string goes through the log redaction (URL user
	// part, query values, tokens). Nil when the file cannot be read.
	Params map[string]any
}

// EntitySwitches are the entity detection switches an overlay may set.
type EntitySwitches struct {
	Enabled     bool
	DependsOn   bool
	Neighbors   bool
	Aggregation bool
}
