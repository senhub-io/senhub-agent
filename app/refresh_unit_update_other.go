//go:build !linux

package app

// refreshUnitAfterUpdate has nothing to do outside Linux: there is no
// systemd unit.
func refreshUnitAfterUpdate(string) (string, error) { return "", nil }
