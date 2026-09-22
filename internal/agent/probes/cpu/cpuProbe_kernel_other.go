//go:build !windows && !linux

package cpu

import "errors"

// readKernelCounters has no source outside Linux: the counters it reads
// live in /proc/stat, which Darwin and the BSDs do not have. The caller
// logs at debug and carries on rather than treating it as a failure.
func readKernelCounters() (*kernelCounters, error) {
	return nil, errors.New("kernel counters are read from /proc/stat, which this OS does not have")
}
