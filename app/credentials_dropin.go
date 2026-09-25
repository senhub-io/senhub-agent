package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// syncDropIn makes the file at path hold body, or removes it when body is
// empty, and reports whether anything on disk changed. Rewriting an
// identical drop-in would still cost the caller a daemon-reload for nothing.
func syncDropIn(path, body string) (bool, error) {
	current, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}

	if strings.TrimSpace(body) == "" {
		if !exists {
			return false, nil
		}
		if err := os.Remove(path); err != nil {
			return false, fmt.Errorf("removing stale drop-in %s: %w", path, err)
		}
		return true, nil
	}

	if exists && bytes.Equal(current, []byte(body)) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("creating drop-in dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return false, fmt.Errorf("writing drop-in %s: %w", path, err)
	}
	return true, nil
}

// unitConfigPath returns the --config-path an installed ExecStart= line
// passes to the agent, or "" when it passes none. install renders every
// argument double-quoted, a hand-written unit usually does not.
func unitConfigPath(execStartLine string) string {
	_, args := splitExecStartLine(execStartLine)
	fields := unitArgs(args)
	for i, f := range fields {
		if f == "--config-path" && i+1 < len(fields) {
			return fields[i+1]
		}
		if v, ok := strings.CutPrefix(f, "--config-path="); ok {
			return v
		}
	}
	return ""
}

// unitArgs splits a systemd command line on spaces, honouring the double
// quotes and \" escapes kardianos/service writes.
func unitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote, started := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && inQuote && i+1 < len(s) && s[i+1] == '"':
			cur.WriteByte('"')
			i++
		case c == '"':
			inQuote = !inQuote
			started = true
		case c == ' ' && !inQuote:
			if started {
				out = append(out, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteByte(c)
			started = true
		}
	}
	if started {
		out = append(out, cur.String())
	}
	return out
}
