package app

import (
	"fmt"
	"path/filepath"
	"strings"
)

// existingServiceConflict says why `install` must leave the machine as it
// is: a senhub-agent service is already registered, from another
// executable. The service name and the data folder are fixed, so a second
// install would take them over. An empty result lets install go on: no
// service, or a re-run from the executable the service already runs.
func existingServiceConflict(imagePath, thisExe string, msiManaged bool) string {
	trimmed := strings.TrimSpace(imagePath)
	if trimmed == "" {
		return ""
	}
	registered := trimmed
	if exe, _, ok := splitImagePath(trimmed); ok {
		registered = strings.Trim(exe, `"`)
	}
	if samePath(registered, thisExe) {
		return ""
	}
	if msiManaged {
		return fmt.Sprintf(`The SenHub Agent installed by the MSI is already registered on this machine (%s).
Nothing was changed.
Upgrade it with the newer MSI, installed over it:
    msiexec /i senhub-agent-<version>-amd64.msi
An agent from the ZIP cannot run beside it: both use the service name senhub-agent and the folder %%ProgramData%%\SenHub.`, registered)
	}
	return fmt.Sprintf(`A senhub-agent service is already registered on this machine, running %s.
Nothing was changed.
To upgrade that agent, stop it and replace its files, or run:
    "%s" update <version>
To move it here, run "%s" uninstall first; this deletes its configuration and its agent key.`, registered, registered, registered)
}

// samePath compares two executable paths as Windows does: case does not
// matter, and "." or ".." segments are resolved.
func samePath(a, b string) bool {
	clean := func(p string) string {
		return strings.ToLower(filepath.Clean(strings.ReplaceAll(p, `\`, "/")))
	}
	return clean(a) == clean(b)
}
