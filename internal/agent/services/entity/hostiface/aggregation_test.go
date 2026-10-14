package hostiface

import (
	"testing"

	"senhub-agent.go/internal/agent/services/entity"
)

func TestBuildObservation_AggregationOnlyWhenEnabled(t *testing.T) {
	old := entity.AggregationEnabled()
	defer entity.SetAggregationEnabled(old)

	ias := []ifaceAddrs{
		{Name: "bond0", IPs: []string{"10.0.0.5"}, Aggregation: &aggregationInfo{Mode: "lacp"}},
		{Name: "eth0", Aggregation: &aggregationInfo{State: "active", MasterName: "bond0"}},
		{Name: "eth1", Aggregation: &aggregationInfo{State: "standby", MasterName: "bond0"}},
	}

	entity.SetAggregationEnabled(true)
	obsTrue := buildObservation("h-1", ias)

	if got := relCount(obsTrue, entity.RelAggregatedInto); got != 2 {
		t.Errorf("aggregated_into count = %d, want 2", got)
	}

	bond0, ok := entityByID(obsTrue, entityTypeNetworkInterface, idKeyInterfaceName, "bond0")
	if !ok {
		t.Fatal("bond0 not emitted")
	}
	if got := bond0.Attributes[entity.AttrAggregationMode]; got != "lacp" {
		t.Errorf("bond0 aggregation mode = %v, want lacp", got)
	}

	eth0, ok := entityByID(obsTrue, entityTypeNetworkInterface, idKeyInterfaceName, "eth0")
	if !ok {
		t.Fatal("eth0 not emitted")
	}
	if got := eth0.Attributes[entity.AttrAggregationState]; got != "active" {
		t.Errorf("eth0 aggregation state = %v, want active", got)
	}

	eth1, ok := entityByID(obsTrue, entityTypeNetworkInterface, idKeyInterfaceName, "eth1")
	if !ok {
		t.Fatal("eth1 not emitted")
	}
	if got := eth1.Attributes[entity.AttrAggregationState]; got != "standby" {
		t.Errorf("eth1 aggregation state = %v, want standby", got)
	}

	entity.SetAggregationEnabled(false)
	obsFalse := buildObservation("h-1", ias)

	if got := relCount(obsFalse, entity.RelAggregatedInto); got != 0 {
		t.Errorf("aggregated_into count with aggregation disabled = %d, want 0", got)
	}

	bond0Disabled, _ := entityByID(obsFalse, entityTypeNetworkInterface, idKeyInterfaceName, "bond0")
	if _, present := bond0Disabled.Attributes[entity.AttrAggregationMode]; present {
		t.Errorf("aggregation mode must be omitted when disabled: %+v", bond0Disabled.Attributes)
	}
	eth0Disabled, _ := entityByID(obsFalse, entityTypeNetworkInterface, idKeyInterfaceName, "eth0")
	if _, present := eth0Disabled.Attributes[entity.AttrAggregationState]; present {
		t.Errorf("aggregation state must be omitted when disabled: %+v", eth0Disabled.Attributes)
	}
}
