//go:build linux

package configuration

import (
	"fmt"
	"os"
	"syscall"
)

// alignLicenseOwner gives a licence sidecar written by root the owner,
// group and (at most 0640) mode of agent.yaml. A package install keeps
// agent.yaml root:senhub 0640 so the service user reads it through its
// group; a sidecar left 0600 root would stop the service from loading
// the licence it was just given.
func alignLicenseOwner(sidecar, configPath string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	ci, err := os.Stat(configPath)
	if err != nil {
		return fmt.Errorf("reading the owner of %s: %w", configPath, err)
	}
	st, ok := ci.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("reading the owner of %s: no unix stat", configPath)
	}
	if err := os.Chown(sidecar, int(st.Uid), int(st.Gid)); err != nil {
		return fmt.Errorf("giving %s the owner of %s: %w", sidecar, configPath, err)
	}
	if err := os.Chmod(sidecar, licenseSidecarMode(ci.Mode())); err != nil {
		return fmt.Errorf("setting the mode of %s: %w", sidecar, err)
	}
	return nil
}
