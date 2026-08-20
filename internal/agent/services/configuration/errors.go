package configuration

import "fmt"

// ParseError reports that a configuration file could not be decoded:
// YAML syntax damage, or a shape the loader cannot accept (a
// strategies.d/ fragment carrying more than one top-level key).
//
// It exists so callers can tell "this file is malformed" apart from
// "this file is missing" or "a ${...} reference could not be
// resolved" without matching on message text. The `config check` CLI
// branches on it to print the offending line with its surrounding
// context, which needs both the file that failed (Path) and the raw
// decoder error (Err) — the decoder reports the line number relative
// to its own input, so a wrapped message would defeat the extraction.
type ParseError struct {
	Path string
	Err  error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("parsing %s: %v", e.Path, e.Err)
}

func (e *ParseError) Unwrap() error { return e.Err }

// newParseError wraps a decoder failure for path. It returns nil when
// err is nil so call sites can stay a single line.
func newParseError(path string, err error) error {
	if err == nil {
		return nil
	}
	return &ParseError{Path: path, Err: err}
}
