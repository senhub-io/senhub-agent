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
	connectErr error
	getResult  []snmpVarBind
	getErr     error
	walkResult map[string][]snmpVarBind // keyed by base OID
	walkErr    error
	closed     bool
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

	points := collect(fc, cfg, "192.0.2.10:161", time.Now(), testLogger(t))

	up, ok := find(points, "snmp.sys.uptime")
	if !ok || up.Value != 12345 {
		t.Fatalf("sys.uptime missing/wrong: %+v (ok=%v)", up, ok)
	}
	if tagVal(up, "metric_type") != "system" {
		t.Errorf("sys.uptime metric_type = %q, want system", tagVal(up, "metric_type"))
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

func TestCollect_BestEffortOnWalkError(t *testing.T) {
	cfg := &config{MIBs: []string{"if-mib"}}
	fc := &fakeClient{walkErr: errors.New("walk timeout")}

	// Should not panic or return partial garbage; failed walks are skipped.
	points := collect(fc, cfg, "192.0.2.10:161", time.Now(), testLogger(t))
	if len(points) != 0 {
		t.Errorf("expected no points when every walk fails, got %d", len(points))
	}
}
