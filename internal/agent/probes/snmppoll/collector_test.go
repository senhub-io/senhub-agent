package snmppoll

import (
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

// fakeClient is a hand-rolled snmpClient stub (the codebase prefers
// concrete fakes over a mocking framework). Shared by the collector and
// probe tests.
type fakeClient struct {
	connectErr    error
	getResult     []snmpVarBind
	getErr        error
	walkResult    map[string][]snmpVarBind // keyed by base OID
	walkErr       error
	walkRawResult map[string][]snmpRawBind // keyed by base OID
	walkRawErr    error
	closed        bool
}

func (f *fakeClient) Connect() error                      { return f.connectErr }
func (f *fakeClient) Get([]string) ([]snmpVarBind, error) { return f.getResult, f.getErr }
func (f *fakeClient) Close() error                        { f.closed = true; return nil }
func (f *fakeClient) BulkWalk(base string) ([]snmpVarBind, error) {
	if f.walkErr != nil {
		return nil, f.walkErr
	}
	return f.walkResult[base], nil
}
func (f *fakeClient) WalkRaw(base string) ([]snmpRawBind, error) {
	if f.walkRawErr != nil {
		return nil, f.walkRawErr
	}
	return f.walkRawResult[base], nil
}

func testLogger(t *testing.T) *logger.ModuleLogger {
	t.Helper()
	return logger.NewModuleLogger(logger.NewLogger(&cliArgs.ParsedArgs{}), "test")
}

func find(points []data_store.DataPoint, name string) (data_store.DataPoint, bool) {
	for _, p := range points {
		if p.Name == name {
			return p, true
		}
	}
	return data_store.DataPoint{}, false
}

func tagVal(p data_store.DataPoint, key string) string {
	for _, tg := range p.Tags {
		if tg.Key == key {
			return tg.Value
		}
	}
	return ""
}

func TestBuildPlan_SplitsScalarsAndWalks(t *testing.T) {
	cfg := &config{
		MIBs: []string{"mib-2", "if-mib"},
		Custom: []customMapping{
			{OID: "1.3.6.1.4.1.9.1", Metric: "senhub.snmp.scalarVendor", Kind: kindGauge},
			{OID: "1.3.6.1.4.1.9.2", Metric: "senhub.snmp.tableVendor", Kind: kindGauge, IndexLabel: "row"},
		},
	}
	scalars, walks := buildPlan(cfg)
	// mib-2 sys.uptime (scalar) + 1 custom scalar = 2 scalars.
	if len(scalars) != 2 {
		t.Errorf("expected 2 scalars, got %d (%+v)", len(scalars), scalars)
	}
	// if-mib has 9 walked columns + 1 custom walked = 10 walks.
	if len(walks) != 10 {
		t.Errorf("expected 10 walks, got %d", len(walks))
	}
}

func TestCollect_ScalarsAndWalks(t *testing.T) {
	cfg := &config{
		MIBs: []string{"mib-2"},
		Custom: []customMapping{
			{OID: "1.3.6.1.4.1.9.2.1", Metric: "senhub.snmp.fanRpm", Kind: kindCounter, IndexLabel: "fan_index"},
		},
	}
	fc := &fakeClient{
		getResult: []snmpVarBind{
			{OID: "1.3.6.1.2.1.1.3.0", Value: 12345, IsNumeric: true}, // sysUpTime.0
		},
		walkResult: map[string][]snmpVarBind{
			"1.3.6.1.4.1.9.2.1": {
				{OID: "1.3.6.1.4.1.9.2.1.1", Value: 100, IsNumeric: true},
				{OID: "1.3.6.1.4.1.9.2.1.2", Value: 200, IsNumeric: true},
				{OID: "1.3.6.1.4.1.9.2.1.3", IsNumeric: false}, // skipped
			},
		},
	}

	points := collect(fc, cfg, "192.0.2.10:161", "", nil, time.Now(), testLogger(t))

	up, ok := find(points, "snmp.sys.uptime")
	if !ok || up.Value != 12345 {
		t.Fatalf("sys.uptime missing/wrong: %+v (ok=%v)", up, ok)
	}
	if tagVal(up, "metric_type") != "system" {
		t.Errorf("sys.uptime metric_type = %q, want system", tagVal(up, "metric_type"))
	}
	// Built-in metrics resolve via the YAML, so they must NOT carry the
	// typed-pass-through marker.
	if tagVal(up, "otel_type") != "" {
		t.Errorf("built-in sys.uptime must not carry otel_type, got %q", tagVal(up, "otel_type"))
	}

	fans := 0
	for _, p := range points {
		if p.Name == "senhub.snmp.fanRpm" {
			fans++
			if tagVal(p, "fan_index") == "" {
				t.Errorf("fan point missing fan_index tag: %+v", p)
			}
			if tagVal(p, "metric_type") != "snmp" {
				t.Errorf("fan metric_type = %q, want snmp", tagVal(p, "metric_type"))
			}
		}
	}
	if fans != 2 {
		t.Errorf("expected 2 fan points (non-numeric skipped), got %d", fans)
	}
}

func TestCollect_DynamicCustomMappingCanonicalName(t *testing.T) {
	// A custom mapping whose name is not yet namespaced gets the canonical
	// senhub.snmp.* prefix and an otel_type tag carrying its kind, so the
	// mapper can pass it through to OTLP/Prometheus (#207).
	cfg := &config{
		Custom: []customMapping{
			{OID: "1.3.6.1.4.1.9999.1", Metric: "vendor.temperature", Kind: kindGauge},
			{OID: "1.3.6.1.4.1.9999.2", Metric: "vendor.bytes", Kind: kindCounter},
		},
	}
	fc := &fakeClient{
		getResult: []snmpVarBind{
			{OID: "1.3.6.1.4.1.9999.1.0", Value: 28, IsNumeric: true},
			{OID: "1.3.6.1.4.1.9999.2.0", Value: 1000, IsNumeric: true},
		},
	}

	points := collect(fc, cfg, "10.0.0.1:161", "", nil, time.Now(), testLogger(t))

	temp, ok := find(points, "senhub.snmp.vendor.temperature")
	if !ok {
		t.Fatalf("dynamic custom metric not emitted under canonical name; points=%+v", points)
	}
	if tagVal(temp, "otel_type") != "gauge" {
		t.Errorf("otel_type = %q, want gauge", tagVal(temp, "otel_type"))
	}
	bytesP, ok := find(points, "senhub.snmp.vendor.bytes")
	if !ok {
		t.Fatal("counter custom metric not emitted under canonical name")
	}
	if tagVal(bytesP, "otel_type") != "counter" {
		t.Errorf("otel_type = %q, want counter", tagVal(bytesP, "otel_type"))
	}
}

func TestDynamicOtelName(t *testing.T) {
	cases := map[string]string{
		"vendor.temperature": "senhub.snmp.vendor.temperature",
		"senhub.snmp.fanRpm": "senhub.snmp.fanRpm",      // already namespaced, kept
		"weird name!":        "senhub.snmp.weird_name_", // sanitised
		"a/b":                "senhub.snmp.a_b",
	}
	for in, want := range cases {
		if got := dynamicOtelName(in); got != want {
			t.Errorf("dynamicOtelName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCollect_BestEffortOnWalkError(t *testing.T) {
	cfg := &config{MIBs: []string{"if-mib"}}
	fc := &fakeClient{walkErr: errors.New("walk timeout")}

	// Should not panic or return partial garbage; failed walks are skipped.
	points := collect(fc, cfg, "192.0.2.10:161", "", nil, time.Now(), testLogger(t))
	if len(points) != 0 {
		t.Errorf("expected no points when every walk fails, got %d", len(points))
	}
}

func TestCollect_CorrelationTags(t *testing.T) {
	// network.device.id (device-level) + interface.name (resolved from if_index)
	// tag the metrics with the SAME identity as the topology entities, so a
	// backend joins this interface's traffic to its network.interface entity.
	cfg := &config{MIBs: []string{"if-mib"}}
	fc := &fakeClient{walkResult: map[string][]snmpVarBind{
		"1.3.6.1.2.1.2.2.1.10": {{OID: "1.3.6.1.2.1.2.2.1.10.5", Value: 12345, IsNumeric: true}}, // ifInOctets @ ifIndex 5
	}}
	points := collect(fc, cfg, "10.0.0.1:161", "serial:9:S1", map[string]string{"5": "Gi0/5"}, time.Now(), testLogger(t))

	p, ok := find(points, "snmp.interface.in_octets")
	if !ok {
		t.Fatalf("interface metric not emitted; points=%+v", points)
	}
	if got := tagVal(p, "network.device.id"); got != "serial:9:S1" {
		t.Errorf("network.device.id = %q, want serial:9:S1", got)
	}
	if got := tagVal(p, "if_index"); got != "5" {
		t.Errorf("if_index = %q, want 5", got)
	}
	if got := tagVal(p, "interface.name"); got != "Gi0/5" {
		t.Errorf("interface.name = %q, want Gi0/5 (joins to the network.interface entity)", got)
	}
}

func TestCollect_NoCorrelationTagsBeforeSweep(t *testing.T) {
	// Before the first topology sweep deviceID/ifNames are empty → the
	// correlation tags are omitted, never emitted empty-valued.
	cfg := &config{MIBs: []string{"if-mib"}}
	fc := &fakeClient{walkResult: map[string][]snmpVarBind{
		"1.3.6.1.2.1.2.2.1.10": {{OID: "1.3.6.1.2.1.2.2.1.10.5", Value: 1, IsNumeric: true}},
	}}
	points := collect(fc, cfg, "10.0.0.1:161", "", nil, time.Now(), testLogger(t))

	p, ok := find(points, "snmp.interface.in_octets")
	if !ok {
		t.Fatal("interface metric not emitted")
	}
	if got := tagVal(p, "network.device.id"); got != "" {
		t.Errorf("network.device.id = %q, want absent before first sweep", got)
	}
	if got := tagVal(p, "interface.name"); got != "" {
		t.Errorf("interface.name = %q, want absent without ifNames", got)
	}
}
