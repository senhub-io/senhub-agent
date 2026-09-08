package http

import "testing"

func TestSanitizeForConsole(t *testing.T) {
	in := map[string]interface{}{
		"host": "db1", "username": "monitor", "password": "clear",
		"stored":  "${secret:probes/db/stored}",
		"tls":     map[string]interface{}{"enabled": true, "ca_file": "/ca.pem"},
		"headers": map[string]interface{}{"Authorization": "Bearer t", "X-Ref": "${env:TOK}", "X-Plain": "v"},
		"v3":      map[interface{}]interface{}{"username": "u", "auth_passphrase": "p"},
	}
	out := sanitizeForConsole(in, []string{"stored", "headers"})
	if out["host"] != "db1" || out["username"] != "monitor" {
		t.Error("identifiers must stay readable")
	}
	if out["password"] != "***" {
		t.Error("a password value must be hidden")
	}
	if out["stored"] != "${secret:probes/db/stored}" {
		t.Error("a reference must be shown as written")
	}
	if out["tls"].(map[string]interface{})["ca_file"] != "/ca.pem" {
		t.Error("a plain nested value must stay")
	}
	h := out["headers"].(map[string]interface{})
	if h["Authorization"] != "***" || h["X-Plain"] != "***" || h["X-Ref"] != "${env:TOK}" {
		t.Errorf("every value of a secret mapping is hidden except references: %v", h)
	}
	v3 := out["v3"].(map[string]interface{})
	if v3["username"] != "u" || v3["auth_passphrase"] != "***" {
		t.Errorf("yaml.v2 maps are converted and masked by key: %v", v3)
	}
	if in["password"] != "clear" {
		t.Error("the input must not be mutated")
	}
	if sanitizeForConsole(nil, nil) != nil {
		t.Error("nil stays nil")
	}
}

func TestDropRedactedValues(t *testing.T) {
	params := map[string]interface{}{
		"host": "h", "password": "***", "tls": map[string]interface{}{"ca_file": "[REDACTED]"},
		"headers": map[string]interface{}{"Authorization": "***", "X-Plain": "v"},
	}
	dropRedactedValues(params)
	if _, has := params["password"]; has {
		t.Error("a placeholder must be dropped")
	}
	if _, has := params["tls"]; has {
		t.Error("a block left empty must be dropped")
	}
	if h := params["headers"].(map[string]interface{}); len(h) != 1 || h["X-Plain"] != "v" {
		t.Errorf("only placeholders go, got %v", h)
	}
}
