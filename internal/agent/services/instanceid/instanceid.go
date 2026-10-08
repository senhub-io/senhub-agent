// Package instanceid lets the agents running on one host recognise each
// other. Each agent writes its own service.instance.id to
// <state dir>/instance.id; the host process discovery of another agent reads
// that file to reuse the id for the agent process it sees listening, instead
// of fabricating a second service.instance for it.
//
// The file is the only channel: an agent's id is derived from its key, and the
// key itself never leaves the configuration.
package instanceid

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v3/process"
)

// ServiceName is the service.name every agent carries on its service.instance.
const ServiceName = "senhub-agent"

// ResolveForPID returns the instance id of the agent running as pid, when that
// process is a SenHub agent other than the caller (ownID) and has published
// one. ok=false for any other process and whenever the id cannot be read: the
// caller then keeps its usual behaviour.
func ResolveForPID(pid int32, name, ownID string) (id string, ok bool) {
	if pid <= 0 {
		return "", false
	}
	p, err := process.NewProcess(pid)
	if err != nil {
		return "", false
	}
	exe, _ := p.Exe()
	if !IsAgentProcess(name, exe) {
		return "", false
	}
	cmdline, _ := p.CmdlineSlice()
	id, err = ReadForProcess(cmdline, ownID)
	if err != nil {
		return "", false
	}
	return id, true
}

// FileName is the file, inside the state directory, holding the instance id.
const FileName = "instance.id"

const (
	// modeShared lets other accounts read the id. Safe only when the agent key
	// is a random UUID: the id is a one-way derivation of the key because of
	// the key's entropy, nothing else.
	modeShared os.FileMode = 0o644
	// modePrivate keeps the id readable by the owner alone, for a key a reader
	// could guess offline from the derived id.
	modePrivate os.FileMode = 0o600
)

var (
	// randomUUID matches a version 4 (random) UUID, the form the agent
	// generates its key in.
	randomUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	// anyUUID matches the shape of an id (any version): the derived id is a
	// UUID v5.
	anyUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// IsRandomKey reports whether the agent key is a random UUID, the only kind of
// key whose derived id may be published to other accounts.
func IsRandomKey(agentKey string) bool {
	return randomUUID.MatchString(agentKey)
}

// OwnStateDir is the state directory of this process: SENHUB_STATE_DIR when
// the operator set it, else the first directory systemd hands the unit, else
// the platform default. STATE_DIRECTORY is a list in the platform's path-list
// form, so it is split with filepath.SplitList: a ':' split cut "C:\..." in
// two on Windows.
func OwnStateDir() string {
	if v := os.Getenv("SENHUB_STATE_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("STATE_DIRECTORY"); v != "" {
		if dirs := filepath.SplitList(v); len(dirs) > 0 && dirs[0] != "" {
			return dirs[0]
		}
	}
	return DefaultStateDir()
}

// DefaultStateDir is the platform's state directory for an agent installed the
// usual way.
func DefaultStateDir() string {
	switch runtime.GOOS {
	case "windows":
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		return filepath.Join(programData, "SenHub")
	case "darwin":
		return "/usr/local/var/lib/senhub-agent"
	default:
		return "/var/lib/senhub-agent"
	}
}

// Write publishes the agent's instance id in dir. The file is world readable
// only when the agent key is a random UUID; for any other key (legacy, short)
// it is readable by its owner alone, so no other account can use it to test
// guesses of the key. The write is atomic so a reader never sees a partial id.
func Write(dir, instanceID, agentKey string) error {
	if instanceID == "" {
		return errors.New("empty instance id")
	}
	mode := modePrivate
	if IsRandomKey(agentKey) {
		mode = modeShared
	}
	target := filepath.Join(dir, FileName)
	if current, err := os.ReadFile(target); err == nil && string(current) == instanceID+"\n" {
		if info, statErr := os.Stat(target); statErr == nil && info.Mode().Perm() == mode {
			return nil
		}
	}
	tmp, err := os.CreateTemp(dir, FileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temporary instance id file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.WriteString(instanceID + "\n"); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("writing instance id: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("closing instance id file: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		cleanup()
		return fmt.Errorf("setting mode %v on instance id file: %w", mode, err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		cleanup()
		return fmt.Errorf("publishing %s: %w", target, err)
	}
	return nil
}

// Read returns the instance id another agent published in dir. An unreadable
// file, or content that is not a UUID, is an error: the caller keeps its own
// behaviour rather than trusting it.
func Read(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(b))
	if !anyUUID.MatchString(id) {
		return "", fmt.Errorf("%s does not hold an instance id", filepath.Join(dir, FileName))
	}
	return id, nil
}

// IsAgentProcess reports whether a process is a SenHub agent, from its
// executable name (senhub-agent, senhub-agent.exe) or the base name of its
// executable path.
func IsAgentProcess(name, exe string) bool {
	return isAgentBinary(name) || isAgentBinary(filepath.Base(strings.ReplaceAll(exe, `\`, "/")))
}

func isAgentBinary(name string) bool {
	n := strings.ToLower(strings.TrimSuffix(strings.ToLower(name), ".exe"))
	return n == "senhub-agent"
}

// StateDirsFor lists where the agent started with cmdline may keep its state,
// most specific first: the directory of an explicit --config-path (a portable
// install keeps its state beside its configuration), then the platform default.
func StateDirsFor(cmdline []string) []string {
	def := DefaultStateDir()
	var dirs []string
	for i, a := range cmdline {
		var p string
		switch {
		case a == "--config-path" && i+1 < len(cmdline):
			p = cmdline[i+1]
		case strings.HasPrefix(a, "--config-path="):
			p = strings.TrimPrefix(a, "--config-path=")
		default:
			continue
		}
		if d := filepath.Dir(p); d != "" && d != "." && d != def {
			dirs = append(dirs, d)
		}
	}
	return append(dirs, def)
}

// ReadForProcess returns the instance id published by the agent process that
// runs with cmdline, or an error when none of its state directories holds one.
// The caller's own id is never an answer: two agents sharing the default
// directory would otherwise each read the file the other, or itself, wrote.
func ReadForProcess(cmdline []string, ownID string) (string, error) {
	last := errors.New("no state directory")
	for _, d := range StateDirsFor(cmdline) {
		id, err := Read(d)
		if err != nil {
			last = err
			continue
		}
		if id == ownID {
			last = fmt.Errorf("%s holds this agent's own id", d)
			continue
		}
		return id, nil
	}
	return "", last
}
