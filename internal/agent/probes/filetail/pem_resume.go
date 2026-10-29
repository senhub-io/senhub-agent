package filetail

import (
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/nxadm/tail"
)

const pemResumeWindow = 64 << 10

var (
	pemResumeBeginRe = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)
	pemResumeEndRe   = regexp.MustCompile(`-----END [A-Z0-9 ]*PRIVATE KEY-----`)
)

// pemBlockOpenBefore reports whether the text before offset ends inside a
// private key block. The window may start in the middle of a line: a
// marker cut by its start no longer matches, and a whole marker in that
// line is one the record carrying it really held.
func pemBlockOpenBefore(path string, offset int64) (bool, error) {
	if offset <= 0 {
		return false, nil
	}

	f, err := tail.OpenFile(path) //nolint:staticcheck // the delete sharing on Windows keeps a rotation by rename possible while this reads
	if err != nil {
		return false, fmt.Errorf("filetail: reading %s before offset %d: %w", path, offset, err)
	}
	defer closeQuietly(f)

	start := offset - pemResumeWindow
	if start < 0 {
		start = 0
	}

	buf := make([]byte, int(offset-start))
	n, err := f.ReadAt(buf, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("filetail: reading %s before offset %d: %w", path, offset, err)
	}
	window := buf[:n]

	begins := pemResumeBeginRe.FindAllIndex(window, -1)
	if len(begins) == 0 {
		return false, nil
	}

	lastBegin := begins[len(begins)-1]
	if pemResumeEndRe.Match(window[lastBegin[1]:]) {
		return false, nil
	}

	return true, nil
}
