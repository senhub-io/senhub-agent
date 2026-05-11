package otlp

import (
	"testing"
)

func TestExpandEnv_Substitution(t *testing.T) {
	t.Setenv("SENHUB_TEST_TOKEN", "supersecret")
	t.Setenv("SENHUB_TEST_HOST", "edge-01")

	cases := []struct {
		in, out string
	}{
		{"Bearer ${env:SENHUB_TEST_TOKEN}", "Bearer supersecret"},
		{"${env:SENHUB_TEST_HOST}.prod", "edge-01.prod"},
		{"${env:SENHUB_TEST_HOST}-${env:SENHUB_TEST_TOKEN}", "edge-01-supersecret"},
		// Unset variables expand to empty (matches OTel collector behavior).
		{"x=${env:SENHUB_TEST_UNSET}-y", "x=-y"},
		// No expansion when no placeholder.
		{"plain string", "plain string"},
		// Empty input is preserved.
		{"", ""},
		// $ alone is not a placeholder.
		{"$5.00", "$5.00"},
		// Other shell-style braces are not touched.
		{"${PLAIN}", "${PLAIN}"},
		{"${env:}", "${env:}"}, // empty name — does not match the identifier regex, left as-is
	}
	for _, c := range cases {
		got := expandEnv(c.in)
		if got != c.out {
			t.Errorf("expandEnv(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

func TestExpandEnvMap_Copies(t *testing.T) {
	t.Setenv("SENHUB_TEST_TOKEN", "abc")
	in := map[string]string{
		"Authorization": "Bearer ${env:SENHUB_TEST_TOKEN}",
		"X-Tenant":      "static",
	}
	out := expandEnvMap(in)

	if out["Authorization"] != "Bearer abc" {
		t.Errorf("Authorization=%q", out["Authorization"])
	}
	if out["X-Tenant"] != "static" {
		t.Errorf("X-Tenant=%q", out["X-Tenant"])
	}
	// Source map must not be mutated.
	if in["Authorization"] != "Bearer ${env:SENHUB_TEST_TOKEN}" {
		t.Errorf("source map mutated: %q", in["Authorization"])
	}
}

func TestParseConfig_ExpandsEnvInHeadersAndEndpointAndResource(t *testing.T) {
	t.Setenv("SENHUB_TEST_TOKEN", "ABCD")
	t.Setenv("SENHUB_TEST_HOST", "host-1")
	t.Setenv("SENHUB_TEST_ENV", "staging")

	cfg, err := ParseConfig(map[string]interface{}{
		"endpoint": "${env:SENHUB_TEST_HOST}:4317",
		"headers": map[string]interface{}{
			"Authorization": "Bearer ${env:SENHUB_TEST_TOKEN}",
		},
		"resource": map[string]interface{}{
			"deployment.environment": "${env:SENHUB_TEST_ENV}",
			"service.instance.id":    "${env:SENHUB_TEST_HOST}",
			"region":                 "${env:SENHUB_TEST_ENV}-fr1",
		},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if cfg.Endpoint != "host-1:4317" {
		t.Errorf("Endpoint=%q", cfg.Endpoint)
	}
	if cfg.Headers["Authorization"] != "Bearer ABCD" {
		t.Errorf("Header=%q", cfg.Headers["Authorization"])
	}
	if cfg.Resource.Environment != "staging" {
		t.Errorf("Environment=%q", cfg.Resource.Environment)
	}
	if cfg.Resource.ServiceInstance != "host-1" {
		t.Errorf("ServiceInstance=%q", cfg.Resource.ServiceInstance)
	}
	if cfg.Resource.Extra["region"] != "staging-fr1" {
		t.Errorf("Extra.region=%q", cfg.Resource.Extra["region"])
	}
}
