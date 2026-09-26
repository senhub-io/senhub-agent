package app

import (
	"bufio"
	"io"
	"strings"
)

// parseEnvironmentFile reads a systemd EnvironmentFile: KEY=VALUE per line,
// "#" and ";" start a comment, and a value may be wrapped in single or
// double quotes.
func parseEnvironmentFile(r io.Reader) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			continue
		}
		out[k] = unquote(strings.TrimSpace(v))
	}
	return out
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// parseSystemctlEnvironment reads the output of
// `systemctl show <unit> -p Environment -p EnvironmentFiles`: the inline
// assignments, and the files to read, in the order systemd applies them.
// A file marked ignore_errors=yes (EnvironmentFile=-/path) may be absent.
func parseSystemctlEnvironment(out string) (inline map[string]string, files []envFile) {
	inline = map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Environment="):
			for _, a := range unitArgs(strings.TrimPrefix(line, "Environment=")) {
				if k, v, ok := strings.Cut(a, "="); ok && k != "" {
					inline[k] = v
				}
			}
		case strings.HasPrefix(line, "EnvironmentFiles="):
			v := strings.TrimPrefix(line, "EnvironmentFiles=")
			path, opts, _ := strings.Cut(v, " (")
			if path = strings.TrimSpace(path); path != "" {
				files = append(files, envFile{Path: path, Optional: strings.Contains(opts, "ignore_errors=yes")})
			}
		}
	}
	return inline, files
}

type envFile struct {
	Path     string
	Optional bool
}
