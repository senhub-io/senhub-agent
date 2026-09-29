//go:build linux

package process

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The kernel's ceilings on what this probe counts. It reports the open
// file descriptors of each watched process and the number of processes
// on the machine; without the limits those numbers say how busy the
// machine is and not how close to its edge it runs.
//
// The native Zabbix agent reports the same two as kernel.maxfiles and
// kernel.maxproc.
const (
	fileMaxPath = "/proc/sys/fs/file-max"
	pidMaxPath  = "/proc/sys/kernel/pid_max"
)

func kernelLimits() (maxFiles, maxProcesses float64, err error) {
	if maxFiles, err = readKernelNumber(fileMaxPath); err != nil {
		return 0, 0, err
	}
	if maxProcesses, err = readKernelNumber(pidMaxPath); err != nil {
		return 0, 0, err
	}
	return maxFiles, maxProcesses, nil
}

func readKernelNumber(path string) (float64, error) {
	raw, err := os.ReadFile(path) // #nosec G304 - a kernel path, not operator input
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", path, err)
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", path, err)
	}
	return v, nil
}
