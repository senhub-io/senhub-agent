//go:build !windows

package auto_update

// startFailedStartWatcher is a no-op off Windows: the self-exit restart path
// only exists for the Windows service.
func (a *autoUpdate) startFailedStartWatcher(toVersion string) {}
