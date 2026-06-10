package app

import "testing"

// TestMigrateImagePathValue pins the #309 migration: a pre-0.2.x
// Windows service ImagePath lacking the `run` subcommand gets it
// inserted right after the executable; everything already carrying a
// subcommand (or unparseable) is left strictly untouched.
func TestMigrateImagePathValue(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		want        string
		wantChanged bool
	}{
		{
			name:        "legacy quoted with config-path (the bbcloud case)",
			in:          `"C:\SenHub\senhub-agent.exe" --config-path C:\SenHub\agent.yaml`,
			want:        `"C:\SenHub\senhub-agent.exe" run --config-path C:\SenHub\agent.yaml`,
			wantChanged: true,
		},
		{
			name:        "legacy unquoted",
			in:          `C:\SenHub\senhub-agent.exe --config-path C:\SenHub\agent.yaml`,
			want:        `C:\SenHub\senhub-agent.exe run --config-path C:\SenHub\agent.yaml`,
			wantChanged: true,
		},
		{
			name:        "legacy bare exe, no flags",
			in:          `"C:\SenHub\senhub-agent.exe"`,
			want:        `"C:\SenHub\senhub-agent.exe" run`,
			wantChanged: true,
		},
		{
			name:        "already migrated",
			in:          `"C:\SenHub\senhub-agent.exe" run --config-path C:\SenHub\agent.yaml`,
			wantChanged: false,
		},
		{
			name:        "quoted path with spaces, legacy",
			in:          `"C:\Program Files\SenHub\senhub-agent.exe" --verbose`,
			want:        `"C:\Program Files\SenHub\senhub-agent.exe" run --verbose`,
			wantChanged: true,
		},
		{
			name:        "explicit other subcommand untouched",
			in:          `"C:\SenHub\senhub-agent.exe" status`,
			wantChanged: false,
		},
		{
			name:        "malformed quote untouched",
			in:          `"C:\SenHub\senhub-agent.exe --config-path x`,
			wantChanged: false,
		},
		{
			name:        "empty untouched",
			in:          "",
			wantChanged: false,
		},
		{
			name:        "no exe token untouched",
			in:          `something-weird --flag`,
			wantChanged: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := migrateImagePathValue(c.in)
			if changed != c.wantChanged {
				t.Fatalf("changed = %v, want %v (got %q)", changed, c.wantChanged, got)
			}
			if changed && got != c.want {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
			if !changed && got != c.in {
				t.Errorf("unchanged input was rewritten: %q -> %q", c.in, got)
			}
		})
	}
}
