package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Naming a probe adds it to the ones every machine runs. Replacing them
// was a trap: an operator adding one commercial template silently
// unlinked the processor, the memory, the network and the disks from the
// autoregistration action, and every host registering afterwards came up
// with none of them.
func TestNamingAProbeAddsItToTheDefaultOnes(t *testing.T) {
	got := withDefaultProbes([]string{"veeam"})
	for _, want := range defaultSetupProbes {
		found := false
		for _, p := range got {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s is no longer linked when a probe is named; got %v", want, got)
		}
	}
	if got[len(got)-1] != "veeam" {
		t.Errorf("the named probe comes last; got %v", got)
	}
	if len(withDefaultProbes(nil)) != len(defaultSetupProbes) {
		t.Errorf("naming nothing links the defaults alone; got %v", withDefaultProbes(nil))
	}
	if len(withDefaultProbes([]string{defaultSetupProbes[0]})) != len(defaultSetupProbes) {
		t.Error("naming a probe that is already a default must not link it twice")
	}
}

// A dry run given a token reads the server but imports nothing, so the
// templates it would link have no id yet. Counting ids told an operator
// previewing the run that the action would link no template at all,
// where the real run links every one it imports.
func TestDryRunCountsTheTemplatesItWouldLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "result": []interface{}{}, "id": 1}); err != nil {
			t.Errorf("answering the API call: %v", err)
		}
	}))
	defer srv.Close()

	var out bytes.Buffer
	s := &zabbixSetup{
		api:    newZabbixAPI(srv.URL, "token"),
		dryRun: true,
		out:    &out,
		templates: map[string][]byte{
			"cpu": nil, "memory": nil, "network": nil,
		},
	}
	if err := s.ensureAction("SenHub (linux)", "senhub-agent linux", "0", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "linking 3 template(s)") {
		t.Errorf("a dry run must count the templates it would import; said %q", out.String())
	}
}
