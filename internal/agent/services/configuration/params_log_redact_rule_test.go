package configuration

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLogSanitizerFollowsTheSharedRule(t *testing.T) {
	in := map[string]interface{}{
		"pass":    "P1",
		"pwd":     "P2",
		"session": "P3",
		"private": "P4",
		"user":    "U1",
		"host":    "h.example",
		"headers": map[string]interface{}{
			"X-Session": "P5",
			"X-Plain":   "ok",
		},
	}
	out := SanitizeParamsForLog(in)
	for _, k := range []string{"pass", "pwd", "session", "private", "user"} {
		if out[k] != "***" {
			t.Errorf("key %q: got %v, want \"***\"", k, out[k])
		}
	}
	h := out["headers"].(map[string]interface{})
	if h["X-Session"] != "***" {
		t.Errorf("headers[X-Session]: got %v, want \"***\"", h["X-Session"])
	}
	if h["X-Plain"] != "ok" {
		t.Errorf("headers[X-Plain]: got %v, want \"ok\"", h["X-Plain"])
	}
	if out["host"] != "h.example" {
		t.Errorf("host: got %v, want \"h.example\"", out["host"])
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"P1", "P2", "P3", "P4", "P5", "U1"} {
		if strings.Contains(string(b), leak) {
			t.Errorf("marshaled log view contains %q: %s", leak, b)
		}
	}
}

func TestConsoleHidesFollowsTheSharedRule(t *testing.T) {
	for _, k := range []string{"pass", "pwd", "session", "private", "cookie", "X-Session", "api_key", "dsn"} {
		if !ConsoleHides(k) {
			t.Errorf("ConsoleHides(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"host", "port", "interval"} {
		if ConsoleHides(k) {
			t.Errorf("ConsoleHides(%q) = true, want false", k)
		}
	}
}
