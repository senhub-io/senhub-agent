package auto_update

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PackageOwner reports which package manager installed the binary at exe:
// "deb", "rpm", or "" when none did (archive, MSI or a build). Replacing a
// package-owned file behind dpkg or rpm leaves them recording a version that
// no longer runs and checksums that no longer match, so the operator update
// refuses it and points at apt or dnf instead.
func PackageOwner(exe string) string {
	return packageOwner(exe, "/var/lib/dpkg/info", rpmOwns)
}

func packageOwner(exe, dpkgInfoDir string, rpmQuery func(path string) bool) string {
	if exe == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	lists, _ := filepath.Glob(filepath.Join(dpkgInfoDir, "senhub-agent*.list"))
	for _, list := range lists {
		if listContains(list, exe) {
			return "deb"
		}
	}
	if rpmQuery != nil && rpmQuery(exe) {
		return "rpm"
	}
	return ""
}

func listContains(list, path string) bool {
	f, err := os.Open(list) // #nosec G304 - dpkg's own file list
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if sc.Text() == path {
			return true
		}
	}
	return false
}

// rpmOwns asks rpm whether a package owns path; no rpm binary means no.
func rpmOwns(path string) bool {
	if _, err := exec.LookPath("rpm"); err != nil {
		return false
	}
	return exec.Command("rpm", "-qf", "--quiet", path).Run() == nil // #nosec G204 - fixed binary, path is our own executable
}

// packageNameOf names the package that owns exe, "" when none does. dpkg is
// asked through its file lists and rpm through its database, as PackageOwner
// does.
func packageNameOf(exe string) string {
	return packageName(exe, "/var/lib/dpkg/info", rpmPackageOf)
}

func packageName(exe, dpkgInfoDir string, rpmQuery func(path string) string) string {
	if exe == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	lists, _ := filepath.Glob(filepath.Join(dpkgInfoDir, "senhub-agent*.list"))
	for _, list := range lists {
		if listContains(list, exe) {
			name := strings.TrimSuffix(filepath.Base(list), ".list")
			name, _, _ = strings.Cut(name, ":")
			return name
		}
	}
	if rpmQuery != nil {
		return rpmQuery(exe)
	}
	return ""
}

func rpmPackageOf(path string) string {
	if _, err := exec.LookPath("rpm"); err != nil {
		return ""
	}
	out, err := exec.Command("rpm", "-qf", "--queryformat", "%{NAME}", path).Output() // #nosec G204 - fixed arguments, path is our own executable
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
