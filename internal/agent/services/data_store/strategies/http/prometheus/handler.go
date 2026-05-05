package prometheus

import (
	"fmt"
	"io"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
)

// WriteExposition reads all cache entries, resolves each through the probe
// definition registry, and writes the Prometheus text exposition to w.
//
// agentRecords is the slice produced by BuildAgentRecords — these are the
// agent's own self-observability metrics (uptime, cache size, build info)
// and are always emitted alongside the probe metrics. Pass nil to omit
// them (used in some isolated unit tests).
//
// Metrics that are explicitly skipped (otel.skip: true) are silently dropped.
// Metrics with no OTel mapping or no matching definition generate a callback
// via errorHandler (if non-nil) and are skipped — the scrape continues
// rather than failing, so a single misconfigured probe doesn't break the
// whole /metrics output.
//
// Returns the number of OtelRecord lines written and the first error from
// the io.Writer (if any).
func WriteExposition(
	reader otelmapper.CacheReader,
	defs otelmapper.DefinitionLookup,
	agentRecords []otelmapper.OtelRecord,
	opts otelmapper.ResolveOptions,
	w io.Writer,
	errorHandler func(metric otelmapper.CacheMetric, err error),
) (int, error) {
	metrics := reader.GetAll()
	// Capacity is a lower-bound estimate; expand directives can multiply this
	// (4-state hw.status → 4× per cache entry). The slice grows transparently
	// — the hint just avoids the first few reallocations on small agents.
	allRecords := make([]otelmapper.OtelRecord, 0, len(agentRecords)+len(metrics))
	allRecords = append(allRecords, agentRecords...)

	for _, m := range metrics {
		def := defs.GetProbeDefinition(m.ProbeType)
		if def == nil {
			if errorHandler != nil {
				errorHandler(m, fmt.Errorf("no probe definition for probe_type=%q", m.ProbeType))
			}
			continue
		}
		recs, err := otelmapper.Resolve(def, m, opts)
		if err != nil {
			if errorHandler != nil {
				errorHandler(m, err)
			}
			continue
		}
		// recs may be nil when the metric is explicitly skipped — that's fine.
		allRecords = append(allRecords, recs...)
	}

	if err := SerializeToTextExposition(allRecords, w, SerializeOptions{}); err != nil {
		return len(allRecords), err
	}
	return len(allRecords), nil
}

// ContentType is the Content-Type header value for the text exposition format
// per the Prometheus spec.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"
