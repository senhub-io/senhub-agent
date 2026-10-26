package filetail

import (
	"path"
	"strings"
)

var protectHomePrefixes = []string{"/home", "/root", "/run/user"}

// hiddenByProtectHome returns the directory that the packaged systemd unit
// (ProtectHome=true) makes invisible to the agent and that p sits under.
// p may be a glob: only its leading directories matter.
func hiddenByProtectHome(p string) (string, bool) {
	if !strings.HasPrefix(p, "/") {
		return "", false
	}
	clean := path.Clean(p)
	for _, prefix := range protectHomePrefixes {
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return prefix, true
		}
	}
	return "", false
}
