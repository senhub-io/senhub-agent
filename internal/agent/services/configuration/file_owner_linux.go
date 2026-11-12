//go:build linux

package configuration

import (
	"os"
	"syscall"
)

// existingOwner is the owner a rewrite by root keeps: that of the file being
// replaced. Aligning a rewritten agent.yaml to its directory instead turned a
// package install's root:senhub file into root:root under a root-owned
// /etc/senhub-agent, and the service could no longer read it. nil when not
// root or when the file is not a regular file: the directory alignment then
// applies as before.
func existingOwner(path string) *fileOwner {
	if os.Geteuid() != 0 {
		return nil
	}
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return nil
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	return &fileOwner{uid: int(st.Uid), gid: int(st.Gid)}
}
