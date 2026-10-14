package snmppoll

import (
	"testing"

	"github.com/gosnmp/gosnmp"

	"senhub-agent.go/internal/agent/services/entity"
)

func lagIfaces() []ifaceRow {
	return []ifaceRow{
		{Index: "1", Name: "Ethernet1", IfType: 6, OperStatus: ifOperUp, Mac: "00-1C-73-00-00-01"},
		{Index: "2", Name: "Ethernet2", IfType: 6, OperStatus: ifOperUp, Mac: "00-1C-73-00-00-02"},
		{Index: "3", Name: "Ethernet3", IfType: 6, OperStatus: ifOperUp},
		{Index: "10", Name: "Port-Channel1", IfType: ifTypeIEEE8023adLag, OperStatus: ifOperUp, Mac: "00-1C-73-00-00-01"},
		{Index: "30", Name: "Ethernet3.100", IfType: 135, OperStatus: ifOperUp},
	}
}

func stackBinds() []snmpRawBind {
	return []snmpRawBind{
		{OID: ifStackStatusOID + ".10.1", Type: gosnmp.Integer, Value: 1},
		{OID: ifStackStatusOID + ".10.2", Type: gosnmp.Integer, Value: 1},
		{OID: ifStackStatusOID + ".30.3", Type: gosnmp.Integer, Value: 1}, // VLAN sub-interface, not aggregation
		{OID: ifStackStatusOID + ".0.10", Type: gosnmp.Integer, Value: 1},
	}
}

func withAggregation(t *testing.T, on bool) {
	t.Helper()
	prev := entity.AggregationEnabled()
	entity.SetAggregationEnabled(on)
	t.Cleanup(func() { entity.SetAggregationEnabled(prev) })
}

// TestLag_LACPMembersAndState covers a LACP port-channel: both members
// aggregated into Port-Channel1, states from the actor oper state, mode lacp,
// and the VLAN sub-interface left alone.
func TestLag_LACPMembersAndState(t *testing.T) {
	withAggregation(t, true)
	fc := &fakeClient{walkRawResult: map[string][]snmpRawBind{
		ifStackStatusOID: stackBinds(),
		dot3adAggPortAttachedAggIDOID: {
			{OID: dot3adAggPortAttachedAggIDOID + ".1", Type: gosnmp.Integer, Value: 10},
			{OID: dot3adAggPortAttachedAggIDOID + ".2", Type: gosnmp.Integer, Value: 10},
		},
		dot3adAggPortActorOperStateOID: {
			{OID: dot3adAggPortActorOperStateOID + ".1", Type: gosnmp.OctetString, Value: []byte{0x3D}},
			{OID: dot3adAggPortActorOperStateOID + ".2", Type: gosnmp.OctetString, Value: []byte{0x0D}},
		},
	}}
	lag := collectLagData(fc, lagIfaces(), testLogger(t))
	if lag.members["1"] != "10" || lag.members["2"] != "10" || len(lag.members) != 2 {
		t.Fatalf("members = %v", lag.members)
	}
	if lag.memberState["1"] != "active" || lag.memberState["2"] != "standby" {
		t.Errorf("states = %v", lag.memberState)
	}
	if lag.aggregateMode["10"] != "lacp" {
		t.Errorf("mode = %v", lag.aggregateMode)
	}
}

// TestLag_StaticHasNoMemberState checks a port-channel without the LAG MIB
// is static and its members carry no state, whatever their link says.
func TestLag_StaticHasNoMemberState(t *testing.T) {
	withAggregation(t, true)
	fc := &fakeClient{walkRawResult: map[string][]snmpRawBind{ifStackStatusOID: stackBinds()}}
	lag := collectLagData(fc, lagIfaces(), testLogger(t))
	if lag.aggregateMode["10"] != "static" {
		t.Errorf("mode = %v", lag.aggregateMode)
	}
	if len(lag.memberState) != 0 {
		t.Errorf("static members must carry no state: %v", lag.memberState)
	}
}

// TestLag_AttachedAggIDAloneAddsThePair covers devices that fill only the
// LAG MIB.
func TestLag_AttachedAggIDAloneAddsThePair(t *testing.T) {
	withAggregation(t, true)
	fc := &fakeClient{walkRawResult: map[string][]snmpRawBind{
		dot3adAggPortAttachedAggIDOID: {{OID: dot3adAggPortAttachedAggIDOID + ".2", Type: gosnmp.Integer, Value: 10}},
	}}
	lag := collectLagData(fc, lagIfaces(), testLogger(t))
	if lag.members["2"] != "10" {
		t.Errorf("members = %v", lag.members)
	}
}

