package app

import (
	"strings"
	"testing"
)

// fakeEscape mimics the essence of Windows escaping for tests: quote
// only when the argument contains a space.
func fakeEscape(s string) string {
	if strings.Contains(s, " ") {
		return `"` + s + `"`
	}
	return s
}

func TestServiceCommandLine(t *testing.T) {
	tests := []struct {
		name string
		exe  string
		args []string
		want string
	}{
		{
			name: "no args",
			exe:  `C:\agent\senhub-agent.exe`,
			want: `C:\agent\senhub-agent.exe`,
		},
		{
			name: "args without spaces",
			exe:  `C:\agent\senhub-agent.exe`,
			args: []string{"run", "--config-path", `C:\agent\agent-config.yaml`},
			want: `C:\agent\senhub-agent.exe run --config-path C:\agent\agent-config.yaml`,
		},
		{
			name: "arg with space",
			exe:  `C:\agent\senhub-agent.exe`,
			args: []string{"--config-path", `C:\Program Files\agent\agent-config.yaml`},
			want: `C:\agent\senhub-agent.exe --config-path "C:\Program Files\agent\agent-config.yaml"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serviceCommandLine(tt.exe, tt.args, fakeEscape); got != tt.want {
				t.Errorf("serviceCommandLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServiceCommandLineMatches(t *testing.T) {
	exe := `C:\agent\senhub-agent.exe`
	args := []string{"run", "--config-path", `C:\agent\agent-config.yaml`}

	tests := []struct {
		name       string
		registered string
		want       bool
	}{
		{
			name:       "identical path",
			registered: serviceCommandLine(exe, args, fakeEscape),
			want:       true,
		},
		{
			name:       "different case",
			registered: serviceCommandLine(`c:\AGENT\SENHUB-AGENT.EXE`, args, fakeEscape),
			want:       true,
		},
		{
			name:       "different argument",
			registered: serviceCommandLine(exe, []string{"run", "--config-path", `C:\other\agent-config.yaml`}, fakeEscape),
			want:       false,
		},
		{
			name:       "no arguments registered",
			registered: exe,
			want:       false,
		},
		{
			name:       "malformed image path",
			registered: `"C:\agent\senhub-agent.exe`,
			want:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serviceCommandLineMatches(tt.registered, exe, args, fakeEscape); got != tt.want {
				t.Errorf("serviceCommandLineMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}
