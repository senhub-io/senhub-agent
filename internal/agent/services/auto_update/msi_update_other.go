//go:build !windows

package auto_update

import "fmt"

// isMsiManaged is always false off Windows: the MSI-managed update path is
// Windows-only, so every other platform keeps the binary self-replace flow.
func isMsiManaged() bool { return false }

// applyMsiUpdate is never reached off Windows (guarded by isMsiManaged); it
// exists only so the shared Update() branch compiles on all platforms.
func (a *autoUpdate) applyMsiUpdate(_, _ string) error {
	return fmt.Errorf("MSI-managed update is only available on Windows")
}
