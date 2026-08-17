package app

import "testing"

func TestParseConfigPathArgs(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantPath string
		wantErr  bool
	}{
		{"no arguments", nil, "", false},
		{"positional path", []string{"/etc/senhub-agent/agent.yaml"}, "/etc/senhub-agent/agent.yaml", false},

		// The reported defect: --config-path was taken AS the path and resolved
		// against the binary's directory, producing
		// "/usr/local/bin/--config-path: no such file or directory".
		{"flag form", []string{"--config-path", "/etc/senhub-agent/agent.yaml"}, "/etc/senhub-agent/agent.yaml", false},
		{"flag form, single dash", []string{"-config-path", "/tmp/a.yaml"}, "/tmp/a.yaml", false},

		{"flag with no value", []string{"--config-path"}, "", true},
		{"unknown flag is refused, not treated as a path", []string{"--redact"}, "", true},
		{"two paths", []string{"/a.yaml", "/b.yaml"}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseConfigPathArgs(tc.args)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.wantPath {
				t.Errorf("path = %q, want %q", got, tc.wantPath)
			}
		})
	}
}

// An unknown option must say what IS accepted. A bare "unknown option" sends
// the reader to --help; naming both forms answers the question in place.
func TestParseConfigPathArgsExplainsWhatIsAccepted(t *testing.T) {
	_, err := parseConfigPathArgs([]string{"--nope"})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"positional", "--config-path"} {
		if !contains(err.Error(), want) {
			t.Errorf("error should mention %q; got %q", want, err)
		}
	}
}

func contains(h, n string) bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return true
		}
	}
	return false
}
