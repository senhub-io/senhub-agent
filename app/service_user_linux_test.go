//go:build linux

package app

import "testing"

// TestInstallArtifactPaths pins the #280 review fix: a custom
// --config-path must NOT cause a recursive chown of its parent
// directory (--config-path /etc/agent.yaml used to mean
// "chown -R senhub /etc"); only the agent's canonical directories are
// walked.
func TestInstallArtifactPaths(t *testing.T) {
	const defDir = "/etc/senhub-agent"
	const logDir = "/var/log/senhub-agent"

	t.Run("canonical config dir is walked", func(t *testing.T) {
		rec, files := installArtifactPaths("/etc/senhub-agent/agent.yaml", defDir, logDir, false)
		if len(files) != 0 {
			t.Errorf("files = %v, want none (dir walk covers the config)", files)
		}
		if len(rec) != 2 || rec[0] != defDir || rec[1] != logDir {
			t.Errorf("recursive = %v, want [%s %s]", rec, defDir, logDir)
		}
	})

	t.Run("custom config path chowns the file only", func(t *testing.T) {
		rec, files := installArtifactPaths("/etc/agent.yaml", defDir, logDir, false)
		if len(files) != 1 || files[0] != "/etc/agent.yaml" {
			t.Errorf("files = %v, want only the config file", files)
		}
		for _, r := range rec {
			if r == "/etc" {
				t.Fatal("/etc must NEVER be a recursive chown root")
			}
		}
		if len(rec) != 1 || rec[0] != logDir {
			t.Errorf("recursive = %v, want only the log dir", rec)
		}
	})

	t.Run("https adds the certs dir next to a custom config", func(t *testing.T) {
		rec, _ := installArtifactPaths("/srv/agent/agent.yaml", defDir, logDir, true)
		want := "/srv/agent/certs"
		found := false
		for _, r := range rec {
			if r == want {
				found = true
			}
			if r == "/srv/agent" {
				t.Fatal("custom config parent must not be walked")
			}
		}
		if !found {
			t.Errorf("recursive = %v, want to include %s", rec, want)
		}
	})
}
