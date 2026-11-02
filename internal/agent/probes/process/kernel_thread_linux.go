//go:build linux

package process

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// pfKthread is the Linux PF_KTHREAD flag in /proc/<pid>/stat.
const pfKthread = 0x00200000

// isKernelThread reports whether pid is a Linux kernel thread. The flags
// field is authoritative; when it cannot be read, kthreadd (pid 2) and its
// children (ppid 2) are the fallback.
func isKernelThread(pid int32, procRoot string) bool {
	if pid == 2 {
		return true
	}
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(int(pid)), "stat"))
	if err != nil {
		return false
	}
	fields := procStatFields(data)
	if len(fields) >= 7 {
		if flags, err := strconv.ParseUint(fields[6], 10, 64); err == nil {
			return flags&pfKthread != 0
		}
	}
	if len(fields) >= 2 {
		if ppid, err := strconv.ParseInt(fields[1], 10, 32); err == nil {
			return ppid == 2
		}
	}
	return false
}

// procStatFields returns the fields after the comm field. comm is in
// parentheses and may contain spaces and ')', so the split is after the
// LAST ')'; flags is then the 7th field (index 6) and ppid the 2nd.
func procStatFields(data []byte) []string {
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return nil
	}
	return strings.Fields(s[i+1:])
}
