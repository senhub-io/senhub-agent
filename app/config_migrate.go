// `agent config migrate` — convert a legacy monolithic configuration
// file into the 0.2.x+ multi-file layout (agent.yaml + probes.d/ +
// strategies.d/). Idempotent: a config already in the multi-file
// shape produces a "nothing to do" exit. The original file is
// preserved as a timestamped backup before any change.
//
// The conversion engine lives in package configuration
// (MigrateToMultiFile) so the boot-time seal can harmonise installs
// onto the same layout before sealing secrets. This file is just the
// CLI surface around it.
package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/configuration"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
)

// migrateConfig is the CLI wrapper around
// configuration.MigrateToMultiFile. It prints human-readable progress
// to stdout and exits non-zero on any error.
func migrateConfig(configPath string) {
	if configPath == "" {
		fmt.Fprintln(os.Stderr, "config migrate: no config path provided")
		os.Exit(2)
	}

	if abs, err := filepath.Abs(configPath); err == nil {
		configPath = abs
	}
	fmt.Printf("Migrating configuration: %s\n", configPath)

	result, err := configuration.MigrateToMultiFile(configPath, newCheckLogger("configuration.migrate"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "config migrate: %v\n", err)
		os.Exit(1)
	}

	if result.AlreadyMultiFile {
		fmt.Println("  [OK] file already in multi-file layout — nothing to do")
		return
	}

	fmt.Printf("  [OK] backup written: %s\n", result.BackupPath)
	fmt.Printf("  [OK] agent.yaml written (globals only)\n")
	if result.WroteProbes {
		fmt.Printf("  [OK] probes.d/00-host.yaml written\n")
	}
	if result.StrategyCount > 0 {
		fmt.Printf("  [OK] %d strategy file(s) written under strategies.d/\n", result.StrategyCount)
	}

	fmt.Println()
	fmt.Println("Migration complete. The agent will read the same configuration as before.")
	fmt.Printf("Backup of the monolithic source: %s\n", result.BackupPath)
	fmt.Println("Comments from the source file were NOT carried into the fragments —")
	fmt.Println("edit them back into the new files if you want them inline.")
}

// newCheckLogger builds the WARN-level module logger used by the
// migrate command to surface loader diagnostics (legacy detection,
// duplicate strategies) and any backup-restore warning.
func newCheckLogger(module string) *agentLogger.ModuleLogger {
	zlog := zerolog.New(os.Stderr).Level(zerolog.WarnLevel)
	base := (*agentLogger.Logger)(&zlog)
	return agentLogger.NewModuleLogger(base, module)
}
