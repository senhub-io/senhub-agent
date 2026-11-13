package app

import (
	"strings"
	"testing"
)

func TestParseUpdateWatchArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantErrPart string
		want        updateWatchArgs
	}{
		{
			name: "all flags present",
			args: []string{"--binary", `C:\agent\agent.exe`, "--version", "1.2.3", "--from", "1.2.2", "--state-dir", `C:\agent\state`},
			want: updateWatchArgs{binary: `C:\agent\agent.exe`, version: "1.2.3", from: "1.2.2", stateDir: `C:\agent\state`},
		},
		{
			name:        "empty version",
			args:        []string{"--binary", "b", "--version", "", "--from", "f", "--state-dir", "s"},
			wantErrPart: "--version",
			want:        updateWatchArgs{binary: "b", version: "", from: "f", stateDir: "s"},
		},
		{
			name:        "no args lists all four flags",
			args:        nil,
			wantErrPart: "missing required flags: --binary, --version, --from, --state-dir",
		},
		{
			name:        "unknown flag",
			args:        []string{"--nope"},
			wantErrPart: "parse update-watch args",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := parseUpdateWatchArgs(tt.args)
			if tt.wantErrPart == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErrPart, err)
				}
			}
			if tt.want != (updateWatchArgs{}) && a != tt.want {
				t.Fatalf("got %+v, want %+v", a, tt.want)
			}
			if tt.name == "empty version" && a.stateDir != "s" {
				t.Fatalf("stateDir not preserved on error: %+v", a)
			}
		})
	}
}