// TestLag_ApplyEmitsRelationAndAttributes checks the edge goes member →
// aggregate and only between interfaces present in the observation.
func TestLag_ApplyEmitsRelationAndAttributes(t *testing.T) {
	ifaces := lagIfaces()
	obs := buildObservation(deviceIdentity{Serial: "S1", VendorPEN: "9"}, lldpTopology{}, nil, ifaces, nil, resolveDeviceID)
	lag := lagData{
		members:       map[string]string{"1": "10", "2": "10", "99": "10"},
		memberState:   map[string]string{"1": "active"},
		aggregateMode: map[string]string{"10": "lacp"},
	}
	applyLag(&obs, lag, ifaces, "serial:9:S1")
	var edges []entity.Relation
	for _, r := range obs.Relations {
		if r.Type == entity.RelAggregatedInto {
			edges = append(edges, r)
		}
	}
	if len(edges) != 2 {
		t.Fatalf("aggregated_into edges = %+v, want 2 (ifIndex 99 is not in the observation)", edges)
	}
	for _, r := range edges {
		if r.ToID[idKeyInterfaceName] != "Port-Channel1" || r.FromType != entityTypeNetworkInterface {
			t.Errorf("edge = %+v", r)
		}
	}
	for _, e := range obs.Entities {
		switch e.ID[idKeyInterfaceName] {
		case "Ethernet1":
			if e.Attributes[entity.AttrAggregationState] != "active" {
				t.Errorf("Ethernet1 attrs = %v", e.Attributes)
			}
		case "Ethernet2":
			if _, ok := e.Attributes[entity.AttrAggregationState]; ok {
				t.Errorf("Ethernet2 has no LACP state, attribute must be absent: %v", e.Attributes)
			}
		case "Port-Channel1":
			if e.Attributes[entity.AttrAggregationMode] != "lacp" {
				t.Errorf("Port-Channel1 attrs = %v", e.Attributes)
			}
		}
	}
}

// countingClient counts the walks issued.
type countingClient struct {
	*fakeClient
	walks int
}

func (c *countingClient) WalkRaw(base string) ([]snmpRawBind, error) {
	c.walks++
	return c.fakeClient.WalkRaw(base)
}

// TestLag_DisabledIssuesNoWalk checks a disabled agent costs nothing.
func TestLag_DisabledIssuesNoWalk(t *testing.T) {
	withAggregation(t, false)
	c := &countingClient{fakeClient: &fakeClient{walkRawResult: map[string][]snmpRawBind{ifStackStatusOID: stackBinds()}}}
	lag := collectLagData(c, lagIfaces(), testLogger(t))
	if c.walks != 0 || len(lag.members) != 0 {
		t.Errorf("walks = %d, members = %v", c.walks, lag.members)
	}
}

// TestInterfaceMACOwners checks the aggregate never owns a MAC a member
// carries, and that a MAC on two physical interfaces is owned by none.
func TestInterfaceMACOwners(t *testing.T) {
	ifaces := append(lagIfaces(),
		ifaceRow{Index: "5", Name: "Ethernet5", IfType: 6, Mac: "00-1C-73-00-00-05"},
		ifaceRow{Index: "6", Name: "Ethernet6", IfType: 6, Mac: "00-1C-73-00-00-05"},
		ifaceRow{Index: "11", Name: "Port-Channel2", IfType: ifTypeIEEE8023adLag, Mac: "00-1C-73-00-00-11"},
	)
	got := interfaceMACOwners(ifaces)
	if got["00-1C-73-00-00-01"] != "Ethernet1" {
		t.Errorf("borrowed MAC owner = %q, want the member Ethernet1", got["00-1C-73-00-00-01"])
	}
	if _, ok := got["00-1C-73-00-00-05"]; ok {
		t.Errorf("a MAC on two physical interfaces must have no owner: %v", got)
	}
	if got["00-1C-73-00-00-11"] != "Port-Channel2" {
		t.Errorf("an aggregate with its own MAC owns it: %v", got)
	}
}
