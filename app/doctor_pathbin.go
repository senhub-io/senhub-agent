package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// binariesOnPath lists the senhub-agent executables the PATH holds, in PATH
// order, each file once.
func binariesOnPath() []string {
	name := "senhub-agent"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	var found []string
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		real := p
		if r, err := filepath.EvalSymlinks(p); err == nil {
			real = r
		}
		if seen[real] {
			continue
		}
		seen[real] = true
		found = append(found, p)
	}
	return found
}

// unitExecBinary is the program the unit's ExecStart= runs.
func unitExecBinary(unit string) string {
	line, _ := installedExecStart(unit)
	value := strings.TrimSpace(strings.TrimPrefix(line, "ExecStart="))
	value = strings.TrimLeft(value, "-@+!:")
	if f := strings.Fields(value); len(f) > 0 {
		return f[0]
	}
	return ""
}

// pathBinaryCheck flags another senhub-agent on the PATH whose version is not
// the running service's: `senhub-agent --version` then answers for a binary
// that is not the one monitoring the host. The service's binary is the one the
// running process was started from, else the unit's ExecStart=.
func pathBinaryCheck(d doctorDeps, unit string, unitErr error, st statusResult) *doctorCheck {
	ip := d.install
	if ip.pathBinaries == nil || ip.binVersion == nil {
		return nil
	}
	ref := ""
	if !st.notRunning && st.managerErr == nil && ip.mainPID != nil && ip.exeLink != nil {
		if pid, err := ip.mainPID(); err == nil && pid != "" && pid != "0" {
			if exe, err := ip.exeLink(pid); err == nil {
				ref = strings.TrimSuffix(exe, " (deleted)")
			}
		}
	}
	if ref == "" && unitErr == nil {
		ref = unitExecBinary(unit)
	}
	if ref == "" {
		return nil
	}
	refVersion := ip.binVersion(ref)
	if refVersion == "" {
		return nil
	}
	refReal := realPath(ref)
	var stale []string
	for _, p := range ip.pathBinaries() {
		if realPath(p) == refReal {
			continue
		}
		if v := ip.binVersion(p); v != "" && v != refVersion {
			stale = append(stale, fmt.Sprintf("%s is %s", p, v))
		}
	}
	if len(stale) == 0 {
		c := okCheck(sectionInstall, "install.path", "no other senhub-agent on the PATH reports a different version")
		return &c
	}
	c := warnCheck(sectionInstall, "install.path",
		fmt.Sprintf("the service runs %s (%s) but %s; `senhub-agent --version` from a shell answers for the wrong binary", ref, refVersion, strings.Join(stale, ", ")),
		fmt.Sprintf("replace the PATH copy with a link to the service binary (sudo ln -sf %s <path>), or remove it", ref))
	return &c
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
