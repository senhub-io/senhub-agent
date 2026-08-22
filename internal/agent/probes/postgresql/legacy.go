package postgresql

import "senhub-agent.go/internal/agent/probes"

// What the paid postgresql probe took and this one cannot (#476, #842).
//
// The connection parameters — `database`, `sslmode`, `sslrootcert` — are
// read again and documented, so they are not listed here: a warning
// about a parameter that works as written is noise.
//
// These three are different. They asked for per-database and per-table
// breakdowns this probe does not produce at all: it reads the
// cluster-wide views, where pg_stat_database is already summed. There is
// nothing to rename them to, so `agent config check` reports them as
// errors rather than letting them sit in a file looking like they do
// something.
func init() {
	probes.RegisterLegacyParams(ProbeType, map[string]probes.LegacyParam{
		"expose_per_database": {
			Note: "this probe reports cluster-wide totals and has no per-database breakdown to turn on",
		},
		"expose_top_tables": {
			Note: "this probe emits no per-table metrics",
		},
		"bloat_top_n": {
			Note: "this probe does not measure table bloat",
		},
	})
}
