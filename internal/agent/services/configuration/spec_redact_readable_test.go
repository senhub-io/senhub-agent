package configuration

import (
	"testing"

	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/data_store/outputspec"
)

func registerReadableHeadersOutput() {
	if _, ok := outputspec.For("readable_headers_test"); ok {
		return
	}
	outputspec.Register(outputspec.Output{
		Type: "readable_headers_test", DisplayName: "test", Mode: outputspec.ModePush,
		Params: []spec.ParamSpec{{Key: "headers", Kind: spec.KindMap, Secret: true, ReadableKeys: []string{"X-Scope-OrgID"}}},
	})
}

func TestSecretMapKeepsDeclaredReadableKeys(t *testing.T) {
	registerReadableHeadersOutput()
	in := map[string]interface{}{
		"endpoint": "collector:4317",
		"headers": map[string]interface{}{
			"Authorization": "Bearer TRAP-auth",
			"Cookie":        "sid=TRAP-cookie",
			"X-Session":     "TRAP-session",
			"X-Custom":      "TRAP-custom",
			"x-scope-orgid": "tenant-a",
		},
	}
	out := SanitizeOutputParamsForLog("readable_headers_test", in)
	h := out["headers"].(map[string]interface{})
	for _, k := range []string{"Authorization", "Cookie", "X-Session", "X-Custom"} {
		if h[k] != "***" {
			t.Errorf("headers[%s] = %v, want ***", k, h[k])
		}
	}
	if h["x-scope-orgid"] != "tenant-a" {
		t.Errorf("tenant header = %v, want it readable", h["x-scope-orgid"])
	}

	data := &LocalConfigurationData{Storage: []StorageConfig{{Name: "readable_headers_test", Params: map[string]interface{}{
		"headers": map[interface{}]interface{}{"Authorization": "Bearer TRAP-auth", "X-Scope-OrgID": "tenant-a"},
	}}}}
	maskSpecSecrets(data)
	hv := data.Storage[0].Params["headers"].(map[interface{}]interface{})
	if hv["Authorization"] != "***" || hv["X-Scope-OrgID"] != "tenant-a" {
		t.Errorf("effective view headers = %v", hv)
	}
}
