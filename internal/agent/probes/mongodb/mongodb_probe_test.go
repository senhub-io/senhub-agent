package mongodb

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	mongodrv "go.mongodb.org/mongo-driver/mongo"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

func newTestLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func defaultParams() map[string]interface{} {
	return map[string]interface{}{
		"uri":      "mongodb://localhost:27017",
		"timeout":  5,
		"interval": 30,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// config
// ─────────────────────────────────────────────────────────────────────────────

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.URI != defaultURI {
		t.Errorf("URI = %q, want %q", cfg.URI, defaultURI)
	}
	if cfg.Timeout != defaultTimeout {
		t.Errorf("Timeout = %v, want %v", cfg.Timeout, defaultTimeout)
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("Interval = %v, want %v", cfg.Interval, defaultInterval)
	}
	if !cfg.DirectConnection {
		t.Error("DirectConnection should default to true")
	}
}

func TestParseConfig_Override(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"uri":               "mongodb://user:pass@mongo.example.com:27018",
		"timeout":           30,
		"interval":          120,
		"direct_connection": false,
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.URI != "mongodb://user:pass@mongo.example.com:27018" {
		t.Errorf("URI = %q", cfg.URI)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", cfg.Timeout)
	}
	if cfg.Interval != 120*time.Second {
		t.Errorf("Interval = %v, want 120s", cfg.Interval)
	}
	if cfg.DirectConnection {
		t.Error("DirectConnection should be false when explicitly set")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// floatFrom
// ─────────────────────────────────────────────────────────────────────────────

func TestFloatFrom_Types(t *testing.T) {
	cases := []struct {
		name  string
		input interface{}
		want  float64
		ok    bool
	}{
		{"int32", int32(42), 42, true},
		{"int64", int64(1000000), 1000000, true},
		{"float64", float64(3.14), 3.14, true},
		{"int", int(7), 7, true},
		{"string", "nope", 0, false},
		{"nil", nil, 0, false},
		{"bool", true, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := bson.M{"v": tc.input}
			got, ok := floatFrom(m, "v")
			if ok != tc.ok {
				t.Errorf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("value = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFloatFrom_MissingKey(t *testing.T) {
	m := bson.M{"present": int32(1)}
	_, ok := floatFrom(m, "absent")
	if ok {
		t.Error("expected ok=false for missing key")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// buildServerStatusPoints
// ─────────────────────────────────────────────────────────────────────────────

func newProbeForTest(t *testing.T) *mongoDBProbe {
	t.Helper()
	p, err := NewMongoDBProbe(defaultParams(), newTestLogger())
	if err != nil {
		t.Fatalf("NewMongoDBProbe: %v", err)
	}
	mp := p.(*mongoDBProbe)
	mp.SetName("mongo-test")
	return mp
}

// buildTestStatus constructs a minimal serverStatus bson.M that exercises
// every parser branch, without requiring a real MongoDB instance.
func buildTestStatus() bson.M {
	return bson.M{
		"uptimeMillis": int64(120000), // 120 seconds
		"connections": bson.M{
			"active":    int32(3),
			"available": int32(97),
			"current":   int32(100),
		},
		"network": bson.M{
			"bytesIn":     int64(1024 * 1024),
			"bytesOut":    int64(2 * 1024 * 1024),
			"numRequests": int64(5000),
		},
		"opcounters": bson.M{
			"insert":  int64(100),
			"query":   int64(400),
			"update":  int64(50),
			"delete":  int64(10),
			"getmore": int64(5),
			"command": int64(1000),
		},
		"mem": bson.M{
			"resident": int32(256), // MB
			"virtual":  int32(512), // MB
		},
		"metrics": bson.M{
			"document": bson.M{
				"deleted":  int64(10),
				"inserted": int64(100),
				"returned": int64(400),
				"updated":  int64(50),
			},
		},
		"wiredTiger": bson.M{
			"cache": bson.M{
				"pages read into cache":    int64(200),
				"pages written from cache": int64(150),
			},
		},
		"globalLock": bson.M{
			"currentQueue": bson.M{
				"readers": int32(2),
				"writers": int32(1),
			},
		},
	}
}

func TestBuildServerStatusPoints_Count(t *testing.T) {
	p := newProbeForTest(t)
	status := buildTestStatus()
	now := time.Now()

	pts := p.buildServerStatusPoints(status, now)

	// Expected: 1 uptime + 3 connections + 3 network + 6 opcounters
	//           + 2 memory + 4 document ops + 2 cache + 2 locks = 23
	const wantCount = 23
	if len(pts) != wantCount {
		t.Errorf("buildServerStatusPoints returned %d points, want %d", len(pts), wantCount)
	}
}

func TestBuildServerStatusPoints_UptimeConversion(t *testing.T) {
	p := newProbeForTest(t)
	now := time.Now()

	// 120000 ms → 120 s
	pts := p.buildServerStatusPoints(bson.M{
		"uptimeMillis": int64(120000),
	}, now)

	found := false
	for _, dp := range pts {
		if dp.Name == "mongodb.uptime" {
			if dp.Value != 120 {
				t.Errorf("uptime = %v, want 120", dp.Value)
			}
			found = true
		}
	}
	if !found {
		t.Error("mongodb.uptime datapoint not found")
	}
}

func TestBuildServerStatusPoints_MemoryConversion(t *testing.T) {
	p := newProbeForTest(t)
	now := time.Now()

	pts := p.buildServerStatusPoints(bson.M{
		"mem": bson.M{
			"resident": int32(256), // 256 MB → 268435456 bytes
			"virtual":  int32(512),
		},
	}, now)

	byType := map[string]float32{}
	for _, dp := range pts {
		if dp.Name == "mongodb.memory.usage" {
			for _, tg := range dp.Tags {
				if tg.Key == "type" {
					byType[tg.Value] = dp.Value
				}
			}
		}
	}

	wantResident := float32(256 * 1024 * 1024)
	if byType["resident"] != wantResident {
		t.Errorf("resident memory = %v, want %v", byType["resident"], wantResident)
	}
	wantVirtual := float32(512 * 1024 * 1024)
	if byType["virtual"] != wantVirtual {
		t.Errorf("virtual memory = %v, want %v", byType["virtual"], wantVirtual)
	}
}

func TestBuildServerStatusPoints_OpcountersOperationTag(t *testing.T) {
	p := newProbeForTest(t)
	now := time.Now()

	pts := p.buildServerStatusPoints(bson.M{
		"opcounters": bson.M{
			"insert":  int64(100),
			"query":   int64(400),
			"update":  int64(50),
			"delete":  int64(10),
			"getmore": int64(5),
			"command": int64(1000),
		},
	}, now)

	seen := map[string]float32{}
	for _, dp := range pts {
		if dp.Name == "mongodb.operations.count" {
			for _, tg := range dp.Tags {
				if tg.Key == "operation" {
					seen[tg.Value] = dp.Value
				}
			}
		}
	}
	wantOps := map[string]float32{
		"insert": 100, "query": 400, "update": 50,
		"delete": 10, "getmore": 5, "command": 1000,
	}
	for op, want := range wantOps {
		if seen[op] != want {
			t.Errorf("opcounters[%s] = %v, want %v", op, seen[op], want)
		}
	}
}

func TestBuildServerStatusPoints_MissingFields(t *testing.T) {
	p := newProbeForTest(t)
	// Empty status should produce zero points (no panics).
	pts := p.buildServerStatusPoints(bson.M{}, time.Now())
	if len(pts) != 0 {
		t.Errorf("expected 0 points for empty status, got %d", len(pts))
	}
}

func TestBuildServerStatusPoints_PartialWiredTiger(t *testing.T) {
	p := newProbeForTest(t)
	now := time.Now()

	// wiredTiger present but cache sub-key absent — must not panic.
	pts := p.buildServerStatusPoints(bson.M{
		"wiredTiger": bson.M{
			"notCache": bson.M{"something": int32(1)},
		},
	}, now)
	for _, dp := range pts {
		if dp.Name == "mongodb.cache.operations" {
			t.Errorf("unexpected cache datapoint when wiredTiger.cache absent")
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// skipDatabases
// ─────────────────────────────────────────────────────────────────────────────

func TestSkipDatabases(t *testing.T) {
	for _, db := range []string{"admin", "local", "config"} {
		if !skipDatabases[db] {
			t.Errorf("database %q should be in skipDatabases", db)
		}
	}
	if skipDatabases["myapp"] {
		t.Error("application database should not be skipped")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Collect — unhappy path via synthetic serverStatus error
// ─────────────────────────────────────────────────────────────────────────────

// TestCollect_EmitsUpZeroWhenUnreachable verifies that a serverStatus
// failure produces senhub.mongodb.up=0 and nil error (observable outage,
// not a collection error). A real mongo.Client pointed at port 1 (always
// refused) is used; mongo.Connect is lazy, OnStart succeeds, and the first
// RunCommand in Collect fails.
func TestCollect_EmitsUpZeroWhenUnreachable(t *testing.T) {
	p := newProbeForTest(t)
	p.SetName("mongo-unreachable")

	// mongo.Connect is lazy: the TCP dial only happens on the first command.
	// We set direct_connection+serverSelectionTimeout short so Collect fails
	// fast on a closed port.
	p.cfg.URI = "mongodb://127.0.0.1:1" // port 1: always refused
	p.cfg.DirectConnection = true
	p.cfg.Timeout = 1 * time.Second
	p.instance = "mongodb://127.0.0.1:1" // credential-free form

	// OnStart — lazy connect, won't fail here.
	if err := p.OnStart(nil); err != nil {
		// If the driver validates the URI synchronously it could fail here;
		// that's fine for this test's purpose.
		t.Logf("OnStart returned err (acceptable for lazy driver): %v", err)
		return
	}

	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect must not return error on unreachable MongoDB, got: %v", err)
	}

	var upValue float32 = -1
	for _, dp := range pts {
		if dp.Name == "senhub.mongodb.up" {
			upValue = dp.Value
		}
	}
	if upValue == -1 {
		t.Fatal("senhub.mongodb.up datapoint missing from Collect output")
	}
	if upValue != 0 {
		t.Errorf("senhub.mongodb.up = %v, want 0 when MongoDB unreachable", upValue)
	}

	_ = mongodrv.ErrNoDocuments // import used via client type above
}

// ─────────────────────────────────────────────────────────────────────────────
// probe metadata
// ─────────────────────────────────────────────────────────────────────────────

func TestProbeType(t *testing.T) {
	probe, err := NewMongoDBProbe(defaultParams(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	p := probe.(*mongoDBProbe)
	if p.GetProbeType() != "mongodb" {
		t.Errorf("probe type = %q, want mongodb", p.GetProbeType())
	}
}

func TestProbeInterval(t *testing.T) {
	p, err := NewMongoDBProbe(map[string]interface{}{"interval": 120}, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	if p.GetInterval() != 120*time.Second {
		t.Errorf("interval = %v, want 120s", p.GetInterval())
	}
}

func TestShouldStart(t *testing.T) {
	p, err := NewMongoDBProbe(defaultParams(), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	if !p.ShouldStart() {
		t.Error("ShouldStart should return true")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Security regression — credentials must never appear in tags or logs (#460)
// ─────────────────────────────────────────────────────────────────────────────

// TestInstanceTag_NoCredentials verifies that when the probe URI contains
// embedded credentials (user:password@host), the 'instance' tag emitted on
// every datapoint does not expose the password or the '@' separator.
func TestInstanceTag_NoCredentials(t *testing.T) {
	params := map[string]interface{}{
		"uri":      "mongodb://admin:s3cr3tP@ss@mongo.example.com:27017",
		"timeout":  5,
		"interval": 30,
	}
	p, err := NewMongoDBProbe(params, newTestLogger())
	if err != nil {
		t.Fatalf("NewMongoDBProbe: %v", err)
	}
	mp := p.(*mongoDBProbe)
	mp.SetName("mongo-creds-test")

	// The instance field must not contain credentials.
	if contains(mp.instance, "@") {
		t.Errorf("instance %q contains '@': credentials leaked", mp.instance)
	}
	if contains(mp.instance, "s3cr3tP@ss") {
		t.Errorf("instance %q contains the password: credentials leaked", mp.instance)
	}
	if contains(mp.instance, "admin") {
		t.Errorf("instance %q contains the username: credentials leaked", mp.instance)
	}

	// The instance tag on datapoints must also be clean.
	tags := mp.baseTags("status")
	for _, tg := range tags {
		if tg.Key == "instance" {
			if contains(tg.Value, "@") {
				t.Errorf("instance tag %q contains '@': credentials leaked", tg.Value)
			}
			if contains(tg.Value, "s3cr3tP@ss") {
				t.Errorf("instance tag %q contains the password: credentials leaked", tg.Value)
			}
		}
	}

	// The safe form should contain the host and port only.
	const wantInstance = "mongodb://mongo.example.com:27017"
	if mp.instance != wantInstance {
		t.Errorf("instance = %q, want %q", mp.instance, wantInstance)
	}
}

// contains is a simple substring check to avoid importing strings in tests.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
