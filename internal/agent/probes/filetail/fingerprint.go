package filetail

import (
	"hash/crc32"
	"io"
	"os"
)

// DefaultFingerprintLength is the number of leading bytes hashed to
// identify a file across restarts. 1000 matches the issue's suggested
// default and is enough to distinguish unrelated logs while staying
// cheap to read.
const DefaultFingerprintLength = 1000

// fingerprint returns a STABLE identity for the file: the CRC32 of its
// first n bytes, hex-encoded — but only once the file actually has n
// bytes. A file shorter than n (or empty/unreadable) yields "".
//
// Why the "must have n bytes" rule: the hash of the first `min(n, size)`
// bytes of a short file CHANGES every time the file grows, because more
// content falls inside the window. Using that unstable value for
// identity makes a restart mistake a grown small file for a brand-new
// one and re-emit its lines (duplication). Hashing only the first n
// bytes once they exist makes the fingerprint immutable for an
// append-only file. Callers treat "" as "no stable fingerprint yet" and
// fall back to an offset/size comparison (see resolveStartOffset).
//
// Purpose: detect truncation / replacement. logrotate's copytruncate
// resets a file to size 0 while keeping the same path; a size-based or
// daily rotation may create a brand-new file at the same path. In both
// cases the old byte offset is meaningless and must not be reused, or
// the tail seeks past EOF (silent data gap) or re-reads from a stale
// position (duplicates).
func fingerprint(path string, n int) string {
	if n <= 0 {
		n = DefaultFingerprintLength
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, n)
	read, _ := io.ReadFull(f, buf)
	if read < n {
		// File shorter than the fingerprint window: no stable identity
		// yet (its head hash would change as it grows).
		return ""
	}
	return crc32Hex(buf)
}

func crc32Hex(b []byte) string {
	sum := crc32.ChecksumIEEE(b)
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 8)
	for i := 0; i < 8; i++ {
		out[7-i] = hexdigits[sum&0xf]
		sum >>= 4
	}
	return string(out)
}

// resolveStartOffset decides where a tail should begin for file, given
// any stored bookmark entry, the current file's stable fingerprint and
// size, and the operator's from_beginning setting.
//
//	stored, both fingerprints stable & equal   -> resume at stored.Offset
//	stored, both fingerprints stable & differ  -> rotated/replaced: 0
//	stored, fingerprint not stable (small file):
//	    stored.Offset <= currentSize           -> same file grown: resume
//	    stored.Offset >  currentSize           -> truncated/replaced: 0
//	no stored & from_beginning                 -> 0 (read existing once)
//	no stored & !from_beginning                -> -1 (seek to EOF)
//
// A return of -1 tells the caller to tail from end-of-file (only new
// lines). Any value >= 0 is a concrete byte offset to seek to.
//
// The small-file fallback exists because a file below the fingerprint
// window has no immutable head hash; comparing the stored offset to the
// current size is the best available same-vs-replaced signal there. A
// tiny file replaced by an unrelated, larger tiny file while the agent
// was down is the one case this cannot distinguish — an inherent limit
// of sub-fingerprint-size files, matching mainstream log shippers.
func resolveStartOffset(stored bookmarkEntry, hasStored bool, currentFingerprint string, currentSize int64, fromBeginning bool) int64 {
	if hasStored {
		if stored.Fingerprint != "" && currentFingerprint != "" {
			if stored.Fingerprint == currentFingerprint {
				return stored.Offset
			}
			return 0
		}
		if stored.Offset <= currentSize {
			return stored.Offset
		}
		return 0
	}
	if fromBeginning {
		return 0
	}
	return -1
}
