package http

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

func registerConsoleTestProbes() {
	if _, has := spec.For("exec"); !has {
		spec.Register(spec.Probe{Type: "exec", DisplayName: "Exec", Params: []spec.ParamSpec{
			{Key: "command", Kind: spec.KindString, Required: true, Executable: true},
		}})
	}
	if _, has := spec.For("ctl_plain"); !has {
		spec.Register(spec.Probe{Type: "ctl_plain", DisplayName: "Plain", Params: []spec.ParamSpec{
			{Key: "host", Kind: spec.KindString, Required: true},
		}})
		spec.Register(spec.Probe{Type: "ctl_tool", DisplayName: "Tool", Params: []spec.ParamSpec{
			{Key: "host", Kind: spec.KindString},
			{Key: "tool_path", Kind: spec.KindString, Default: "toolctl", Executable: true},
		}})
	}
}

// consoleRouter serves the console with the given console block, set
// both in the running parameters and in the output's file, as a loaded
// configuration has it.
func consoleRouter(t *testing.T, console map[string]interface{}) (*mux.Router, string) {
	t.Helper()
	registerConsoleTestProbes()
	file := "http:\n  port: 8080\n  endpoints: [\"web\", \"prtg\"]\n"
	var extra map[string]interface{}
	if console != nil {
		extra = map[string]interface{}{"console": console}
		file += "  console:\n"
		for k, v := range console {
			file += fmt.Sprintf("    %s: %v\n", k, v)
		}
	}
	return newOutputsTestRouterWith(t, extra, file)
}

