//go:build linux

package process

import (
	"fmt"
	"hash/crc32"
	"os"
	"strconv"
	"strings"
)

const (
	fileNrPath = "/proc/sys/fs/file-nr"
	passwdPath = "/etc/passwd"
)

// hostSecuritySignals reads the machine-wide facts a monitoring server
// uses to see a host change under it: the file handles the kernel has
// handed out, and the state of the account database. A fact that cannot
// be read is left out and logged by the caller; it never fails the cycle.
func hostSecuritySignals() (signals []hostSignal, errs []error) {
	if v, err := allocatedFileHandles(fileNrPath); err != nil {
		errs = append(errs, err)
	} else {
		signals = append(signals, hostSignal{"kernel_open_files", v})
	}
	checksum, modified, err := passwdState(passwdPath)
	if err != nil {
		errs = append(errs, err)
	} else {
		signals = append(signals,
			hostSignal{"passwd_checksum", checksum},
			hostSignal{"passwd_modified_time", modified},
		)
	}
	return signals, errs
}

// allocatedFileHandles returns the first field of file-nr: the number
// of file handles the kernel has allocated, of which the second field
// counts the unused ones. The third is file-max, already reported.
func allocatedFileHandles(path string) (float64, error) {
	raw, err := os.ReadFile(path) // #nosec G304 - a kernel path, not operator input
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", path, err)
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 1 {
		return 0, fmt.Errorf("reading %s: empty", path)
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", path, err)
	}
	return v, nil
}

// passwdState returns the CRC32 of the file's content and its
// modification time in Unix seconds. The checksum is a number, not a
// digest string, so that every output carries it as a gauge; 32 bits
// are exact in a float64 and are enough to see a file change, which is
// all it is for. It is not a cryptographic hash and says nothing about
// what changed.
func passwdState(path string) (checksum, modified float64, err error) {
	raw, err := os.ReadFile(path) // #nosec G304 - a fixed system path, not operator input
	if err != nil {
		return 0, 0, fmt.Errorf("reading %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, fmt.Errorf("reading %s: %w", path, err)
	}
	return float64(crc32.ChecksumIEEE(raw)), float64(info.ModTime().Unix()), nil
}
