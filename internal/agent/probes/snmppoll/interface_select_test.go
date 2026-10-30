package snmppoll

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

const seriesPerInterface = 9

// selectionSwitch is a synthetic chassis switch: 48 access ports (1-48,
// the first 10 up), 4 LAGs (49-52, all up) and 68 VLAN interfaces (53-120,
// all up but administratively down for the odd ones).
func selectionSwitch() (rows []ifaceRow, fc *fakeClient) {
	fc = &fakeClient{walkResult: map[string][]snmpVarBind{}, walkRawResult: map[string][]snmpRawBind{}}
	add := func(idx int, name string, typ, admin, oper int) {
		s := strconv.Itoa(idx)
		rows = append(rows, ifaceRow{Index: s, Name: name, IfType: typ, OperStatus: oper})
		fc.walkRawResult[ifDescr] = append(fc.walkRawResult[ifDescr],
			snmpRawBind{OID: ifDescr + "." + s, Value: []byte("descr-" + name)})
		fc.walkRawResult[ifAdminStatus] = append(fc.walkRawResult[ifAdminStatus],
			snmpRawBind{OID: ifAdminStatus + "." + s, Value: admin})
		for _, m := range builtinMIBs["if-mib"] {
			fc.walkResult[m.OID] = append(fc.walkResult[m.OID],
				snmpVarBind{OID: m.OID + "." + s, Value: float64(idx), IsNumeric: true})
		}
	}
	for i := 1; i <= 48; i++ {
		oper := 2
		if i <= 10 {
			oper = 1
		}
		add(i, "Gi1/0/"+strconv.Itoa(i), 6, 1, oper)
	}
	for i := 49; i <= 52; i++ {
		add(i, "Po"+strconv.Itoa(i-48), 161, 1, 1)
	}
	for i := 53; i <= 120; i++ {
		admin := 1
		if i%2 == 1 {
			admin = 2
		}
		add(i, "Vlan"+strconv.Itoa(i), 53, admin, 1)
	}
	return rows, fc
}

func selectionConfig(t *testing.T, raw map[string]interface{}) *config {
	t.Helper()
	cfg, err := parseConfig(map[string]interface{}{
		"target": "192.0.2.10", "mibs": []interface{}{"if-mib"}, "interfaces": raw,
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	return cfg
}

func seriesCount(t *testing.T, cfg *config, rows []ifaceRow, fc *fakeClient) int {
	t.Helper()
	opts := collectOptions{}
	if cfg.Interfaces.active() {
		collectSelectionFields(fc, rows, cfg.Interfaces)
		opts.SelectedInterfaces, _ = cfg.Interfaces.selectIndexes(rows)
	}
	names := map[string]string{}
	for _, r := range rows {
		names[r.Index] = r.Name
	}
	points, _ := collect(fc, cfg, "192.0.2.10:161", "serial:1:S1", names, time.Now(), testLogger(t), opts)
	return len(points)
}

func TestInterfaceSelectionBoundsTheSeriesCount(t *testing.T) {
	rows, fc := selectionSwitch()
	cases := []struct {
		name       string
		raw        map[string]interface{}
		interfaces int
	}{
		{"no selection keeps every interface", nil, 120},
		{"include by ifName", map[string]interface{}{"include": []interface{}{`^Gi1/0/`}}, 48},
		{"include matches ifDescr too", map[string]interface{}{"include": []interface{}{`^descr-Po`}}, 4},
		{"exclude drops VLAN interfaces", map[string]interface{}{"exclude": []interface{}{`^Vlan`}}, 52},
		{"ifType by name and number", map[string]interface{}{"types": []interface{}{"ethernetCsmacd", 161}}, 52},
		{"oper up only", map[string]interface{}{"oper_up_only": true}, 10 + 4 + 68},
		{"admin up only", map[string]interface{}{"admin_up_only": true}, 48 + 4 + 34},
		{"include, type and oper up combine", map[string]interface{}{
			"include": []interface{}{`^Gi`}, "types": []interface{}{"ethernetCsmacd"}, "oper_up_only": true}, 10},
		{"cap keeps the lowest ifIndex", map[string]interface{}{"max_interfaces": 20}, 20},
		{"cap above the match count changes nothing", map[string]interface{}{
			"exclude": []interface{}{`^Vlan`}, "max_interfaces": 500}, 52},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := selectionConfig(t, tc.raw)
			if got, want := seriesCount(t, cfg, rows, fc), tc.interfaces*seriesPerInterface; got != want {
				t.Errorf("series = %d, want %d (%d interfaces x %d)", got, want, tc.interfaces, seriesPerInterface)
			}
		})
	}
}

