// Package dbcommon holds the small pieces of code shared by the
// mysql and postgresql probes — engine-neutral helpers only. Things
// that need engine-specific SQL stay in their probe package; this
// package handles the concepts (role kind, metric_type families,
// top-N cap, environment detection) that both probes describe the
// same way.
//
// The split keeps the metric YAML readable: each probe references
// the same family names defined here, and the data_store sees a
// uniform tag vocabulary regardless of the engine.
package dbcommon

import (
	"sort"
	"strings"
)

// Role identifies the replication role detected for a database
// instance. The probe sets this once per collect cycle and uses it
// to gate the replication family (replica-only metrics are not
// emitted on a primary or standalone — see DESIGN §5.1).
type Role int

const (
	// RoleStandalone is a server that has no replication
	// configured. SHOW SLAVE STATUS empty AND no connected
	// replicas (MySQL); pg_is_in_recovery() = false AND no rows
	// in pg_stat_replication (PG).
	RoleStandalone Role = 0

	// RolePrimary is a server that has at least one downstream
	// replica connected.
	RolePrimary Role = 1

	// RoleReplica is a server that is itself catching up from an
	// upstream primary.
	RoleReplica Role = 2
)

// RoleValue is the numeric form emitted as the
// `senhub.db.replication.role` metric (matches the PRTG lookup
// senhub.db.replication.role.ovl).
func (r Role) RoleValue() float64 {
	return float64(r)
}

// String returns the human-readable form used as a tag value
// alongside the numeric metric — useful in Grafana legends and the
// PRTG channel name.
func (r Role) String() string {
	switch r {
	case RolePrimary:
		return "primary"
	case RoleReplica:
		return "replica"
	default:
		return "standalone"
	}
}

// MetricType is one of the family values that drives PRTG sensor
// chip selection and Prometheus dashboard organisation. The full
// list lives in docs/developer-guide/database-probes/DESIGN.md §4.
type MetricType string

const (
	MetricTypeOverview       MetricType = "overview"
	MetricTypeConnections    MetricType = "connections"
	MetricTypeThroughput     MetricType = "throughput"
	MetricTypeReplication    MetricType = "replication"
	MetricTypeCache          MetricType = "cache"
	MetricTypeLocks          MetricType = "locks"
	MetricTypeIO             MetricType = "io"
	MetricTypeStorage        MetricType = "storage"
	MetricTypePerDatabase    MetricType = "per_database"
	MetricTypePerTable       MetricType = "per_table"
	MetricTypeAutovacuum     MetricType = "autovacuum"
	MetricTypeEngine         MetricType = "engine"
	MetricTypeArchiver       MetricType = "archiver"
	MetricTypeBloat          MetricType = "bloat"
	MetricTypeStatStatements MetricType = "stat_statements"
)

// Environment is the heuristic-detected hosting environment for
// the monitored instance. Used as the senhub.db.environment tag so
// dashboards can split self-hosted vs managed without manual
// config. The detection is best-effort: a misclassification is
// harmless because every metric still works.
type Environment string

const (
	EnvironmentSelfHosted    Environment = "self_hosted"
	EnvironmentRDS           Environment = "rds"
	EnvironmentAurora        Environment = "aurora"
	EnvironmentCloudSQL      Environment = "cloudsql"
	EnvironmentAzureFlexible Environment = "azure_flexible"
	EnvironmentSupabase      Environment = "supabase"
	EnvironmentUnknown       Environment = "unknown"
)

// DetectEnvironment maps a free-form engine version string (the
// output of `version()` on PG, `@@version_comment` on MySQL) to
// an Environment value. The match is substring + case-insensitive
// to absorb minor wording differences across releases.
//
// Order matters: more specific matches (Aurora) are checked before
// more general ones (RDS — which also matches Aurora strings).
func DetectEnvironment(versionString string) Environment {
	s := strings.ToLower(versionString)
	switch {
	case strings.Contains(s, "aurora"):
		return EnvironmentAurora
	case strings.Contains(s, "rds") || strings.Contains(s, "amazon"):
		return EnvironmentRDS
	case strings.Contains(s, "google") || strings.Contains(s, "cloud sql"):
		return EnvironmentCloudSQL
	case strings.Contains(s, "azure") || strings.Contains(s, "microsoft"):
		return EnvironmentAzureFlexible
	case strings.Contains(s, "supabase"):
		return EnvironmentSupabase
	case s == "":
		return EnvironmentUnknown
	default:
		return EnvironmentSelfHosted
	}
}

// TopNBySize returns the indices of the `n` largest entries in
// `sizes`, in descending order. Used by both probes to cap
// per-table cardinality at scrape time (see DESIGN §6). Returns
// all indices if `n` is zero, negative, or larger than the slice.
//
// This is a small, pure helper rather than a sort+slice in every
// caller because the cap is applied at every scrape on every probe
// instance — keeping the allocation pattern in one place lets us
// optimise if it ever shows up in a profile.
func TopNBySize(sizes []int64, n int) []int {
	if len(sizes) == 0 {
		return nil
	}
	indices := make([]int, len(sizes))
	for i := range sizes {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool {
		return sizes[indices[i]] > sizes[indices[j]]
	})
	if n <= 0 || n >= len(indices) {
		return indices
	}
	return indices[:n]
}

// IsSystemDatabase reports whether the given database name belongs
// to the engine's internal namespace. Both probes call this to skip
// system databases by default when emitting per_database metrics
// (overridable via the `include_system_databases: true` config flag
// — see DESIGN §6).
func IsSystemDatabase(name string) bool {
	switch strings.ToLower(name) {
	case "mysql", "performance_schema", "information_schema", "sys",
		"postgres", "template0", "template1", "rdsadmin", "azure_sys",
		"azure_maintenance":
		return true
	}
	return false
}
