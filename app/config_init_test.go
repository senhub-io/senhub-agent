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
			"--otlp-protocol", "http",
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
		if opts.otlpProtocol != "http" {
			t.Errorf("otlpProtocol = %q", opts.otlpProtocol)
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

	t.Run("http port parsed", func(t *testing.T) {
		opts, err := parseInitConfigArgs([]string{"--http-port", "9080"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.httpPort != 9080 {
			t.Errorf("httpPort = %d, want 9080", opts.httpPort)
		}
	})

	// The MSI always passes --http-port, expanded to "" when the operator
	// set no HTTP_PORT property: empty must mean the default, not an error.
	t.Run("empty http port means default", func(t *testing.T) {
		opts, err := parseInitConfigArgs([]string{"--http-port", ""})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.httpPort != 0 {
			t.Errorf("httpPort = %d, want 0 (default)", opts.httpPort)
		}
	})

	t.Run("http port out of range or not a number rejected", func(t *testing.T) {
		for _, raw := range []string{"0", "65536", "-1", "http", "80 80"} {
			if _, err := parseInitConfigArgs([]string{"--http-port", raw}); err == nil {
				t.Errorf("--http-port %q must be rejected", raw)
			}
		}
	})

	t.Run("empty args ok", func(t *testing.T) {
		if _, err := parseInitConfigArgs(nil); err != nil {
			t.Errorf("empty args should parse: %v", err)
		}
	})

	// audit M4/m12: validate OTLP inputs at parse, before any file is written.
	t.Run("invalid protocol rejected at parse", func(t *testing.T) {
		if _, err := parseInitConfigArgs([]string{"--otlp-endpoint", "x:4317", "--otlp-protocol", "thrift"}); err == nil {
			t.Error("invalid --otlp-protocol must fail at parse, not after config generation")
		}
	})
	t.Run("protocol without endpoint rejected", func(t *testing.T) {
		if _, err := parseInitConfigArgs([]string{"--otlp-protocol", "http"}); err == nil {
			t.Error("--otlp-protocol without --otlp-endpoint must be rejected, not silently ignored")
		}
	})

	// audit M3: endpoint carrying YAML-breaking characters is rejected.
	t.Run("endpoint injection rejected", func(t *testing.T) {
		for _, ep := range []string{"collector:4317\n  insecure: true", "vm:4318 # comment", "{evil}", "a b:1"} {
			if _, err := parseInitConfigArgs([]string{"--otlp-endpoint", ep}); err == nil {
				t.Errorf("endpoint %q should be rejected", ep)
			}
		}
	})
	t.Run("valid endpoint+protocol pass", func(t *testing.T) {
		if _, err := parseInitConfigArgs([]string{"--otlp-endpoint", "vm.example.com:4318", "--otlp-protocol", "http"}); err != nil {
			t.Errorf("valid inputs should parse: %v", err)
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
