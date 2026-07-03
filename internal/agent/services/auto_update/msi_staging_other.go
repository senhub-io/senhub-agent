//go:build !windows

package auto_update

import (
	"fmt"
	"os"
)

// secureStageBase creates the staging base with owner-only permissions. Off
// Windows the 0o700 mode is enforced by the kernel, so no extra hardening is
// needed (the MSI-managed path is Windows-only anyway; this exists so the shared
// staging code compiles and unit-tests everywhere).
func secureStageBase(baseDir string) error {
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return fmt.Errorf("creating staging base %s: %w", baseDir, err)
	}
	return nil
}
