package fsown

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"
)

func TestRefusedNamesWhatAShareAnswers(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{&fs.PathError{Op: "chmod", Path: "/mnt/share/f", Err: syscall.EPERM}, true},
		{fmt.Errorf("wrapped: %w", &fs.PathError{Op: "chown", Path: "/mnt/share/f", Err: syscall.ENOTSUP}), true},
		{&fs.PathError{Op: "chmod", Path: "/f", Err: syscall.ENOENT}, false},
		{errors.New("disk full"), false},
	} {
		if got := Refused(tc.err); got != tc.want {
			t.Errorf("Refused(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
