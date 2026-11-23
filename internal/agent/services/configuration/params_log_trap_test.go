package configuration

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeProbeParamsForLog_LiteralSecretKeyNames(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]interface{}
		leak   string
	}{
		{"pass", map[string]interface{}{"auth": map[string]interface{}{"pass": "TRAP-pass"}}, "TRAP-pass"},
		{"pwd", map[string]interface{}{"pwd": "LAtrap-pwd"}, "LAtrap-pwd"},
		{"session", map[string]interface{}{"session": "TRAP-session"}, "TRAP-session"},
		{"private", map[string]interface{}{"private": "TRAP-private"}, "TRAP-private"},
		{"cookie header", map[string]interface{}{"headers": map[string]interface{}{"Cookie": "sid=TRAP-cookie"}}, "TRAP-cookie"},
		{"x-session header", map[string]interface{}{"headers": map[string]interface{}{"X-Session": "TRAP-xsession"}}, "TRAP-xsession"},
		{"url userinfo", map[string]interface{}{"targets": []interface{}{"http://lab:TRAP-userinfo@host.example/p"}}, "TRAP-userinfo"},
		{"url query", map[string]interface{}{"endpoint": "https://host.example/p?x=TRAP-query"}, "TRAP-query"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := json.Marshal(SanitizeProbeParamsForLog("unknown_probe_type", c.params))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), c.leak) {
				t.Errorf("log view leaks %q: %s", c.leak, b)
			}
		})
	}
}
