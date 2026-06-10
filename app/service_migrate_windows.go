//go:build windows

package app

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const serviceRegistryPath = `SYSTEM\CurrentControlSet\Services\senhub-agent`

// migrateLegacyServiceRegistration fixes a pre-0.2.x service ImagePath
// that lacks the `run` subcommand (#309). Best-effort: called from the
// service start/restart commands (which already require administrator),
// it logs what it did and never blocks the start on migration errors —
// a failed migration leaves the operator exactly where they were, with
// the sc.exe one-liner documented in the release notes.
func migrateLegacyServiceRegistration() {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, serviceRegistryPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		// Service not installed (or insufficient rights): nothing to do.
		return
	}
	defer key.Close()

	imagePath, _, err := key.GetStringValue("ImagePath")
	if err != nil {
		return
	}

	migrated, changed := migrateImagePathValue(imagePath)
	if !changed {
		return
	}

	if err := key.SetStringValue("ImagePath", migrated); err != nil {
		fmt.Printf("Warning: legacy service command line detected but migration failed: %v\n", err)
		fmt.Printf("Fix manually with:\n    sc.exe config senhub-agent binPath= \"%s\"\n", migrated)
		return
	}
	fmt.Println("Migrated legacy service command line (added the 'run' subcommand) — see issue #309")
}
