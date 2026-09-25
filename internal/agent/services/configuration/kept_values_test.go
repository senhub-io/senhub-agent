package configuration

import "testing"

// A save from the console carries back only what the console showed. A
// literal it hid (a plain header under the secret headers map) must be
// kept, a plain field the operator cleared must not come back, and an
// explicit null still removes a stored value.
func TestAConsoleSaveKeepsWhatTheConsoleHid(t *testing.T) {
	existing := map[string]interface{}{
		"endpoint": "collector:4317",
		"timeout":  "30s",
		"headers": map[string]interface{}{
			"Authorization": "${secret:strategies.otlp.headers.Authorization}",
			"X-Tenant":      "acme",
		},
		"api_token": "literal-token",
	}
	incoming := map[string]interface{}{"endpoint": "collector:4317"}
	out := KeepStoredValues(existing, incoming, []string{"headers"})

	h, _ := out["headers"].(map[string]interface{})
	if h["X-Tenant"] != "acme" {
		t.Errorf("the hidden plain header was dropped: %v", out["headers"])
	}
	if h["Authorization"] != "${secret:strategies.otlp.headers.Authorization}" {
		t.Errorf("the stored reference was dropped: %v", out["headers"])
	}
	if out["api_token"] != "literal-token" {
		t.Errorf("a value under a secret-looking key was dropped: %v", out["api_token"])
	}
	if _, back := out["timeout"]; back {
		t.Error("a plain field the form left out came back")
	}

	removal := map[string]interface{}{"headers": map[string]interface{}{"X-Tenant": nil}}
	out = KeepStoredValues(existing, removal, []string{"headers"})
	DropNilValues(out)
	if h, _ := out["headers"].(map[string]interface{}); h != nil {
		if _, still := h["X-Tenant"]; still {
			t.Error("an explicit null did not remove the stored header")
		}
	}
}
