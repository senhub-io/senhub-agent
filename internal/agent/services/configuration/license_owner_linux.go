//go:build linux

package configuration

import (
	"fmt"
	"os"
	"syscall"
)

// licenseSidecarOwner is the owner and mode a licence sidecar written by
// root takes: those of agent.yaml, the mode narrowed to at most 0640. A
// package install keeps agent.yaml root:senhub 0640 so the service user
// reads it through its group; a sidecar left 0600 root would stop the
// service from loading the licence it was just given. Not root: nil, the
// writer's own 0600 file.
func licenseSidecarOwner(configPath string) (*fileOwner, os.FileMode, error) {
	if os.Geteuid() != 0 {
		return nil, 0o600, nil
	}
	ci, err := os.Stat(configPath)
	if err != nil {
		return nil, 0, fmt.Errorf("reading the owner of %s: %w", configPath, err)
	}
	st, ok := ci.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, 0, fmt.Errorf("reading the owner of %s: no unix stat", configPath)
	}
	return &fileOwner{uid: int(st.Uid), gid: int(st.Gid)}, licenseSidecarMode(ci.Mode()), nil
}
