//go:build !linux

package configuration

import "os"

// licenseSidecarOwner is Linux-only: on Windows the folder ACL governs the
// sidecar, and darwin is a development target.
func licenseSidecarOwner(_ string) (*fileOwner, os.FileMode, error) { return nil, 0o600, nil }
