//go:build linux

package process

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
)

// Open login sessions, read from the accounting file the login programs
// keep. One record per session, which is what `who` lists and what the
// native Zabbix agent counts as system.users.num — a machine where four
// terminals are open on the same account reports four.
//
// The record is glibc's struct utmp. Its size follows the architecture
// (see utmpRecordSize), while the two fields read here sit at the same
// place in both layouts, ahead of the tail that differs. The rest of
// the layout is therefore not mirrored.
//
// A machine whose libc is musl has no such file: musl's login
// accounting functions do nothing, so nothing writes it. The read
// fails, the caller logs at debug, and the metric is simply absent —
// which is the case inside our own container image.
const (
	// ut_type == USER_PROCESS marks a record as an open session; the
	// other values are boot times, run levels and dead sessions whose
	// records are kept in place.
	utmpUserProcess = 7
	// ut_user starts at offset 44 and is 32 bytes. A record whose name
	// is empty is not a session, whatever its type says.
	utmpUserOffset = 44
	utmpUserLen    = 32
)

var utmpPaths = []string{"/var/run/utmp", "/run/utmp"}

func loggedInSessions() (float64, error) {
	var raw []byte
	var err error
	for _, path := range utmpPaths {
		raw, err = os.ReadFile(path) // #nosec G304 - a fixed system path, not operator input
		if err != nil {
			continue
		}
		// Whole records are written at fixed slots, so a file that is
		// not a multiple of the record size is not the layout this
		// build expects. Counting it anyway would report a number that
		// looks plausible and is wrong; reporting nothing is better.
		if len(raw)%utmpRecordSize != 0 {
			return 0, fmt.Errorf("%s holds %d bytes, which is not a whole number of %d-byte records", path, len(raw), utmpRecordSize)
		}
		return countUtmpSessions(raw), nil
	}
	return 0, fmt.Errorf("reading the login accounting file: %w", err)
}

func countUtmpSessions(raw []byte) float64 {
	sessions := 0
	for off := 0; off+utmpRecordSize <= len(raw); off += utmpRecordSize {
		record := raw[off : off+utmpRecordSize]
		if binary.NativeEndian.Uint16(record[0:2]) != utmpUserProcess {
			continue
		}
		name := record[utmpUserOffset : utmpUserOffset+utmpUserLen]
		if i := bytes.IndexByte(name, 0); i >= 0 {
			name = name[:i]
		}
		if len(bytes.TrimSpace(name)) == 0 {
			continue
		}
		sessions++
	}
	return float64(sessions)
}
