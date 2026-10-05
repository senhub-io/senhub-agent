//go:build !windows

package logicaldisk

import (
	"sort"
	"strconv"
	"strings"
)

// parseMountInfo reads the /proc/self/mountinfo format:
//
//	id parent maj:min root mountpoint opts [optional...] - fstype source superopts
func parseMountInfo(content string) []mountInfo {
	var mounts []mountInfo
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		sep := -1
		for i, f := range fields {
			if f == "-" {
				sep = i
				break
			}
		}
		if sep < 6 || len(fields) < sep+3 {
			continue
		}
		mounts = append(mounts, mountInfo{
			device:     unescapeMountField(fields[sep+2]),
			mountpoint: unescapeMountField(fields[4]),
			fstype:     fields[sep+1],
			majMin:     fields[2],
			root:       unescapeMountField(fields[3]),
		})
	}
	return mounts
}

// unescapeMountField decodes the octal escapes (\040 for a space, ...) the
// kernel applies to paths in mountinfo.
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// dedupeMountsByDevice keeps one mount per backing device. A service running
// under a hardened systemd unit sees bind mounts of host directories
// (ProtectSystem, ReadWritePaths, private /tmp, ...) that share the host
// filesystem's device but differ in root; reporting each one multiplies a
// single filesystem into several series. The kept mount is the one whose root
// is "/", otherwise the shortest mount point. Mounts without a device number
// are passed through untouched, and input order is preserved.
func dedupeMountsByDevice(mounts []mountInfo) []mountInfo {
	best := make(map[string]int)
	for i, m := range mounts {
		if m.majMin == "" {
			continue
		}
		j, seen := best[m.majMin]
		if !seen || betterMount(m, mounts[j]) {
			best[m.majMin] = i
		}
	}
	keep := make([]int, 0, len(mounts))
	for i, m := range mounts {
		if m.majMin == "" || best[m.majMin] == i {
			keep = append(keep, i)
		}
	}
	sort.Ints(keep)
	out := make([]mountInfo, 0, len(keep))
	for _, i := range keep {
		out = append(out, mounts[i])
	}
	return out
}

func betterMount(a, b mountInfo) bool {
	aRoot, bRoot := a.root == "/", b.root == "/"
	if aRoot != bRoot {
		return aRoot
	}
	return len(a.mountpoint) < len(b.mountpoint)
}
