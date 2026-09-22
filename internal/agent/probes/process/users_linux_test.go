//go:build linux

package process

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// utmpRecord builds one record of the accounting file: the type at the
// front, the account name at its fixed offset, the rest left as the
// login program would leave it.
func utmpRecord(recordType uint16, user string) []byte {
	rec := make([]byte, utmpRecordSize)
	binary.NativeEndian.PutUint16(rec[0:2], recordType)
	copy(rec[utmpUserOffset:utmpUserOffset+utmpUserLen], user)
	return rec
}

func TestOnlyOpenSessionsAreCounted(t *testing.T) {
	var file []byte
	file = append(file, utmpRecord(2, "reboot")...) // BOOT_TIME
	file = append(file, utmpRecord(utmpUserProcess, "ana")...)
	file = append(file, utmpRecord(utmpUserProcess, "ana")...) // a second terminal
	file = append(file, utmpRecord(8, "sam")...)               // DEAD_PROCESS
	file = append(file, utmpRecord(5, "")...)                  // LOGIN_PROCESS
	file = append(file, utmpRecord(utmpUserProcess, "sam")...)

	if got := countUtmpSessions(file); got != 3 {
		t.Errorf("counted %v sessions, want 3: two terminals for one account count two, and a closed session counts none", got)
	}
}

// A record left behind by a logout keeps its slot and sometimes its
// type; without a name it is not a session.
func TestARecordWithNoAccountIsNotASession(t *testing.T) {
	if got := countUtmpSessions(utmpRecord(utmpUserProcess, "")); got != 0 {
		t.Errorf("counted %v, want 0", got)
	}
}

// The counter stops at the last whole record rather than reading past
// the end of what it was given.
func TestAPartialRecordIsNotReadPastTheEnd(t *testing.T) {
	file := append(utmpRecord(utmpUserProcess, "ana"), make([]byte, 40)...)
	if got := countUtmpSessions(file); got != 1 {
		t.Errorf("counted %v, want 1", got)
	}
	if got := countUtmpSessions(nil); got != 0 {
		t.Errorf("an empty file counted %v", got)
	}
}

// The record grew by sixteen bytes on the architectures that had no
// 32-bit predecessor, and reading one layout as the other yields a
// number that looks plausible and is wrong. A file that is not a whole
// number of records is the signal, and it must produce no value rather
// than a false one.
func TestAFileOfTheWrongLayoutYieldsNoValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "utmp")
	if err := os.WriteFile(path, make([]byte, utmpRecordSize+7), 0o600); err != nil {
		t.Fatal(err)
	}
	restore := utmpPaths
	utmpPaths = []string{path}
	t.Cleanup(func() { utmpPaths = restore })

	if got, err := loggedInSessions(); err == nil {
		t.Errorf("counted %v sessions from a file this build cannot read", got)
	}
}
