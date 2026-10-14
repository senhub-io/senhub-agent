package snmppoll

import (
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
)

func setEntitySourceState(p *snmppollProbe, deviceID string, ifNames map[string]string) {
	p.entitySource.interval = time.Minute
	p.entitySource.lastSweep = time.Now().Add(time.Hour)
	p.entitySource.deviceID = deviceID
	p.entitySource.ifNames = ifNames
	if deviceID != "" {
		p.entitySource.swept = true
	} else {
		p.entitySource.swept = false
	}
}

func TestCollect_IdentitySweepFailsTwiceThenSucceeds(t *testing.T) {
	fc := &fakeClient{
		getResult: []snmpVarBind{{OID: "1.3.6.1.2.1.1.3.0", Value: 12345, IsNumeric: true}},
	}
	probe := newTestProbe(t, baseRaw(), fc)
	setEntitySourceState(probe, "", nil)

	for i := 1; i <= 2; i++ {
		points, err := probe.Collect()
		if err != nil {
			t.Fatalf("cycle %d Collect: %v", i, err)
		}
		if _, ok := find(points, "snmp.sys.uptime"); ok {
			t.Fatalf("cycle %d emitted device metrics before identity resolved", i)
		}
	}

	probe.entitySource.deviceID = "serial:9:S1"
	probe.entitySource.swept = true

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect after sweep success: %v", err)
	}
	uptime, ok := find(points, "snmp.sys.uptime")
	if !ok {
		t.Fatalf("device metric not emitted after identity resolved")
	}
	if got := tagVal(uptime, "network.device.id"); got == "" {
		t.Fatalf("network.device.id not added after identity resolved")
	}
}

func TestCollect_IdentityNeverResolves_GivesUpKeyless(t *testing.T) {
	fc := &fakeClient{
		getResult: []snmpVarBind{{OID: "1.3.6.1.2.1.1.3.0", Value: 12345, IsNumeric: true}},
	}
	probe := newTestProbe(t, baseRaw(), fc)
	setEntitySourceState(probe, "", nil)

	for i := 1; i <= 3; i++ {
		points, err := probe.Collect()
		if err != nil {
			t.Fatalf("cycle %d Collect: %v", i, err)
		}
		if _, ok := find(points, "snmp.sys.uptime"); ok {
			t.Fatalf("cycle %d emitted device metrics before give-up", i)
		}
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatal(err)
	}
	uptime, ok := find(points, "snmp.sys.uptime")
	if !ok {
		t.Fatalf("device metric not emitted after give-up")
	}
	if got := tagVal(uptime, "network.device.id"); got != "" {
		t.Fatalf("keyless mode network.device.id = %q, want absent", got)
	}

	// A later successful topology sweep still leaves metric tags keyless.
	probe.entitySource.deviceID = "serial:9:S1"
	probe.entitySource.swept = true

	points, err = probe.Collect()
	if err != nil {
		t.Fatal(err)
	}
	uptime, ok = find(points, "snmp.sys.uptime")
	if !ok {
		t.Fatalf("device metric not emitted after later successful sweep")
	}
	if got := tagVal(uptime, "network.device.id"); got != "" {
		t.Fatalf("network.device.id = %q, want still absent after later sweep", got)
	}
}

func TestCollect_ResolvedIdentitySkipsUnnamedInterface(t *testing.T) {
	cfg := &config{MIBs: []string{"if-mib"}}
	fc := &fakeClient{walkResult: map[string][]snmpVarBind{
		"1.3.6.1.2.1.2.2.1.10": {
			{OID: "1.3.6.1.2.1.2.2.1.10.5", Value: 100, IsNumeric: true},
			{OID: "1.3.6.1.2.1.2.2.1.10.6", Value: 200, IsNumeric: true},
		},
	}}
	points, _ := collect(fc, cfg, "10.0.0.1:161", "serial:9:S1", map[string]string{"5": "Gi0/5"}, time.Now(), testLogger(t), collectOptions{SkipUnnamedInterfaces: true})

	var p5 data_store.DataPoint
	found5, found6 := false, false
	for _, p := range points {
		if p.Name != "snmp.interface.in_octets" {
			continue
		}
		switch tagVal(p, "if_index") {
		case "5":
			found5 = true
			p5 = p
		case "6":
			found6 = true
		}
	}

	if !found5 {
		t.Fatalf("named interface row was not emitted")
	}
	if got := tagVal(p5, "network.interface.name"); got != "Gi0/5" {
		t.Fatalf("network.interface.name = %q, want Gi0/5", got)
	}
	if found6 {
		t.Fatalf("unnamed interface row was emitted")
	}
}

