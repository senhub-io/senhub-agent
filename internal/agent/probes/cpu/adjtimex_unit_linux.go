//go:build linux

package cpu

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const agentUnitName = "senhub-agent.service"

// unitDirs are the systemd unit directories, highest precedence first: a
// file in /etc shadows the same name in the vendor directories.
var unitDirs = []string{
	"/etc/systemd/system",
	"/run/systemd/system",
	"/usr/lib/systemd/system",
	"/lib/systemd/system",
}

// agentUnitKillsOnAdjtimex reports whether the systemd unit this process
// runs under kills it on adjtimex. It answers false whenever it cannot tell
// (not under the agent's unit, unit unreadable), so the caller falls back to
// probing in a child.
func agentUnitKillsOnAdjtimex(cgroupPath string) bool {
	if !runsUnderUnit(cgroupPath, agentUnitName) {
		return false
	}
	text, ok := readUnitWithDropIns(agentUnitName, unitDirs)
	if !ok {
		return false
	}
	return unitKillsOnAdjtimex(text)
}

// runsUnderUnit reports whether a /proc/<pid>/cgroup file places the
// process in the named unit.
func runsUnderUnit(cgroupPath, unit string) bool {
	raw, err := os.ReadFile(cgroupPath) // #nosec G304 - a /proc cgroup file
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), "/"+unit) {
			return true
		}
	}
	return false
}

// readUnitWithDropIns returns the unit file followed by its drop-ins in the
// order systemd applies them: drop-ins sorted by file name, a name in a
// higher-precedence directory shadowing the same name further down.
func readUnitWithDropIns(unit string, dirs []string) (string, bool) {
	var main []byte
	for _, dir := range dirs {
		raw, err := os.ReadFile(filepath.Join(dir, unit)) // #nosec G304 - systemd unit directories
		if err == nil {
			main = raw
			break
		}
	}
	if main == nil {
		return "", false
	}
	dropIns := map[string]string{}
	for i := len(dirs) - 1; i >= 0; i-- {
		paths, _ := filepath.Glob(filepath.Join(dirs[i], unit+".d", "*.conf"))
		for _, p := range paths {
			dropIns[filepath.Base(p)] = p
		}
	}
	names := make([]string, 0, len(dropIns))
	for name := range dropIns {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.Write(main)
	for _, name := range names {
		raw, err := os.ReadFile(dropIns[name]) // #nosec G304 - systemd drop-in directories
		if err != nil {
			continue
		}
		b.WriteString("\n")
		b.Write(raw)
	}
	return b.String(), true
}

// unitKillsOnAdjtimex reads the [Service] directives of a unit (with its
// drop-ins appended) and reports whether its seccomp filter kills the
// process on adjtimex. ProtectClock denies the @clock group, which holds
// adjtimex; an allow-list SystemCallFilter without adjtimex or @clock denies
// it too. Either kills the process unless SystemCallErrorNumber turns the
// refusal into an error.
func unitKillsOnAdjtimex(text string) bool {
	var (
		errorNumber  string
		protectClock bool
		allowLists   int
		allowsClock  bool
		deniesClock  bool
	)
	inService := false
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inService = line == "[Service]"
			continue
		}
		if !inService {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "SystemCallErrorNumber":
			errorNumber = value
		case "ProtectClock":
			protectClock = parseUnitBool(value)
		case "SystemCallFilter":
			if value == "" {
				allowLists, allowsClock, deniesClock = 0, false, false
				continue
			}
			deny := strings.HasPrefix(value, "~")
			for _, name := range strings.Fields(strings.TrimPrefix(value, "~")) {
				if name != "adjtimex" && name != "@clock" {
					continue
				}
				if deny {
					deniesClock = true
				} else {
					allowsClock = true
				}
			}
			if !deny {
				allowLists++
			}
		}
	}
	if errorNumber != "" && errorNumber != "kill" {
		return false
	}
	return protectClock || deniesClock || (allowLists > 0 && !allowsClock)
}

func parseUnitBool(v string) bool {
	switch strings.ToLower(v) {
	case "1", "yes", "true", "on":
		return true
	}
	return false
}
