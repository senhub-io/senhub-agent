package mysql

import "senhub-agent.go/internal/agent/probes"

// The names the paid mysql probe answered to before this one took over
// the type on the free tier (#476). Every one of them still works — the
// parser reads it and maps it onto the current spelling — so they are
// declared accepted, not removed. What the declaration buys is that an
// operator learns the current name instead of discovering years later
// that their file speaks a dialect nothing else does.
func init() {
	probes.RegisterLegacyParams(ProbeType, map[string]probes.LegacyParam{
		"expose_per_database": {
			Replacement: "per_database",
			Accepted:    true,
		},
		"expose_top_tables": {
			Replacement: "per_table + top_n_tables",
			Note:        "one key became two: per_table turns the family on, top_n_tables says how many",
			Accepted:    true,
		},
	})
}