func TestCollect_FirstCycleWithResolvedIdentityEmitsBothKeys(t *testing.T) {
	raw := map[string]interface{}{
		"target": "192.0.2.10",
		"mibs":   []interface{}{"mib-2", "if-mib"},
	}
	fc := &fakeClient{
		getResult: []snmpVarBind{{OID: "1.3.6.1.2.1.1.3.0", Value: 54321, IsNumeric: true}},
		walkResult: map[string][]snmpVarBind{
			"1.3.6.1.2.1.2.2.1.10": {{OID: "1.3.6.1.2.1.2.2.1.10.5", Value: 12345, IsNumeric: true}},
		},
	}
	probe := newTestProbe(t, raw, fc)
	setEntitySourceState(probe, "serial:9:S1", map[string]string{"5": "Gi0/5"})

	points, err := probe.Collect()
	if err != nil {
		t.Fatal(err)
	}

	uptime, ok := find(points, "snmp.sys.uptime")
	if !ok {
		t.Fatalf("device metric not emitted")
	}
	if got := tagVal(uptime, "network.device.id"); got != "serial:9:S1" {
		t.Errorf("device metric network.device.id = %q, want serial:9:S1", got)
	}

	iface, ok := find(points, "snmp.interface.in_octets")
	if !ok {
		t.Fatalf("interface metric not emitted")
	}
	if got := tagVal(iface, "network.device.id"); got != "serial:9:S1" {
		t.Errorf("interface network.device.id = %q, want serial:9:S1", got)
	}
	if got := tagVal(iface, "if_index"); got != "5" {
		t.Errorf("if_index = %q, want 5", got)
	}
	if got := tagVal(iface, "network.interface.name"); got != "Gi0/5" {
		t.Errorf("network.interface.name = %q, want Gi0/5", got)
	}
}

// TestCollect_UnreachableAtStartDoesNotGiveUp checks that cycles where the
// device answers nothing do not count toward giving up on its identity: a
// switch still booting must get its keys once it answers.
func TestCollect_UnreachableAtStartDoesNotGiveUp(t *testing.T) {
	timeout := errors.New("request timeout")
	fc := &fakeClient{getErr: timeout, walkErr: timeout, walkRawErr: timeout}
	probe := newTestProbe(t, baseRaw(), fc)
	setEntitySourceState(probe, "", nil)

	for i := 1; i <= keysWaitCycles+2; i++ {
		if _, err := probe.Collect(); err != nil {
			t.Fatalf("cycle %d Collect: %v", i, err)
		}
	}
	if probe.identityGaveUp {
		t.Fatal("an unreachable device made the probe give up on its identity")
	}

	fc.getErr, fc.walkErr, fc.walkRawErr = nil, nil, nil
	fc.getResult = []snmpVarBind{{OID: "1.3.6.1.2.1.1.3.0", Value: 12345, IsNumeric: true}}
	probe.entitySource.deviceID = "serial:9:S1"
	probe.entitySource.swept = true
	points, err := probe.Collect()
	if err != nil {
		t.Fatal(err)
	}
	uptime, ok := find(points, "snmp.sys.uptime")
	if !ok || tagVal(uptime, "network.device.id") != "serial:9:S1" {
		t.Fatalf("device metric after the device answered = %+v, want it with network.device.id", uptime)
	}
}

// TestCollect_DeviceWithoutInterfaceNamesKeepsItsInterfaceMetrics checks a
// device that names none of its interfaces still reports them, unnamed.
func TestCollect_DeviceWithoutInterfaceNamesKeepsItsInterfaceMetrics(t *testing.T) {
	raw := baseRaw()
	raw["mibs"] = []interface{}{"if-mib"}
	fc := &fakeClient{walkResult: map[string][]snmpVarBind{
		"1.3.6.1.2.1.2.2.1.10": {{OID: "1.3.6.1.2.1.2.2.1.10.5", Value: 100, IsNumeric: true}},
	}}
	probe := newTestProbe(t, raw, fc)
	setEntitySourceState(probe, "serial:9:S1", nil)

	points, err := probe.Collect()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := find(points, "snmp.interface.in_octets")
	if !ok {
		t.Fatal("interface metric dropped for a device that names no interface")
	}
	if tagVal(p, "network.device.id") != "serial:9:S1" || tagVal(p, "network.interface.name") != "" {
		t.Errorf("interface metric tags = %+v", p.Tags)
	}
}
