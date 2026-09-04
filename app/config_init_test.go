package app

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestParseInitConfigArgs_LicenseAndHTTPPort(t *testing.T) {
	opts, err := parseInitConfigArgs([]string{"--license", "j", "--http-port", "9080"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.license != "j" || opts.httpPort != 9080 {
		t.Errorf("opts = %+v", opts)
	}
}

func TestResolveLicenseInput(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "lic.jwt")
	if err := os.WriteFile(f, []byte("  tok-from-file \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveLicenseInput("inline", f); err != nil || got != "tok-from-file" {
		t.Errorf("file wins: got %q, %v", got, err)
	}
	if got, err := resolveLicenseInput("inline", ""); err != nil || got != "inline" {
		t.Errorf("no file: got %q, %v", got, err)
	}
	if _, err := resolveLicenseInput("inline", filepath.Join(dir, "missing.jwt")); err == nil {
		t.Error("a missing file must be an error, before anything is written")
	}
	empty := filepath.Join(dir, "empty.jwt")
	os.WriteFile(empty, []byte("\n"), 0o600)
	if got, _ := resolveLicenseInput("inline", empty); got != "inline" {
		t.Errorf("empty file falls back to inline: got %q", got)
	}
}

func TestFindLicenseInDir(t *testing.T) {
	dir := t.TempDir()
	if got, err := findLicenseInDir(dir); err != nil || got != "" {
		t.Errorf("empty folder: got %q, %v; want no file and no error (Free tier)", got, err)
	}
	one := filepath.Join(dir, "license-client1-Pro.jwt")
	os.WriteFile(one, []byte("tok"), 0o600)
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o600)
	os.MkdirAll(filepath.Join(dir, "sub.jwt"), 0o700)
	if got, err := findLicenseInDir(dir); err != nil || got != one {
		t.Errorf("single jwt: got %q, %v; want %q", got, err, one)
	}
	os.WriteFile(filepath.Join(dir, "other.jwt"), []byte("tok2"), 0o600)
	if _, err := findLicenseInDir(dir); err == nil {
		t.Error("two jwt files must be refused, not guessed")
	}
	named := filepath.Join(dir, "license.jwt")
	os.WriteFile(named, []byte("tok3"), 0o600)
	if got, err := findLicenseInDir(dir); err != nil || got != named {
		t.Errorf("license.jwt wins over siblings: got %q, %v", got, err)
	}
	if got, err := findLicenseInDir(filepath.Join(dir, "missing")); err != nil || got != "" {
		t.Errorf("a missing folder is the installer default and means no licence: got %q, %v", got, err)
	}
	if _, err := parseInitConfigArgs([]string{"--license-dir", dir}); err != nil {
		t.Errorf("--license-dir must parse: %v", err)
	}
}
