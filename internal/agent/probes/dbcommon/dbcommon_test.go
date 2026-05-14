package dbcommon

import (
	"testing"
)

func TestRoleString(t *testing.T) {
	cases := []struct {
		role Role
		want string
	}{
		{RoleStandalone, "standalone"},
		{RolePrimary, "primary"},
		{RoleReplica, "replica"},
	}
	for _, c := range cases {
		if got := c.role.String(); got != c.want {
			t.Errorf("Role(%d).String() = %q, want %q", c.role, got, c.want)
		}
	}
}

func TestRoleValue(t *testing.T) {
	// The numeric values must match the PRTG lookup
	// senhub.db.replication.role.ovl. The constants below are the
	// contract — changing them is a breaking change for any
	// already-deployed sensor.
	if v := RoleStandalone.RoleValue(); v != 0 {
		t.Errorf("RoleStandalone.RoleValue() = %v, want 0", v)
	}
	if v := RolePrimary.RoleValue(); v != 1 {
		t.Errorf("RolePrimary.RoleValue() = %v, want 1", v)
	}
	if v := RoleReplica.RoleValue(); v != 2 {
		t.Errorf("RoleReplica.RoleValue() = %v, want 2", v)
	}
}

func TestDetectEnvironment(t *testing.T) {
	cases := []struct {
		version string
		want    Environment
	}{
		// Aurora must beat the generic RDS match (Aurora is also RDS).
		{"PostgreSQL 15.4 on aarch64-unknown-linux-gnu, compiled by aws-aurora-postgresql", EnvironmentAurora},
		{"8.0.32 Source distribution Amazon RDS", EnvironmentRDS},
		{"PostgreSQL 14.10 on x86_64-pc-linux-gnu, compiled by gcc (Google) 9.4.0", EnvironmentCloudSQL},
		{"PostgreSQL 15 — Azure Database for PostgreSQL Flexible Server", EnvironmentAzureFlexible},
		{"PostgreSQL 15.4 on Supabase", EnvironmentSupabase},
		// A vanilla self-hosted instance has none of the cloud
		// markers; the catch-all returns SelfHosted.
		{"PostgreSQL 16.1 on x86_64-pc-linux-gnu, compiled by gcc 11.4.0", EnvironmentSelfHosted},
		{"8.0.32 MySQL Community Server - GPL", EnvironmentSelfHosted},
		// Empty input is the explicit Unknown bucket; we want to
		// avoid silently returning SelfHosted when the probe could
		// not read the version (e.g. during a transient connection
		// failure).
		{"", EnvironmentUnknown},
	}
	for _, c := range cases {
		if got := DetectEnvironment(c.version); got != c.want {
			t.Errorf("DetectEnvironment(%q) = %q, want %q", c.version, got, c.want)
		}
	}
}

func TestTopNBySize_BasicOrdering(t *testing.T) {
	sizes := []int64{10, 100, 50, 1000, 5}
	// Top 3 by size, descending: 1000, 100, 50 — indices 3, 1, 2.
	got := TopNBySize(sizes, 3)
	want := []int{3, 1, 2}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: got %d, want %d", i, got[i], want[i])
		}
	}
}

func TestTopNBySize_EdgeCases(t *testing.T) {
	// n <= 0 → return all indices, still sorted by size.
	got := TopNBySize([]int64{10, 100, 50}, 0)
	if len(got) != 3 {
		t.Errorf("n=0 should return all entries, got %d", len(got))
	}

	// n larger than the slice → return all.
	got = TopNBySize([]int64{10, 100, 50}, 100)
	if len(got) != 3 {
		t.Errorf("n>len should return all entries, got %d", len(got))
	}

	// Empty slice → nil result.
	if got := TopNBySize(nil, 5); got != nil {
		t.Errorf("nil input should yield nil, got %v", got)
	}
}

func TestIsSystemDatabase(t *testing.T) {
	// Both engines' system names + the managed-DB internal
	// databases must all be flagged. The list is the contract for
	// the default skip-list applied when expose_per_database is on.
	cases := map[string]bool{
		"mysql":              true,
		"performance_schema": true,
		"information_schema": true,
		"sys":                true,
		"postgres":           true,
		"template0":          true,
		"template1":          true,
		"rdsadmin":           true,
		"azure_sys":          true,
		"azure_maintenance":  true,
		// Case-insensitivity matters: a user might create a DB
		// named "Postgres" thinking that's distinct from "postgres".
		"Postgres":           true,
		"INFORMATION_SCHEMA": true,
		// User databases must NOT be flagged.
		"production": false,
		"app_db":     false,
		"users":      false,
	}
	for name, want := range cases {
		if got := IsSystemDatabase(name); got != want {
			t.Errorf("IsSystemDatabase(%q) = %v, want %v", name, got, want)
		}
	}
}