func TestInterfaceSelectionCapReportsTheMatchCount(t *testing.T) {
	rows, _ := selectionSwitch()
	cfg := selectionConfig(t, map[string]interface{}{"max_interfaces": 5})
	selected, matched := cfg.Interfaces.selectIndexes(rows)
	if matched != 120 || len(selected) != 5 {
		t.Fatalf("matched=%d kept=%d, want 120 matched and 5 kept", matched, len(selected))
	}
	for _, idx := range []string{"1", "2", "3", "4", "5"} {
		if !selected[idx] {
			t.Errorf("ifIndex %s should be kept (lowest indexes win, numerically)", idx)
		}
	}
}

func TestInterfaceSelectionFailsClosedOnUnreadState(t *testing.T) {
	rows, _ := selectionSwitch()
	cfg := selectionConfig(t, map[string]interface{}{"admin_up_only": true})
	failing := &fakeClient{walkRawErr: errWalkUnsupported}
	if skipped := collectSelectionFields(failing, rows, cfg.Interfaces); len(skipped) != 1 {
		t.Fatalf("skipped = %v, want the ifAdminStatus walk reported", skipped)
	}
	if selected, _ := cfg.Interfaces.selectIndexes(rows); len(selected) != 0 {
		t.Errorf("selected %d interfaces with admin state unread, want none", len(selected))
	}
}

func TestInterfaceSelectionNeverNarrowsTheTopologyRows(t *testing.T) {
	rows, fc := selectionSwitch()
	cfg := selectionConfig(t, map[string]interface{}{"include": []interface{}{`^Gi1/0/1$`}})
	collectSelectionFields(fc, rows, cfg.Interfaces)
	if selected, _ := cfg.Interfaces.selectIndexes(rows); len(selected) != 1 {
		t.Fatalf("selected = %d, want 1", len(selected))
	}
	if len(rows) != 120 {
		t.Errorf("inventory rows = %d, want all 120 kept for the entity rail", len(rows))
	}
}

func TestParseInterfaceSelectionRejectsBadInput(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"include": {"include": []interface{}{"("}},
		"exclude": {"exclude": []interface{}{"[a-"}},
		"types":   {"types": []interface{}{"ethernet-ish"}},
		"cap":     {"max_interfaces": 0},
		"oper":    {"oper_up_only": "maybe"},
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseConfig(map[string]interface{}{
				"target": "192.0.2.10", "mibs": []interface{}{"if-mib"}, "interfaces": raw,
			})
			if err == nil || !strings.Contains(err.Error(), "interfaces") {
				t.Fatalf("err = %v, want an error naming interfaces", err)
			}
		})
	}
}

func TestSelectedInterfacesIsEmptyBeforeTheFirstSweep(t *testing.T) {
	cfg := selectionConfig(t, map[string]interface{}{"oper_up_only": true})
	s := newEntitySource(cfg, testLogger(t))
	if got := s.SelectedInterfaces(); got == nil || len(got) != 0 {
		t.Errorf("SelectedInterfaces() = %v, want an empty non-nil set", got)
	}
	plain := newEntitySource(&config{Target: "192.0.2.10"}, testLogger(t))
	if plain.SelectedInterfaces() != nil {
		t.Error("without a selection the set must be nil (keep everything)")
	}
}

func TestAPartialSweepKeepsThePreviousSelection(t *testing.T) {
	prev := map[string]bool{"1": true, "2": true}
	if got := keepSelectionIfStale(prev, sweepResult{selectionStale: true}); len(got) != 2 {
		t.Errorf("stale sweep replaced the selection with %v", got)
	}
	fresh := map[string]bool{"3": true}
	if got := keepSelectionIfStale(prev, sweepResult{selectedIfaces: fresh}); !got["3"] || len(got) != 1 {
		t.Errorf("complete sweep should replace the selection, got %v", got)
	}
	if got := keepSelectionIfStale(nil, sweepResult{selectionStale: true}); got != nil {
		t.Errorf("no previous selection: got %v, want nil", got)
	}
}

func TestCapStateChangedWarnsOncePerCut(t *testing.T) {
	s := &snmpEntitySource{}
	steps := []struct {
		capped        bool
		matched, kept int
		want          bool
	}{
		{true, 120, 100, true},
		{true, 120, 100, false},
		{true, 120, 100, false},
		{true, 130, 100, true},
		{false, 90, 90, true},
		{false, 90, 90, false},
		{true, 130, 100, true},
	}
	for i, st := range steps {
		if got := s.capStateChanged(st.capped, st.matched, st.kept); got != st.want {
			t.Fatalf("step %d: capStateChanged(%v, %d, %d) = %v, want %v", i, st.capped, st.matched, st.kept, got, st.want)
		}
	}
}
