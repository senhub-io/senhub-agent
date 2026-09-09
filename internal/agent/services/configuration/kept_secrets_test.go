package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeepStoredReferences(t *testing.T) {
	existing := map[string]interface{}{
		"host": "db1", "password": "${secret:mysql-prod.password}",
		"tls":     map[string]interface{}{"enabled": true, "ca_file": "${file:/ca.pem}"},
		"headers": map[string]interface{}{"Authorization": "${secret:strategies.otlp.headers.Authorization}", "X-Plain": "v"},
		"gone":    map[string]interface{}{"token": "${secret:x}"},
	}
	incoming := map[string]interface{}{
		"host": "db2", "tls": map[string]interface{}{"enabled": false},
		"headers": map[string]interface{}{"X-Other": "w"}, "gone": nil,
	}
	out := KeepStoredReferences(existing, incoming)
	if out["host"] != "db2" {
		t.Error("a value the form sets must win")
	}
	if out["password"] != "${secret:mysql-prod.password}" {
		t.Error("an omitted stored secret must be kept")
	}
	tls := out["tls"].(map[string]interface{})
	if tls["enabled"] != false || tls["ca_file"] != "${file:/ca.pem}" {
		t.Errorf("nested references must be kept under a block the form sets: %v", tls)
	}
	hdrs := out["headers"].(map[string]interface{})
	if hdrs["Authorization"] == nil || hdrs["X-Plain"] != nil || hdrs["X-Other"] != "w" {
		t.Errorf("only references are kept, plain values are the form's to send: %v", hdrs)
	}
	if _, has := out["gone"].(map[string]interface{}); has {
		t.Error("a block the form set to nil is not resurrected")
	}
	if got := KeepStoredReferences(map[string]interface{}{"password": "${secret:p}"}, nil); got["password"] != "${secret:p}" {
		t.Error("a nil incoming map must be created")
	}
}

func TestUpdateFragmentsKeepStoredSecrets(t *testing.T) {
	main := multiFileForFragments(t)
	if _, err := CreateProbeFragment(main, ProbeConfig{Name: "db", Type: "mysql", Params: map[string]interface{}{"host": "h", "username": "u", "password": "s3"}}, []string{"password"}); err != nil {
		t.Fatal(err)
	}
	params, err := ReadProbeFragmentParams(main, "db")
	if err != nil || params["password"] != "${secret:db.password}" {
		t.Fatalf("fragment params must carry the reference: %v %v", params, err)
	}
	if _, err := UpdateProbeFragment(main, ProbeConfig{Name: "db", Type: "mysql", Params: map[string]interface{}{"host": "h2", "username": "u"}}, []string{"password"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(ProbeFragmentPath(main, "db"))
	if !strings.Contains(string(raw), "${secret:db.password}") || !strings.Contains(string(raw), "host: h2") {
		t.Errorf("an update without the password must keep its reference:\n%s", raw)
	}
	if p, _ := ReadProbeFragmentParams(main, "nothing"); p != nil {
		t.Error("a missing fragment reads as nil")
	}

	dir := filepath.Join(filepath.Dir(main), "strategies.d")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateStrategyFragment(main, "otlp", map[string]interface{}{"endpoint": "c:4317", "headers": map[string]interface{}{"Authorization": "Bearer t"}}, true, []string{"headers"}); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateStrategyFragment(main, "otlp", map[string]interface{}{"endpoint": "c:4318"}, true, []string{"headers"}); err != nil {
		t.Fatal(err)
	}
	params, _ = StrategyFragmentParams(main, "otlp")
	hdrs, _ := params["headers"].(map[string]interface{})
	if params["endpoint"] != "c:4318" || hdrs["Authorization"] != "${secret:strategies.otlp.headers.Authorization}" {
		t.Errorf("an update without the headers must keep the stored token: %v", params)
	}
}

func TestDropNilValuesRemovesAStoredEntryOnRequest(t *testing.T) {
	existing := map[string]interface{}{"headers": map[string]interface{}{"Authorization": "${secret:a}", "X-Tenant": "${secret:b}"}}
	incoming := KeepStoredReferences(existing, map[string]interface{}{"headers": map[string]interface{}{"Authorization": nil}})
	DropNilValues(incoming)
	h, _ := incoming["headers"].(map[string]interface{})
	if _, kept := h["Authorization"]; kept || h["X-Tenant"] != "${secret:b}" {
		t.Errorf("null removes one entry and the other reference stays: %v", incoming)
	}
	only := map[string]interface{}{"headers": map[string]interface{}{"Authorization": nil}, "endpoint": "c:4317"}
	DropNilValues(only)
	if _, has := only["headers"]; has || only["endpoint"] != "c:4317" {
		t.Errorf("a mapping emptied by removals goes with it: %v", only)
	}
}

func TestSecretsInsideAListOfBlocks(t *testing.T) {
	main := multiFileForFragments(t)
	users := []interface{}{
		map[string]interface{}{"username": "u1", "auth_password": "p1"},
		map[string]interface{}{"username": "u2", "auth_password": "p2"},
	}
	p := ProbeConfig{Name: "traps", Type: "snmp_trap", Params: map[string]interface{}{"v3": map[string]interface{}{"users": users}}}
	if _, err := CreateProbeFragment(main, p, []string{"v3.users.auth_password"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(ProbeFragmentPath(main, "traps"))
	if strings.Contains(string(raw), "p1") || strings.Contains(string(raw), "p2") {
		t.Fatalf("passwords inside a list must be sealed:\n%s", raw)
	}
	if !strings.Contains(string(raw), "${secret:traps.v3.users.0.auth_password}") || !strings.Contains(string(raw), "${secret:traps.v3.users.1.auth_password}") {
		t.Errorf("each row gets its own store key:\n%s", raw)
	}
	// An update that re-sends the rows without their passwords keeps them.
	again := ProbeConfig{Name: "traps", Type: "snmp_trap", Params: map[string]interface{}{"v3": map[string]interface{}{"users": []interface{}{
		map[string]interface{}{"username": "u1"},
		map[string]interface{}{"username": "u2-renamed"},
	}}}}
	if _, err := UpdateProbeFragment(main, again, []string{"v3.users.auth_password"}); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(ProbeFragmentPath(main, "traps"))
	if !strings.Contains(string(raw), "${secret:traps.v3.users.0.auth_password}") || !strings.Contains(string(raw), "${secret:traps.v3.users.1.auth_password}") || !strings.Contains(string(raw), "u2-renamed") {
		t.Errorf("stored passwords of list rows must survive an update:\n%s", raw)
	}
}
