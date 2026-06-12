package snmppoll

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func newTestProbe(t *testing.T, raw map[string]interface{}, fc *fakeClient) *snmppollProbe {
	t.Helper()
	p, err := NewSnmpPollProbe(raw, logger.NewLogger(&cliArgs.ParsedArgs{}))
	if err != nil {
		t.Fatalf("NewSnmpPollProbe: %v", err)
	}
	probe := p.(*snmppollProbe)
	probe.SetName("snmp-test")
	probe.newClient = func(*config) snmpClient { return fc }
	return probe
}

func baseRaw() map[string]interface{} {
	return map[string]interface{}{
		"target": "192.0.2.10",
		"mibs":   []interface{}{"mib-2"},
	}
}

func TestNewSnmpPollProbe_SetsType(t *testing.T) {
	probe := newTestProbe(t, baseRaw(), &fakeClient{})
	if probe.GetProbeType() != "snmp_poll" {
		t.Errorf("probe_type = %q, want snmp_poll", probe.GetProbeType())
	}
	if probe.GetName() != "snmp-test" {
		t.Errorf("name = %q, want snmp-test", probe.GetName())
	}
}

func TestCollect_Success(t *testing.T) {
	fc := &fakeClient{
		getResult: []snmpVarBind{
			{OID: "1.3.6.1.2.1.1.3.0", Value: 54321, IsNumeric: true},
		},
	}
	probe := newTestProbe(t, baseRaw(), fc)

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	uptime, ok := find(points, "snmp.sys.uptime")
	if !ok || uptime.Value != 54321 {
		t.Fatalf("sys.uptime missing/wrong: %+v (ok=%v)", uptime, ok)
	}
	// EnrichDataPointsWithProbeName contract.
	if tagVal(uptime, "probe_type") != "snmp_poll" || tagVal(uptime, "probe_name") != "snmp-test" {
		t.Errorf("enrichment tags missing: %+v", uptime.Tags)
	}

	up, ok := find(points, "senhub.snmp.up")
	if !ok || up.Value != 1 {
		t.Errorf("expected senhub.snmp.up=1, got %+v (ok=%v)", up, ok)
	}
	if tagVal(up, "instance") != "192.0.2.10:161" {
		t.Errorf("up instance tag = %q", tagVal(up, "instance"))
	}
	if _, ok := find(points, "senhub.snmp.poll.duration"); !ok {
		t.Errorf("missing senhub.snmp.poll.duration")
	}
}

func TestCollect_ConnectFailureEmitsDown(t *testing.T) {
	fc := &fakeClient{connectErr: errors.New("no route to host")}
	probe := newTestProbe(t, baseRaw(), fc)

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect should not error on connect failure: %v", err)
	}
	if _, ok := find(points, "snmp.sys.uptime"); ok {
		t.Errorf("no metric points expected when connect fails")
	}
	up, ok := find(points, "senhub.snmp.up")
	if !ok || up.Value != 0 {
		t.Errorf("expected senhub.snmp.up=0, got %+v (ok=%v)", up, ok)
	}
}

func TestOnShutdown_NoError(t *testing.T) {
	probe := newTestProbe(t, baseRaw(), &fakeClient{})
	if err := probe.OnShutdown(context.Background()); err != nil {
		t.Fatalf("OnShutdown: %v", err)
	}
}

// A self-contained MIB (same pattern as snmpmib's tests) under a private
// enterprise arc so it cannot collide with the process-global gosmi store.
const testProbeMIB = `TEST-SENHUBPOLL-MIB DEFINITIONS ::= BEGIN
pollRoot   OBJECT IDENTIFIER ::= { 1 3 6 1 4 1 99992 }
pollScalar OBJECT IDENTIFIER ::= { pollRoot 7 }
END
`

// TestNewProbe_MIBResolvesCustomMappingName pins the #291 acceptance:
// snmppoll resolves OIDs via snmpmib — a custom mapping may omit
// 'metric' when mib_paths are configured, and the resolved name feeds
// the canonical senhub.snmp.* dynamic naming.
func TestNewProbe_MIBResolvesCustomMappingName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "TEST-SENHUBPOLL-MIB"), []byte(testProbeMIB), 0o644); err != nil {
		t.Fatal(err)
	}

	raw := map[string]interface{}{
		"target":    "192.0.2.10",
		"mib_paths": []interface{}{dir},
		"custom_mappings": []interface{}{
			map[string]interface{}{"oid": "1.3.6.1.4.1.99992.7"},
		},
	}
	probe, err := NewSnmpPollProbe(raw, logger.NewLogger(&cliArgs.ParsedArgs{}))
	if err != nil {
		t.Fatalf("NewSnmpPollProbe: %v", err)
	}
	p := probe.(*snmppollProbe)
	if got := p.cfg.Custom[0].Metric; got != "pollScalar" {
		t.Errorf("resolved metric = %q, want pollScalar", got)
	}
}

func TestNewProbe_MIBResolutionFailsFast(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "TEST-SENHUBPOLL-MIB"), []byte(testProbeMIB), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("unresolvable OID with mib_paths", func(t *testing.T) {
		raw := map[string]interface{}{
			"target":    "192.0.2.10",
			"mib_paths": []interface{}{dir},
			"custom_mappings": []interface{}{
				map[string]interface{}{"oid": "1.3.6.1.4.1.424242.1"},
			},
		}
		if _, err := NewSnmpPollProbe(raw, logger.NewLogger(&cliArgs.ParsedArgs{})); err == nil {
			t.Fatal("expected an unresolvable-OID error")
		}
	})

	t.Run("no metric and no mib_paths stays an error", func(t *testing.T) {
		raw := map[string]interface{}{
			"target": "192.0.2.10",
			"custom_mappings": []interface{}{
				map[string]interface{}{"oid": "1.3.6.1.4.1.99992.7"},
			},
		}
		if _, err := NewSnmpPollProbe(raw, logger.NewLogger(&cliArgs.ParsedArgs{})); err == nil {
			t.Fatal("expected a missing-metric config error")
		}
	})
}
