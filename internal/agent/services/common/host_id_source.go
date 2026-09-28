package common

import (
	"os"
	"strings"
)

// HostIDFromConfiguration reports whether id was set by the deployment
// through SENHUB_HOST_ID rather than derived from the machine. The
// container entrypoint writes that value where the OS keeps its machine
// id, so from here on it is indistinguishable from a derived one; saying
// where it came from is what makes two hosts sharing a copied value
// diagnosable instead of a mystery.
func HostIDFromConfiguration(id string) bool {
	configured := normalizeHostID(os.Getenv("SENHUB_HOST_ID"))
	return configured != "" && configured == normalizeHostID(id)
}

// DegenerateHostID reports an identity no machine derives by chance: fewer
// than four distinct hexadecimal digits (all zeros, all f's), or a run of
// at least sixteen ascending steps, which is what an example value such as
// 01234567-89ab-cdef-0123-456789abcdef looks like. Every host carrying
// one merges with every other host carrying it, silently. It is judged on
// its shape rather than against a list, since the next example value
// will not be on any list.
func DegenerateHostID(id string) bool {
	h := normalizeHostID(id)
	if len(h) != 32 {
		return false
	}
	distinct := map[rune]bool{}
	ascending := 0
	for i, r := range h {
		distinct[r] = true
		if i > 0 && hexValue(r) == (hexValue(rune(h[i-1]))+1)%16 {
			ascending++
		}
	}
	return len(distinct) < 4 || ascending >= 16
}

func normalizeHostID(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "-", ""))
}

func hexValue(r rune) int {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0')
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10
	}
	return -100
}
