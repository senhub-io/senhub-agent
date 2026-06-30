//go:build linux

package configuration

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"senhub-agent.go/internal/agent/services/logger"
)

// reownAfterSeal aligns the ownership of everything under the config dir with
// the dir's own owner. It runs only when the seal executed as root (euid 0): an
// operator sealing via `sudo agent secret migrate` writes the store, the age
// key and the rewritten config files as root:root, which a non-root service
// user (User=senhub, #223) then cannot read at runtime. Matching the new files
// to the config dir's owner restores readability without needing to know the
// service user's name — on a hardened install the dir is senhub-owned, on a
// legacy root install it is root-owned and this is a no-op.
//
// Best-effort: a chown failure on a single entry is logged at debug and does
// not fail the seal (the verify already guaranteed the data is correct).
func reownAfterSeal(dir string, log *logger.ModuleLogger) {
	if os.Geteuid() != 0 {
		return // non-root seal already created files as the running user
	}
	di, err := os.Stat(dir)
	if err != nil {
		return
	}
	st, ok := di.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	uid, gid := int(st.Uid), int(st.Gid)
	_ = filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		// Lchown so a symlink in the config dir is not followed.
		if cerr := os.Lchown(p, uid, gid); cerr != nil && log != nil {
			log.Debug().Err(cerr).Str("path", p).Msg("Could not align ownership after seal")
		}
		return nil
	})
}
