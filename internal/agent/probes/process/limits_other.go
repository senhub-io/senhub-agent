//go:build !linux

package process

import "errors"

// kernelLimits has no source outside Linux: the two values live in
// /proc/sys, which Darwin and Windows do not have. The caller logs at
// debug and carries on.
func kernelLimits() (maxFiles, maxProcesses float64, err error) {
	return 0, 0, errors.New("kernel limits are read from /proc/sys, which this OS does not have")
}
