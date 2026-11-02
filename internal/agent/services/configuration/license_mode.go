package configuration

import "os"

// licenseSidecarMode is the mode of agent.yaml narrowed to owner read-write
// and group read: the sidecar is never readable by others, nor writable by
// the group, whatever agent.yaml allows.
func licenseSidecarMode(config os.FileMode) os.FileMode {
	return config.Perm()&0o640 | 0o600
}
