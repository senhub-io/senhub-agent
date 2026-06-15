package oracle

import (
	"context"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

func testLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

// TestNewOracleProbe_ParseError verifies that a misconfigured probe never
// constructs (the error surfaces at build time, not at first Collect).
func TestNewOracleProbe_ParseError(t *testing.T) {
	_, err := NewOracleProbe(map[string]interface{}{}, testLogger())
	if err == nil {
		t.Fatal("expected error for missing required fields, got nil")
	}
}

// TestNewOracleProbe_Valid verifies that a valid params block returns a
// probe without error and that the probe type is set.
func TestNewOracleProbe_Valid(t *testing.T) {
	raw := map[string]interface{}{
		"host":         "oracle.local",
		"service_name": "ORCL",
		"username":     "monitor",
	}
	p, err := NewOracleProbe(raw, testLogger())
	if err != nil {
		t.Fatalf("NewOracleProbe: %v", err)
	}
	if p == nil {
		t.Fatal("probe is nil")
	}
	if p.(*oracleProbe).GetProbeType() != ProbeType {
		t.Errorf("ProbeType = %q, want %q", p.(*oracleProbe).GetProbeType(), ProbeType)
	}
}

// TestCollect_DatabaseDown verifies the always-emit-up contract: even when
// the db is unreachable (db==nil), Collect returns senhub.db.up=0 and no error.
func TestCollect_DatabaseDown(t *testing.T) {
	raw := map[string]interface{}{
		"host":         "oracle.local",
		"service_name": "ORCL",
		"username":     "monitor",
	}
	p, err := NewOracleProbe(raw, testLogger())
	if err != nil {
		t.Fatalf("NewOracleProbe: %v", err)
	}
	op := p.(*oracleProbe)
	op.SetName("test-oracle")
	// db is nil — simulate a database that is not yet connected.

	points, err := op.Collect()
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	// Must emit at least senhub.db.up.
	if len(points) == 0 {
		t.Fatal("Collect returned no datapoints")
	}
	var upPoint *data_store.DataPoint
	for i := range points {
		if points[i].Name == "senhub.db.up" {
			upPoint = &points[i]
			break
		}
	}
	if upPoint == nil {
		t.Fatal("senhub.db.up not emitted")
	}
	if upPoint.Value != 0 {
		t.Errorf("senhub.db.up = %v with nil db, want 0", upPoint.Value)
	}
}

// TestCollect_EnrichesWithProbeName verifies that EnrichDataPointsWithProbeName
// runs — every datapoint must carry probe_name and probe_type.
func TestCollect_EnrichesWithProbeName(t *testing.T) {
	raw := map[string]interface{}{
		"host":         "oracle.local",
		"service_name": "ORCL",
		"username":     "monitor",
	}
	p, err := NewOracleProbe(raw, testLogger())
	if err != nil {
		t.Fatalf("NewOracleProbe: %v", err)
	}
	op := p.(*oracleProbe)
	op.SetName("test-oracle-enrichment")

	points, err := op.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, dp := range points {
		if !hasTag(dp.Tags, "probe_name", "test-oracle-enrichment") {
			t.Errorf("datapoint %s missing probe_name tag", dp.Name)
		}
		if !hasTag(dp.Tags, "probe_type", ProbeType) {
			t.Errorf("datapoint %s missing probe_type=%s tag", dp.Name, ProbeType)
		}
	}
}

// TestOnShutdown_NilDB verifies that OnShutdown is safe when the db was never
// opened (i.e. OnStart was never called).
func TestOnShutdown_NilDB(t *testing.T) {
	raw := map[string]interface{}{
		"host":         "oracle.local",
		"service_name": "ORCL",
		"username":     "monitor",
	}
	p, err := NewOracleProbe(raw, testLogger())
	if err != nil {
		t.Fatalf("NewOracleProbe: %v", err)
	}
	op := p.(*oracleProbe)
	if err := op.OnShutdown(context.Background()); err != nil {
		t.Errorf("OnShutdown with nil db returned error: %v", err)
	}
}

// TestNormalizeStatus checks the status normalization helper.
func TestNormalizeStatus(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ACTIVE", "active"},
		{"INACTIVE", "inactive"},
		{"KILLED", "killed"},
		{"SNIPED", "sniped"},
		{"CACHED", "cached"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := normalizeStatus(tc.in); got != tc.want {
				t.Errorf("normalizeStatus(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestPoint_Tags checks that point() emits the standard tag set.
func TestPoint_Tags(t *testing.T) {
	op := &oracleProbe{
		instance: "oracle://db.local:1521/ORCL",
		cfg:      config{Host: "db.local", Port: 1521, ServiceName: "ORCL"},
	}
	dp := op.point("oracle.sessions.count", 5, time.Now(), metricTypeConnections, map[string]string{"status": "active"})

	mustHaveTag := func(key, value string) {
		t.Helper()
		if !hasTag(dp.Tags, key, value) {
			t.Errorf("tag %s=%s missing; tags=%v", key, value, dp.Tags)
		}
	}
	mustHaveTag("metric_type", metricTypeConnections)
	mustHaveTag("instance", "oracle://db.local:1521/ORCL")
	mustHaveTag("db.system.name", "oracle")
	mustHaveTag("server.address", "db.local")
	mustHaveTag("server.port", "1521")
	mustHaveTag("status", "active")
}

func hasTag(tgs []tags.Tag, key, value string) bool {
	for _, t := range tgs {
		if t.Key == key && t.Value == value {
			return true
		}
	}
	return false
}
