package app

import "testing"

// parseInitConfigArgs must fail closed on unattended install paths: an
// unknown flag (a typo'd --licence) or a value-taking flag with no value
// must be a hard error, never a silently-ignored token that leaves the host
// running Free-tier while reporting success.
func TestParseInitConfigArgs(t *testing.T) {
	t.Run("all known flags", func(t *testing.T) {
		opts, err := parseInitConfigArgs([]string{
			"--config-path", "/etc/senhub/agent.yaml",
			"--license", "jwt-token",
			"--tags", "env=prod,role=db",
			"--otlp-endpoint", "otlp:4317",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.configPath != "/etc/senhub/agent.yaml" {
			t.Errorf("configPath = %q", opts.configPath)
		}
		if opts.license != "jwt-token" {
			t.Errorf("license = %q", opts.license)
		}
		if opts.otlpEndpoint != "otlp:4317" {
			t.Errorf("otlpEndpoint = %q", opts.otlpEndpoint)
		}
		if opts.tags["env"] != "prod" || opts.tags["role"] != "db" {
			t.Errorf("tags = %v", opts.tags)
		}
	})

	t.Run("unknown flag rejected", func(t *testing.T) {
		if _, err := parseInitConfigArgs([]string{"--licence", "jwt"}); err == nil {
			t.Error("typo'd --licence must be rejected, not silently ignored")
		}
	})

	t.Run("dangling value flag rejected", func(t *testing.T) {
		if _, err := parseInitConfigArgs([]string{"--license"}); err == nil {
			t.Error("--license with no value must be rejected")
		}
	})

	t.Run("empty args ok", func(t *testing.T) {
		if _, err := parseInitConfigArgs(nil); err != nil {
			t.Errorf("empty args should parse: %v", err)
		}
	})
}

func TestParseTagList(t *testing.T) {
	cases := []struct {
		in   string
		want map[string]string
	}{
		{"k=v", map[string]string{"k": "v"}},
		{"a=1, b=2 ", map[string]string{"a": "1", "b": "2"}},
		{"noeq,x=y", map[string]string{"x": "y"}},
		{"", map[string]string{}},
		{"=novalue", map[string]string{}},
	}
	for _, tc := range cases {
		got := parseTagList(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("parseTagList(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("parseTagList(%q)[%q] = %q, want %q", tc.in, k, got[k], v)
			}
		}
	}
}
