//go:build !linux

package app

// serviceEnvironment has no service manager to read outside systemd.
func serviceEnvironment(_ string) (map[string]string, error) { return nil, nil }
