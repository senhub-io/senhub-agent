package http

import (
	"fmt"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/probes/spec"
)

// A probe the environment declares or adjusts is read-only in the
// console: a file written from here would be overridden at the next start.
func TestProbesSetByEnvironmentAreReadOnly(t *testing.T) {
	spec.Register(spec.Probe{Type: "envlock", DisplayName: "Env lock", Params: []spec.ParamSpec{
		{Key: "host", Kind: spec.KindString, Required: true},
	}})
	router, _ := newOutputsTestRouter(t)
	base := "/api/" + testAdminKey
	for _, n := range []string{"viafile", "viaenv"} {
		if code, resp := doJSON(t, router, "POST", base+"/config/probes", map[string]interface{}{
			"name": n, "type": "envlock", "params": map[string]interface{}{"host": "a"},
		}); code != 201 {
			t.Fatalf("create %s: %d %v", n, code, resp)
		}
	}
	t.Setenv("SENHUB_PROBE_VIAENV_HOST", "secret-looking-value")
	t.Setenv("SENHUB_PROBE_ENVONLY_TYPE", "envlock")
	t.Setenv("SENHUB_PROBE_ENVONLY_HOST", "h")

	for _, call := range []struct{ method string }{{"PUT"}, {"DELETE"}} {
		var body interface{}
		if call.method == "PUT" {
			body = map[string]interface{}{"name": "viaenv", "type": "envlock", "params": map[string]interface{}{"host": "b"}}
		}
		code, resp := doJSON(t, router, call.method, base+"/config/probes/viaenv", body)
		msg := strings.ToLower(fmt.Sprint(resp))
		if code != 409 || !strings.Contains(msg, "senhub_probe_viaenv_host") {
			t.Errorf("%s of an env probe: %d %v, want 409 naming the variable", call.method, code, resp)
		}
		if strings.Contains(msg, "secret-looking-value") {
			t.Errorf("%s: the refusal printed a value: %v", call.method, resp)
		}
	}

	if code, resp := doJSON(t, router, "PUT", base+"/config/probes/viafile", map[string]interface{}{
		"name": "viafile", "type": "envlock", "params": map[string]interface{}{"host": "c"},
	}); code != 200 {
		t.Errorf("a file probe must stay editable: %d %v", code, resp)
	}
	if code, resp := doJSON(t, router, "DELETE", base+"/config/probes/viafile", nil); code != 200 {
		t.Errorf("a file probe must stay deletable: %d %v", code, resp)
	}

	code, resp := doJSON(t, router, "GET", base+"/config/probes", nil)
	if code != 200 {
		t.Fatalf("list: %d %v", code, resp)
	}
	found := map[string]interface{}{}
	for _, it := range resp["probes"].([]interface{}) {
		p := it.(map[string]interface{})
		found[p["name"].(string)] = p["env_variables"]
	}
	if v, _ := found["viaenv"].([]interface{}); len(v) != 1 || v[0] != "SENHUB_PROBE_VIAENV_HOST" {
		t.Errorf("viaenv provenance = %v", found["viaenv"])
	}
	if v, _ := found["envonly"].([]interface{}); len(v) != 2 {
		t.Errorf("envonly provenance = %v", found["envonly"])
	}
}
