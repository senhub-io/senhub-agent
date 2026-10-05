package app

import (
	"fmt"
	"io/fs"
	"os"
	osuser "os/user"
	"path/filepath"
	"runtime"

	"github.com/kardianos/service"

	"senhub-agent.go/internal/agent/cliArgs"
)

// installState is what `install` finds on the machine before it touches
// anything. When all five hold, running install again has nothing to do.
type installState struct {
	serviceInstalled bool
	configPresent    bool
	binaryCurrent    bool
	unitCurrent      bool
	serviceEnabled   bool
}

func (s installState) alreadyDone() bool {
	return s.serviceInstalled && s.configPresent && s.binaryCurrent && s.unitCurrent && s.serviceEnabled
}

// installProbes are the host questions behind installState that need more
// than a file stat. A nil probe answers "yes, current".
type installProbes struct {
	binaryCurrent  func() bool
	unitCurrent    func() bool
	serviceEnabled func() bool
}

func probeOrTrue(f func() bool) bool { return f == nil || f() }

// serviceStatuser is the one method of the service manager the install
// check needs.
type serviceStatuser interface {
	Status() (service.Status, error)
}

// detectInstallState reads the machine without changing it. A service
// manager that cannot say (an error other than "installed and answering")
// counts as not installed, so install proceeds exactly as it did before
// this check existed.
func detectInstallState(svc serviceStatuser, configPath string, probes installProbes) installState {
	var state installState
	if _, err := svc.Status(); err == nil {
		state.serviceInstalled = true
	}
	if _, err := os.Stat(configPath); err == nil {
		state.configPresent = true
	}
	state.binaryCurrent = probeOrTrue(probes.binaryCurrent)
	if state.serviceInstalled {
		state.unitCurrent = probeOrTrue(probes.unitCurrent)
		state.serviceEnabled = probeOrTrue(probes.serviceEnabled)
	}
	return state
}

// configTreeSnapshot fingerprints the files a configuration is made of
// (the main file and the fragments under probes.d/ and strategies.d/) by
// size and modification time, so two snapshots can say whether anything
// was written in between.
func configTreeSnapshot(configPath string) map[string]string {
	snapshot := map[string]string{}
	record := func(path string, info fs.FileInfo) {
		snapshot[path] = fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
	}
	if info, err := os.Stat(configPath); err == nil {
		record(configPath, info)
	}
	base := filepath.Dir(configPath)
	for _, sub := range []string{"probes.d", "strategies.d"} {
		_ = filepath.WalkDir(filepath.Join(base, sub), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, infoErr := d.Info(); infoErr == nil {
				record(path, info)
			}
			return nil
		})
	}
	return snapshot
}

func sameSnapshot(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// installBinaryCurrent reports whether the machine already has what the
// install step would put there: on Linux, the service user and a system
// binary identical to the one running. Windows and macOS carry a single
// binary that is the one invoked, so there is nothing to compare.
func installBinaryCurrent(exe, serviceUser string) bool {
	if runtime.GOOS != "linux" {
		return true
	}
	if serviceUser != rootServiceUser {
		if _, err := osuser.Lookup(serviceUser); err != nil {
			return false
		}
	}
	dst := filepath.Join(systemBinaryDir, systemBinaryName)
	if exe == dst {
		return true
	}
	same, err := sameContents(exe, dst)
	return err == nil && same
}

// generateConfigurationReport runs the install-time configuration step and
// says whether it wrote anything. generate is idempotent by contract (it
// loads what is there and only creates what is missing); the snapshot
// turns that into a fact the caller can print and test.
func generateConfigurationReport(args *cliArgs.ParsedArgs, configPath string, generate func(*cliArgs.ParsedArgs) error) (changed bool, err error) {
	before := configTreeSnapshot(configPath)
	if err := generate(args); err != nil {
		return false, err
	}
	return !sameSnapshot(before, configTreeSnapshot(configPath)), nil
}
