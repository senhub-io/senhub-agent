package http

import (
	"context"
)

// The HTTP strategy cannot import the probe registry (package probes
// imports data_store), so the two operations that need a real probe —
// building one to see what it refuses, and running one collect cycle —
// are provided by the application at start-up through these hooks.
// When unset, validation stops at the schema and a test reports that
// collection is unavailable, rather than pretending.

// ProbeIssue is one parameter a probe could not read as written.
type ProbeIssue struct {
	Key  string
	Want string
	Got  interface{}
}

// ProbeChecker builds a probe from its params without starting it and
// reports the parameters it could not read and the error it returned.
var ProbeChecker func(probeType string, params map[string]interface{}) (issues []ProbeIssue, err error)

// OutputValidator runs a strategy's own parser on the params of an
// output, the check `config check` applies to a file. Provided by the
// strategy registry, which this package cannot import.
var OutputValidator func(outputType string, params map[string]interface{}) error

// ProbeCollector builds a probe, starts it, runs one collect cycle and
// stops it, returning what it collected. The context bounds the whole
// attempt; the caller keeps it short.
var ProbeCollector func(ctx context.Context, probeType string, params map[string]interface{}) ([]PreviewMetric, error)
