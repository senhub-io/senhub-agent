package zabbix

import (
	"context"
	"testing"
)

// Measured against a real 7.0.30: an unchanged list comes back with
// neither data nor a revision. Reading that as an empty list would
// silence every item on the host until the next configuration change,
// which is worse than resending the list for ever.
func TestAnUnchangedCheckListLeavesTheItemsAlone(t *testing.T) {
	srv := newFakeServer(t)
	srv.setItems("senhub.k[p]", "senhub.k2[p]")
	srv.setConfigRevision(7)

	c, err := newClient(testConfig(srv.addr()))
	if err != nil {
		t.Fatal(err)
	}
	items, changed, err := c.activeChecks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(items) != 2 {
		t.Fatalf("first call: changed=%v items=%d", changed, len(items))
	}

	srv.setUnchanged()
	items, changed, err = c.activeChecks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("an unchanged reply was read as a change; the caller would have replaced its list with nothing")
	}
	if items != nil {
		t.Fatalf("items = %+v, want none: the caller keeps what it holds", items)
	}

	// What the agent declares about itself is what lets the server
	// answer that way at all.
	reqs := srv.requestsOf("active checks")
	if len(reqs) != 2 {
		t.Fatalf("requests = %d", len(reqs))
	}
	first, second := reqs[0], reqs[1]
	if first["version"] != advertisedAgent || first["session"] == "" {
		t.Errorf("the agent does not say what it is: %+v", first)
	}
	if first["config_revision"] != float64(0) {
		t.Errorf("first config_revision = %v, want 0", first["config_revision"])
	}
	if second["config_revision"] != float64(7) {
		t.Errorf("second config_revision = %v, want the revision the server gave", second["config_revision"])
	}
}