func TestParseConsolePolicy(t *testing.T) {
	cases := []struct {
		name    string
		params  map[string]interface{}
		want    ConsolePolicy
		wantErr bool
	}{
		{"absent is today's behaviour", map[string]interface{}{}, ConsolePolicy{AllowExec: true}, false},
		{"read only", map[string]interface{}{"console": map[string]interface{}{"read_only": true}}, ConsolePolicy{ReadOnly: true, AllowExec: true}, false},
		{"no exec", map[string]interface{}{"console": map[string]interface{}{"allow_exec": false}}, ConsolePolicy{}, false},
		{"yaml v2 keys", map[string]interface{}{"console": map[interface{}]interface{}{"allow_exec": false}}, ConsolePolicy{}, false},
		{"not a bool", map[string]interface{}{"console": map[string]interface{}{"read_only": "yes"}}, ConsolePolicy{ReadOnly: true}, true},
		{"unknown key", map[string]interface{}{"console": map[string]interface{}{"readonly": true}}, ConsolePolicy{ReadOnly: true}, true},
		{"not a block", map[string]interface{}{"console": true}, ConsolePolicy{ReadOnly: true}, true},
	}
	for _, c := range cases {
		got, err := ParseConsolePolicy(c.params)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", c.name, err, c.wantErr)
		}
		if got != c.want {
			t.Errorf("%s: policy = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestConsolePolicyModes(t *testing.T) {
	for policy, want := range map[ConsolePolicy]string{
		{AllowExec: true}:                 "full",
		{}:                                "no-exec",
		{ReadOnly: true, AllowExec: true}: "read-only",
		{ReadOnly: true}:                  "read-only",
	} {
		if got := policy.Mode(); got != want {
			t.Errorf("%+v: mode %q, want %q", policy, got, want)
		}
	}
	if (ConsolePolicy{ReadOnly: true, AllowExec: true}).ExecAllowed() {
		t.Error("a read-only console must not allow exec even with allow_exec true")
	}
}

// Every route that changes the agent answers 403 with a code the page
// can read, and leaves the disk as it was.
func TestReadOnlyConsoleRefusesEveryWrite(t *testing.T) {
	router, dir := consoleRouter(t, map[string]interface{}{"read_only": true})
	base := "/api/" + testAdminKey
	probe := map[string]interface{}{"name": "p1", "type": "ctl_plain", "params": map[string]interface{}{"host": "a"}}
	output := map[string]interface{}{"type": "otlp", "params": map[string]interface{}{"endpoint": "https://x.example:4318"}}
	for _, w := range []struct {
		method, path string
		body         interface{}
	}{
		{"POST", "/config/settings", map[string]interface{}{"port": 9090}},
		{"PUT", "/config/governance", map[string]interface{}{"governance": map[string]interface{}{"criticality": "low"}}},
		{"POST", "/config/probes", probe},
		{"PUT", "/config/probes/p1", probe},
		{"DELETE", "/config/probes/p1", nil},
		{"POST", "/config/outputs", output},
		{"PUT", "/config/outputs/http", map[string]interface{}{"params": map[string]interface{}{"port": 9090}}},
		{"DELETE", "/config/outputs/otlp", nil},
		{"POST", "/debug/logs", map[string]interface{}{}},
		{"POST", "/admin/cache/clear", nil},
	} {
		code, resp := doJSON(t, router, w.method, base+w.path, w.body)
		if code != 403 {
			t.Errorf("%s %s: %d, want 403", w.method, w.path, code)
			continue
		}
		if resp["code"] != "console_read_only" || !strings.Contains(fmt.Sprint(resp["error"]), "read-only") {
			t.Errorf("%s %s: body %v does not say why", w.method, w.path, resp)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "probes.d")); len(entries) != 0 {
		t.Errorf("a refused write left %d file(s) in probes.d", len(entries))
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "strategies.d", "00-http.yaml"))
	if strings.Contains(string(raw), "9090") {
		t.Errorf("a refused write changed the http output:\n%s", raw)
	}
}

// Reading is what a read-only console is for.
func TestReadOnlyConsoleStillReads(t *testing.T) {
	router, _ := consoleRouter(t, map[string]interface{}{"read_only": true})
	base := "/api/" + testAdminKey
	for _, path := range []string{
		"/config/probes", "/config/outputs", "/config/settings", "/config/governance",
		"/catalog/probes", "/catalog/outputs", "/info/system", "/debug/logs",
	} {
		if code, resp := doJSON(t, router, "GET", base+path, nil); code != 200 {
			t.Errorf("GET %s: %d %v, want 200", path, code, resp)
		}
	}
	code, resp := doJSON(t, router, "GET", base+"/info/system", nil)
	console, _ := resp["console"].(map[string]interface{})
	if code != 200 || console["read_only"] != true || console["allow_exec"] != false || console["mode"] != "read-only" {
		t.Errorf("info/system must tell the page the mode, got %v", resp["console"])
	}
	// Checking a configuration without running a probe stays possible.
	if code, resp := doJSON(t, router, "POST", base+"/config/validate", map[string]interface{}{
		"probe": "ctl_plain", "config": map[string]interface{}{"host": "a"}, "validation": "schema",
	}); code != 200 {
		t.Errorf("schema validation: %d %v", code, resp)
	}
}

// A read-only console also keeps a caller from having it run a probe
// it described, which is how an exec path would otherwise be reached.
func TestReadOnlyConsoleDoesNotRunDescribedProbes(t *testing.T) {
	router, _ := consoleRouter(t, map[string]interface{}{"read_only": true})
	code, _ := doJSON(t, router, "POST", "/api/"+testAdminKey+"/config/test", map[string]interface{}{
		"probe": "exec", "config": map[string]interface{}{"command": "/bin/true"},
	})
	if code != 403 {
		t.Errorf("config/test of an exec probe: %d, want 403", code)
	}
}

func TestDefaultConsoleKeepsWriting(t *testing.T) {
	router, _ := consoleRouter(t, nil)
	base := "/api/" + testAdminKey
	for _, params := range []map[string]interface{}{
		{"host": "a"},
	} {
		if code, resp := doJSON(t, router, "POST", base+"/config/probes", map[string]interface{}{"name": "d1", "type": "ctl_plain", "params": params}); code != 201 {
			t.Errorf("create in the default mode: %d %v", code, resp)
		}
	}
	if code, resp := doJSON(t, router, "POST", base+"/config/probes", map[string]interface{}{
		"name": "e1", "type": "exec", "enabled": false, "params": map[string]interface{}{"command": "/bin/true"},
	}); code != 201 {
		t.Errorf("an exec probe in the default mode: %d %v", code, resp)
	}
	if code, _ := doJSON(t, router, "POST", base+"/admin/cache/clear", nil); code == 403 {
		t.Error("cache clear refused in the default mode")
	}
}

func TestNoExecConsoleRefusesPrograms(t *testing.T) {
	router, dir := consoleRouter(t, map[string]interface{}{"allow_exec": false})
	base := "/api/" + testAdminKey

	create := func(name, typ string, params map[string]interface{}) (int, map[string]interface{}) {
		return doJSON(t, router, "POST", base+"/config/probes", map[string]interface{}{"name": name, "type": typ, "params": params})
	}
	if code, resp := create("e1", "exec", map[string]interface{}{"command": "/bin/true"}); code != 403 || !strings.Contains(fmt.Sprint(resp["error"]), "allow_exec") {
		t.Errorf("exec probe: %d %v, want 403 naming allow_exec", code, resp)
	}
	if code, resp := create("t1", "ctl_tool", map[string]interface{}{"host": "a", "tool_path": "/tmp/evil"}); code != 403 {
		t.Errorf("program path: %d %v, want 403", code, resp)
	}
	if _, err := os.Stat(filepath.Join(dir, "probes.d", "50-e1.yaml")); err == nil {
		t.Error("the refused exec probe was written")
	}

	// Everything else is still writable, a program path left at its
	// default included.
	if code, resp := create("p1", "ctl_plain", map[string]interface{}{"host": "a"}); code != 201 {
		t.Errorf("plain probe: %d %v", code, resp)
	}
	if code, resp := create("t2", "ctl_tool", map[string]interface{}{"host": "a", "tool_path": "toolctl"}); code != 201 {
		t.Errorf("default program path: %d %v", code, resp)
	}
	if code, resp := doJSON(t, router, "PUT", base+"/config/probes/p1", map[string]interface{}{"type": "ctl_plain", "params": map[string]interface{}{"host": "b"}}); code != 200 {
		t.Errorf("plain update: %d %v", code, resp)
	}
	if code, _ := doJSON(t, router, "DELETE", base+"/config/probes/p1", nil); code != 200 {
		t.Errorf("delete: %d, want 200", code)
	}
	if code, _ := doJSON(t, router, "POST", base+"/admin/cache/clear", nil); code == 403 {
		t.Error("cache clear refused by a console that only forbids programs")
	}

	// A path the file set stays editable around: the operator can change
	// the rest of the probe, not the program.
	if _, err := configuration.CreateProbeFragment(filepath.Join(dir, "agent.yaml"), configuration.ProbeConfig{
		Name: "iac", Type: "ctl_tool", Params: map[string]interface{}{"host": "a", "tool_path": "/opt/tool"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if code, resp := doJSON(t, router, "PUT", base+"/config/probes/iac", map[string]interface{}{"type": "ctl_tool", "params": map[string]interface{}{"host": "b", "tool_path": "/opt/tool"}}); code != 200 {
		t.Errorf("update leaving the stored path alone: %d %v", code, resp)
	}
	if code, _ := doJSON(t, router, "PUT", base+"/config/probes/iac", map[string]interface{}{"type": "ctl_tool", "params": map[string]interface{}{"host": "b", "tool_path": "/opt/other"}}); code != 403 {
		t.Errorf("update changing the stored path: %d, want 403", code)
	}
}

func TestNoExecConsoleDoesNotRunDescribedPrograms(t *testing.T) {
	router, _ := consoleRouter(t, map[string]interface{}{"allow_exec": false})
	url := "/api/" + testAdminKey + "/config/"
	exec := map[string]interface{}{"probe": "exec", "config": map[string]interface{}{"command": "/bin/true"}}
	if code, _ := doJSON(t, router, "POST", url+"test", exec); code != 403 {
		t.Errorf("config/test of exec: %d, want 403", code)
	}
	exec["validation"] = "connectivity"
	if code, _ := doJSON(t, router, "POST", url+"validate", exec); code != 403 {
		t.Errorf("connectivity validation of exec: %d, want 403", code)
	}
	exec["validation"] = "schema"
	if code, _ := doJSON(t, router, "POST", url+"validate", exec); code != 200 {
		t.Errorf("schema validation of exec: %d, want 200", code)
	}
	tool := map[string]interface{}{"probe": "ctl_tool", "validation": "full", "config": map[string]interface{}{"tool_path": "/tmp/evil"}}
	if code, _ := doJSON(t, router, "POST", url+"test", tool); code != 403 {
		t.Errorf("config/test with a program path: %d, want 403", code)
	}
}

// The switch lives in the file. An administration key can neither set it
// nor lose it by rewriting the output around it.
func TestTheConsoleBlockCannotBeChangedThroughTheAPI(t *testing.T) {
	router, dir := consoleRouter(t, map[string]interface{}{"allow_exec": false})
	base := "/api/" + testAdminKey
	file := filepath.Join(dir, "strategies.d", "00-http.yaml")
	read := func() string { raw, _ := os.ReadFile(file); return string(raw) }
	endpoints := []interface{}{"web", "prtg"}

	code, resp := doJSON(t, router, "PUT", base+"/config/outputs/http", map[string]interface{}{
		"params": map[string]interface{}{"port": 8080, "endpoints": endpoints, "console": map[string]interface{}{"allow_exec": true}},
	})
	if code != 403 || resp["code"] != "console_policy_file_only" {
		t.Fatalf("widening the block: %d %v, want 403 console_policy_file_only", code, resp)
	}
	if !strings.Contains(read(), "allow_exec: false") {
		t.Fatalf("the refused write changed the file:\n%s", read())
	}

	// The page sends back the block it was shown, or none at all.
	for name, console := range map[string]interface{}{"echoed": map[string]interface{}{"allow_exec": false}, "omitted": nil} {
		params := map[string]interface{}{"port": 8081, "endpoints": endpoints}
		if console != nil {
			params["console"] = console
		}
		if code, resp := doJSON(t, router, "PUT", base+"/config/outputs/http", map[string]interface{}{"params": params}); code != 200 {
			t.Fatalf("%s: %d %v", name, code, resp)
		}
		if got := read(); !strings.Contains(got, "allow_exec: false") || !strings.Contains(got, "8081") {
			t.Errorf("%s: the edit must land and the block survive:\n%s", name, got)
		}
	}
}

func TestRemovingTheConsoleBlockRestoresTheDefaults(t *testing.T) {
	cm := NewConfigurationManager(nil, map[string]interface{}{
		"console": map[string]interface{}{"read_only": true},
	}, testModuleLogger())
	if !cm.Console().ReadOnly {
		t.Fatal("the block was not read")
	}
	if err := cm.UpdateConfiguration(map[string]interface{}{"port": 8080}); err != nil {
		t.Fatal(err)
	}
	if got := cm.Console(); got != DefaultConsolePolicy {
		t.Errorf("after the block left the file: %+v, want the defaults", got)
	}
}

func TestAMalformedConsoleBlockFailsClosed(t *testing.T) {
	cm := NewConfigurationManager(nil, map[string]interface{}{
		"console": map[string]interface{}{"read_only": "true"},
	}, testModuleLogger())
	if !cm.Console().ReadOnly {
		t.Error("an unreadable block must leave the console read-only")
	}
	if err := ValidateParams(map[string]interface{}{"console": map[string]interface{}{"read_only": "true"}}); err == nil {
		t.Error("config check must reject a non-boolean read_only")
	}
}

func testModuleLogger() *logger.ModuleLogger {
	return logger.NewModuleLogger(logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}), "test.console")
}

// The page hides what a read-only console would refuse by the data-write
// mark, so a control that writes must carry it and the rule must exist.
func TestConsoleAssetsHideWriteControlsWhenReadOnly(t *testing.T) {
	css, err := os.ReadFile("assets/css/console.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), `[data-console="read-only"] [data-write]`) {
		t.Error("console.css does not hide [data-write] in a read-only console")
	}
	for page, ids := range map[string][]string{
		"probes":        {`id="btn-save" data-write`, `id="btn-add" data-write`},
		"output-editor": {`id="btn-save" data-write`, `id="btn-delete" data-write`, `id="btn-toggle" data-write`},
		"output-http":   {`id="h-save" data-write`},
		"settings":      {`id="save-lic" data-write`, `id="save-conn" data-write`, `id="save-gov" data-write`},
	} {
		raw, err := os.ReadFile("assets/html/" + page + ".html")
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			if !strings.Contains(string(raw), id) {
				t.Errorf("%s.html: %s is not marked data-write", page, id)
			}
		}
	}
}
