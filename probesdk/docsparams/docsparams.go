// Package docsparams is the public mirror of the renderer that keeps a
// probe page's parameter table equal to its schema
// (senhub-agent.go/internal/docsparams). Probe packages from the separate
// senhub-agent-enterprise module use this mirror because Go forbids
// importing senhub-agent.go/internal/... across module boundaries; it
// lets them run the same generator, against the same documentation tree,
// rather than keeping a second renderer that would drift from this one.
package docsparams

import (
	idocs "senhub-agent.go/internal/docsparams"
	"senhub-agent.go/probesdk/spec"
)

// Markers delimit the generated block inside a page.
const (
	Start = idocs.Start
	End   = idocs.End
)

// Result reports what a Sync found.
type Result = idocs.Result

// Render returns the block a page carries between the markers.
func Render(p spec.Probe) string { return idocs.Render(p) }

// PageFor returns the page of a probe type under docsDir, or "" when it
// has none.
func PageFor(docsDir, probeType string) string { return idocs.PageFor(docsDir, probeType) }

// DocsDir returns the probe pages directory of the core checkout this
// build compiled against.
func DocsDir() (string, error) { return idocs.DocsDir() }

// Sync compares — and with update true rewrites — the generated block of
// every given probe's page under docsDir.
func Sync(docsDir string, probes []spec.Probe, update bool) (Result, error) {
	return idocs.Sync(docsDir, probes, update)
}
