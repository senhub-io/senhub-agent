// Package probes provides probe registration and instantiation.
//
// Probes register themselves via an init() function in their own
// package (see e.g. internal/agent/probes/cpu/register.go). Code that
// needs the registry populated must blank-import every probe package
// it wants available — that's what cmd/agent does. This package
// deliberately does NOT import any probe package, so the registry is
// pluggable and a build can drop probes by simply not importing them.
//
// The pattern is the standard Go self-registration / "registry of
// pluggables" used by database/sql, image, expvar, etc.
package probes

import (
	"fmt"
	"sort"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
)

// ProbeConstructor is the function signature every probe package
// exposes from its New<Name>Probe(). Configuration is the free-form
// `params` block from the probe YAML entry; the base logger is used
// to derive a module logger inside the probe.
type ProbeConstructor func(map[string]interface{}, *logger.Logger) (types.Probe, error)

// probeConstructors is the runtime catalogue of probe types known to
// this binary. Populated lazily by init() callbacks from each probe
// package (see RegisterProbe). NEVER hardcode entries here — that
// reintroduces the tight coupling this refactor removed.
var probeConstructors = map[string]ProbeConstructor{}

// RegisterProbe wires a probe constructor under the canonical type
// name used in the probe YAML (`type: <name>`). Intended to be called
// from an init() function in the probe's own package:
//
//	func init() {
//	    probes.RegisterProbe("cpu", NewCpuProbe)
//	}
//
// Panics on duplicate registration — that condition reflects a
// programmer error (two packages claiming the same name) and there
// is no recoverable behaviour. Detection at init time is the point:
// the binary refuses to start with an ambiguous catalogue.
func RegisterProbe(name string, ctor ProbeConstructor) {
	if name == "" {
		panic("probes.RegisterProbe: empty name")
	}
	if ctor == nil {
		panic(fmt.Sprintf("probes.RegisterProbe(%q): nil constructor", name))
	}
	if _, exists := probeConstructors[name]; exists {
		panic(fmt.Sprintf("probes.RegisterProbe(%q): duplicate registration", name))
	}
	probeConstructors[name] = ctor
}

// LookupProbeConstructor returns the constructor registered under
// the given probe type name, or false if no probe has been
// registered with that name in this build.
func LookupProbeConstructor(name string) (ProbeConstructor, bool) {
	ctor, ok := probeConstructors[name]
	return ctor, ok
}

// GetRegisteredProbeTypes returns a set of all registered probe type
// names. Reflects the current binary's catalogue, not a static list.
func GetRegisteredProbeTypes() map[string]bool {
	result := make(map[string]bool, len(probeConstructors))
	for name := range probeConstructors {
		result[name] = true
	}
	return result
}

// RegisteredProbeNames returns the registered probe names sorted
// alphabetically. Useful for deterministic listings (help screens,
// logs, structural tests).
func RegisteredProbeNames() []string {
	names := make([]string, 0, len(probeConstructors))
	for name := range probeConstructors {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
