package app

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// serviceEnvReport says what doctor learned of the environment the
// service runs with.
type serviceEnvReport struct {
	Applied    int
	Unreadable []string
}

type envFileRef struct {
	path     string
	optional bool
}

// readServiceEnvironment resolves the environment systemd gives the
// unit: Environment= and EnvironmentFile= (a leading '-' makes the file
// optional) of the unit and then of its drop-ins in lexical order, a
// later setting overriding an earlier one. EnvironmentFile values win
// over Environment=, as in systemd. Files that cannot be read are
// returned so the caller can say why a variable stays unresolved.
func readServiceEnvironment(unitPath string) (env map[string]string, unreadable []string, err error) {
	unit, err := os.ReadFile(unitPath) // #nosec G304 - the installed systemd unit
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", unitPath, err)
	}
	texts := []string{string(unit)}
	dropIns, _ := filepath.Glob(unitPath + ".d/*.conf")
	sort.Strings(dropIns)
	for _, p := range dropIns {
		raw, rerr := os.ReadFile(p) // #nosec G304 - a drop-in of the installed unit
		if rerr != nil {
			unreadable = append(unreadable, p)
			continue
		}
		texts = append(texts, string(raw))
	}

	env = map[string]string{}
	var files []envFileRef
	for _, text := range texts {
		for _, kv := range parseUnitEnvironment(text, &files) {
			env[kv[0]] = kv[1]
		}
	}
	for _, f := range files {
		fh, ferr := os.Open(f.path) // #nosec G304 - path named by the installed unit
		if ferr != nil {
			if !f.optional || !os.IsNotExist(ferr) {
				unreadable = append(unreadable, f.path)
			}
			continue
		}
		for k, v := range parseEnvironmentFile(fh) {
			env[k] = v
		}
		_ = fh.Close()
	}
	return env, unreadable, nil
}

// parseUnitEnvironment extracts the Environment= assignments of one
// unit or drop-in text and appends its EnvironmentFile= entries to
// files. An empty EnvironmentFile= resets the list, as in systemd.
func parseUnitEnvironment(text string, files *[]envFileRef) [][2]string {
	var assignments [][2]string
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "Environment="):
			for _, word := range splitUnitWords(strings.TrimPrefix(line, "Environment=")) {
				if k, v, ok := strings.Cut(word, "="); ok && k != "" {
					assignments = append(assignments, [2]string{k, v})
				}
			}
		case strings.HasPrefix(line, "EnvironmentFile="):
			value := strings.TrimSpace(strings.TrimPrefix(line, "EnvironmentFile="))
			if value == "" {
				*files = nil
				continue
			}
			ref := envFileRef{path: value}
			if strings.HasPrefix(value, "-") {
				ref = envFileRef{path: strings.TrimPrefix(value, "-"), optional: true}
			}
			*files = append(*files, ref)
		}
	}
	return assignments
}

// splitUnitWords splits an Environment= value on unquoted whitespace
// and strips the double quotes around a word or around its value.
func splitUnitWords(s string) []string {
	var words []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case (r == ' ' || r == '\t') && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return words
}

// applyServiceEnvironment exports the unit's environment into this
// process so the configuration resolves as it does under the service. A
// variable already exported in the shell wins, as in config check.
func applyServiceEnvironment(unitPath string) serviceEnvReport {
	env, unreadable, err := readServiceEnvironment(unitPath)
	if err != nil {
		return serviceEnvReport{Unreadable: []string{unitPath}}
	}
	rep := serviceEnvReport{Unreadable: unreadable}
	for k, v := range env {
		if _, set := os.LookupEnv(k); set {
			continue
		}
		if os.Setenv(k, v) == nil {
			rep.Applied++
		}
	}
	return rep
}
