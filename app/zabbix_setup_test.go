package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/services/data_store/strategies/zabbix/template"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// The setup only knows the frontend URL. It printed host:10051 whatever
// the trapper port, and a server listening on another port got agents
// pointed at a closed one. The address it prints carries no port, which
// the agent completes with 10051.
func TestSetupPrintsNoPortItCannotKnow(t *testing.T) {
	for _, u := range []string{
		"https://zabbix.example.com",
		"http://zabbix.example.com:8080/zabbix",
		"zabbix.example.com/",
	} {
		if got := hostOf(u); got != "zabbix.example.com" {
			t.Errorf("hostOf(%q) = %q, want the bare host", u, got)
		}
	}
	if note := serverAddressNote("zabbix.example.com"); !strings.Contains(note, "10051") || !strings.Contains(note, "host:port") {
		t.Errorf("the note must say the default port and how to name another: %q", note)
	}
}

// Naming a probe adds it to the ones every machine runs. Replacing them
// was a trap: an operator adding one commercial template silently
// unlinked the processor, the memory, the network and the disks from the
// autoregistration action, and every host registering afterwards came up
// with none of them.
func TestNamingAProbeAddsItToTheDefaultOnes(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	defaults := withDefaultProbes(defs, "linux", nil)
	got := withDefaultProbes(defs, "linux", []string{"veeam"})
	for _, want := range defaults {
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
	if len(withDefaultProbes(defs, "linux", []string{defaults[0]})) != len(defaults) {
		t.Error("naming a probe that is already a default must not link it twice")
	}
}

func isLinked(list []string, p string) bool {
	for _, x := range list {
		if x == p {
			return true
		}
	}
	return false
}

// The host probes of a platform are linked without anyone naming them:
// on a Windows host the services and the event log counter come with
// the processor and the disks, on Linux they do not exist. Before, the
// default was a fixed list of five and a Windows host collecting its
// services got no item for them.
func TestTheDefaultLinkFollowsThePlatform(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	win := withDefaultProbes(defs, "windows", nil)
	lin := withDefaultProbes(defs, "linux", nil)
	for _, p := range []string{"cpu", "memory", "network", "logicaldisk", "process", "winservices", "windows_eventlog"} {
		if !isLinked(win, p) {
			t.Errorf("%s is not linked by default on Windows: %v", p, win)
		}
	}
	for _, p := range []string{"cpu", "memory", "network", "logicaldisk", "process"} {
		if !isLinked(lin, p) {
			t.Errorf("%s is not linked by default on Linux: %v", p, lin)
		}
	}
	for _, p := range []string{"winservices", "windows_eventlog", "hyperv"} {
		if isLinked(lin, p) {
			t.Errorf("%s is linked on Linux, where it cannot send anything", p)
		}
	}
}

// Guard: a registered probe that emits metrics must have a generated
// template that declares an item for it on some platform. The failure it
// prevents is a probe that collects and sends, and finds no item on the
// server: a definition whose metrics are all marked as not mapped, or a
// platform filter that empties it.
func TestEveryRegisteredMetricProbeHasATemplate(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	for name := range probes.GetRegisteredProbeTypes() {
		def, ok := defs[name]
		if !ok {
			// A probe with no definition is the business of the
			// definition completeness guard.
			continue
		}
		emits := false
		for _, m := range def.Metrics {
			if m.Otel == nil || !m.Otel.Skip {
				emits = true
			}
		}
		if !emits {
			continue
		}
		declared := false
		for _, platform := range supportedPlatforms {
			exp, err := template.Generate(def, template.Options{Platform: platform})
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if !exp.DeclaresNothing() {
				declared = true
			}
		}
		if !declared {
			t.Errorf("probe %q emits metrics but no generated template declares an item for it on any platform", name)
		}
	}
}

// Every probe the setup links by default is a probe a template exists
// for on that platform; the setup skips the ones with nothing to send.
func TestEveryDefaultLinkedProbeRendersATemplate(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	for _, platform := range supportedPlatforms {
		rendered, names, err := renderTemplates(withDefaultProbes(defs, platform, nil), template.Options{Platform: platform})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range withDefaultProbes(defs, platform, nil) {
			if _, ok := rendered[p]; !ok {
				t.Errorf("%s is linked by default on %s but renders no template", p, platform)
			}
		}
		if len(names) != len(rendered) {
			t.Errorf("%s: %d template names for %d rendered files", platform, len(names), len(rendered))
		}
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

// Zabbix before 6.4 reads the token only from the JSON-RPC envelope, and
// 8.0 refuses it there: each line must get the one it accepts, or every
// authenticated call answers "Not authorized".
func TestTheTokenTravelsWhereTheServerReadsIt(t *testing.T) {
	for _, bodyAuth := range []bool{false, true} {
		var header string
		var envelope map[string]interface{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header = r.Header.Get("Authorization")
			if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
				t.Errorf("reading the request: %v", err)
			}
			if err := json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "result": []interface{}{}, "id": 1}); err != nil {
				t.Errorf("answering the API call: %v", err)
			}
		}))
		api := newZabbixAPI(srv.URL, "tok")
		api.bodyAuth = bodyAuth
		if err := api.call("hostgroup.get", map[string]interface{}{}, nil); err != nil {
			t.Fatal(err)
		}
		srv.Close()
		_, inBody := envelope["auth"]
		if bodyAuth && (!inBody || header != "") {
			t.Errorf("pre-6.4: auth in body = %v, header = %q; want the body only", inBody, header)
		}
		if !bodyAuth && (inBody || header != "Bearer tok") {
			t.Errorf("6.4 and later: auth in body = %v, header = %q; want the header only", inBody, header)
		}
	}
}

func TestZabbixBefore(t *testing.T) {
	cases := []struct {
		version      string
		major, minor int
		want         bool
	}{
		{"6.0.48", 6, 4, true},
		{"6.0.48", 6, 2, true},
		{"6.4.0", 6, 4, false},
		{"7.0.30", 6, 4, false},
		{"8.0.0", 6, 2, false},
		{"unreadable", 6, 4, false},
	}
	for _, c := range cases {
		if got := zabbixBefore(c.version, c.major, c.minor); got != c.want {
			t.Errorf("zabbixBefore(%q, %d, %d) = %v, want %v", c.version, c.major, c.minor, got, c.want)
		}
	}
}
