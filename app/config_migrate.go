// `agent config migrate` — convert a legacy monolithic configuration
// file into the 0.2.x+ multi-file layout (agent.yaml + probes.d/ +
// strategies.d/). Idempotent: a config already in the multi-file
// shape produces a "nothing to do" exit. The original file is
// preserved as a timestamped backup before any change.
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/yaml.v2"

	"senhub-agent.go/internal/agent/services/configuration"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
)

// migrateConfig is the entry point invoked by `agent config migrate`.
//
// Steps:
//
//  1. Read configPath. Refuse to proceed if the file is missing.
//  2. Decide whether it's monolithic (top-level `probes:` or
//     `storage:`). If not, the layout is already multi-file →
//     report "nothing to do" and exit 0.
//  3. Take a snapshot of what the agent currently sees via
//     LoadFromDisk (so we can verify equality post-write).
//  4. Backup the source file to <configPath>.pre-multi-file.<ts>.
//  5. Write `agent.yaml` (globals only) at configPath, replacing
//     the monolithic source.
//  6. Write probes.d/00-host.yaml (the full probes list).
//  7. Write one strategies.d/NN-<name>.yaml per strategy, preserving
//     source order.
//  8. Verify the new layout loads to the same merged data as the
//     pre-write snapshot. On mismatch, restore the backup and bail.
//
// Comment preservation: the source file's comments are NOT carried
// into the generated fragments — they live in the backup file. New
// fragments are written with their own helpful comment headers.
// Operators who want comments inline can edit the fragments after
// migration.
// MigrateResult summarises what runMigrate did. AlreadyMultiFile is
// true when no migration was needed (idempotent no-op). BackupPath
// is set when the migration touched disk — it points at the
// timestamped copy of the source file that the runner can restore
// from manually. StrategyCount counts the number of files written
// under strategies.d/.
type MigrateResult struct {
	AlreadyMultiFile bool
	BackupPath       string
	StrategyCount    int
	WroteProbes      bool
}

// runMigrate is the testable migrate engine — same behaviour as
// migrateConfig minus the os.Exit calls. Returns a MigrateResult on
// success (including the idempotent no-op) and an error on any
// failure; the caller decides how to surface the outcome.
//
// On any disk-touching error after the backup has been written,
// runMigrate restores the backup before returning the error.
func runMigrate(configPath string) (MigrateResult, error) {
	result := MigrateResult{}
	if configPath == "" {
		return result, fmt.Errorf("no config path provided")
	}

	raw, err := os.ReadFile(configPath) // #nosec G304 - operator-provided path for migration
	if err != nil {
		return result, fmt.Errorf("cannot read %s: %w", configPath, err)
	}

	if !hasMonolithicMarkers(raw) {
		result.AlreadyMultiFile = true
		return result, nil
	}

	// Snapshot the merged view BEFORE we touch anything.
	loaderLog := newCheckLogger("configuration.migrate")
	before, err := configuration.LoadFromDisk(configPath, loaderLog)
	if err != nil {
		return result, fmt.Errorf("failed to load current configuration: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	backupPath := configPath + ".pre-multi-file." + timestamp
	if err := os.WriteFile(backupPath, raw, 0600); err != nil {
		return result, fmt.Errorf("failed to write backup at %s: %w", backupPath, err)
	}
	result.BackupPath = backupPath

	// Parse the source as a free-form map.
	var srcMap map[string]interface{}
	if err := yaml.Unmarshal(raw, &srcMap); err != nil {
		restoreBackup(configPath, backupPath)
		return result, fmt.Errorf("parse source: %w", err)
	}

	probesRaw := srcMap["probes"]
	storageRaw := srcMap["storage"]
	delete(srcMap, "probes")
	delete(srcMap, "storage")

	// New agent.yaml (globals only).
	globalsYAML, err := yaml.Marshal(srcMap)
	if err != nil {
		restoreBackup(configPath, backupPath)
		return result, fmt.Errorf("marshal globals: %w", err)
	}
	agentYAML := append([]byte("# SenHub Agent — globals (migrated to multi-file layout on "+timestamp+")\n"+
		"# Probes live in probes.d/ ; storage strategies in strategies.d/.\n"+
		"# Original monolithic file kept as "+filepath.Base(backupPath)+".\n\n"), globalsYAML...)
	if err := os.WriteFile(configPath, agentYAML, 0600); err != nil {
		restoreBackup(configPath, backupPath)
		return result, fmt.Errorf("write agent.yaml: %w", err)
	}

	configDir := filepath.Dir(configPath)
	probesDir := filepath.Join(configDir, "probes.d")
	strategiesDir := filepath.Join(configDir, "strategies.d")
	if err := os.MkdirAll(probesDir, 0750); err != nil {
		restoreBackup(configPath, backupPath)
		return result, fmt.Errorf("mkdir %s: %w", probesDir, err)
	}
	if err := os.MkdirAll(strategiesDir, 0750); err != nil {
		restoreBackup(configPath, backupPath)
		return result, fmt.Errorf("mkdir %s: %w", strategiesDir, err)
	}

	// probes.d/00-host.yaml — the whole probes list as a YAML array.
	if probesRaw != nil {
		probesYAML, err := yaml.Marshal(probesRaw)
		if err != nil {
			restoreBackup(configPath, backupPath)
			return result, fmt.Errorf("marshal probes: %w", err)
		}
		probesPath := filepath.Join(probesDir, "00-host.yaml")
		body := append([]byte("# Probes migrated from monolithic config on "+timestamp+".\n"+
			"# Add new probes by creating additional files in this directory\n"+
			"# (e.g. 10-mydb.yaml). Files load alphabetically.\n\n"), probesYAML...)
		if err := os.WriteFile(probesPath, body, 0600); err != nil {
			restoreBackup(configPath, backupPath)
			return result, fmt.Errorf("write %s: %w", probesPath, err)
		}
		result.WroteProbes = true
	}

	// strategies.d/NN-<name>.yaml — one file per strategy.
	count, err := splitStrategies(storageRaw, strategiesDir, timestamp)
	if err != nil {
		restoreBackup(configPath, backupPath)
		return result, err
	}
	result.StrategyCount = count

	// Equality check.
	after, err := configuration.LoadFromDisk(configPath, loaderLog)
	if err != nil {
		restoreBackup(configPath, backupPath)
		return result, fmt.Errorf("post-write load failed: %w", err)
	}
	if !reflect.DeepEqual(before, after) {
		restoreBackup(configPath, backupPath)
		return result, fmt.Errorf("post-write data drifted from pre-write snapshot — backup restored")
	}

	return result, nil
}

// migrateConfig is the CLI wrapper around runMigrate. It prints
// human-readable progress to stdout and exits non-zero on any error.
func migrateConfig(configPath string) {
	if configPath == "" {
		fmt.Fprintln(os.Stderr, "config migrate: no config path provided")
		os.Exit(2)
	}

	if abs, err := filepath.Abs(configPath); err == nil {
		configPath = abs
	}
	fmt.Printf("Migrating configuration: %s\n", configPath)

	result, err := runMigrate(configPath)
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

// hasMonolithicMarkers detects a top-level `probes:` or `storage:`
// block — the signal that the file is still in the legacy
// monolithic layout. We unmarshal into a tiny shape rather than
// scanning bytes so YAML edge cases (comments, multi-line literals)
// don't confuse the detection.
func hasMonolithicMarkers(raw []byte) bool {
	var probe struct {
		Probes  []interface{} `yaml:"probes"`
		Storage []interface{} `yaml:"storage"`
	}
	if err := yaml.Unmarshal(raw, &probe); err != nil {
		// If we cannot parse, assume monolithic so the migrate path
		// surfaces the parse error rather than silently exiting "ok".
		return true
	}
	return probe.Probes != nil || probe.Storage != nil
}

// splitStrategies turns a yaml.v2 free-form `storage:` value into
// per-strategy files under strategiesDir. The yaml.v2 unmarshal of a
// list-of-maps produces []interface{} of map[interface{}]interface{};
// we coerce each entry into a {Name, Params} pair and emit one file
// per strategy, named NN-<name>.yaml where NN is a zero-padded index
// preserving source order.
func splitStrategies(storageRaw interface{}, strategiesDir, timestamp string) (int, error) {
	if storageRaw == nil {
		return 0, nil
	}
	list, ok := storageRaw.([]interface{})
	if !ok {
		return 0, fmt.Errorf("source `storage:` block is not a list (%T)", storageRaw)
	}

	// Sort-stable name → params extraction. We keep source order
	// because operators may rely on it (e.g. the senhub strategy
	// appears before prtg). The file-prefix NN reflects that order.
	type entry struct {
		name   string
		params map[string]interface{}
	}
	var entries []entry
	for i, raw := range list {
		m, ok := raw.(map[interface{}]interface{})
		if !ok {
			// yaml.v3 returns map[string]interface{} — accept both.
			if mss, ok2 := raw.(map[string]interface{}); ok2 {
				m = make(map[interface{}]interface{}, len(mss))
				for k, v := range mss {
					m[k] = v
				}
			} else {
				return 0, fmt.Errorf("strategy #%d is not a map (%T)", i, raw)
			}
		}
		nameVal, hasName := m["name"]
		if !hasName {
			return 0, fmt.Errorf("strategy #%d has no `name` field", i)
		}
		name, ok := nameVal.(string)
		if !ok {
			return 0, fmt.Errorf("strategy #%d name is not a string (%T)", i, nameVal)
		}
		paramsRaw := m["params"]
		params, _ := coerceMap(paramsRaw)
		entries = append(entries, entry{name: name, params: params})
	}

	// Dedup case: two storage entries with the same name. The
	// strategies.d layout enforces one file per strategy (loader
	// rejects multiple top-level keys per file but tolerates the same
	// name across files). We pick the LAST occurrence — matches the
	// loader's "later file wins" override semantics.
	finalByName := map[string]entry{}
	order := []string{}
	for _, e := range entries {
		if _, seen := finalByName[e.name]; !seen {
			order = append(order, e.name)
		}
		finalByName[e.name] = e
	}
	sort.SliceStable(order, func(i, j int) bool { return false }) // preserve append order

	for i, name := range order {
		e := finalByName[name]
		prefix := fmt.Sprintf("%02d", (i+1)*10)
		filename := prefix + "-" + safeFilenameComponent(name) + ".yaml"
		path := filepath.Join(strategiesDir, filename)

		single := map[string]interface{}{name: e.params}
		body, err := yaml.Marshal(single)
		if err != nil {
			return 0, fmt.Errorf("marshal strategy %q: %w", name, err)
		}
		header := []byte("# Strategy '" + name + "' migrated from monolithic config on " + timestamp + ".\n" +
			"# This file MUST have exactly one top-level key — the strategy name.\n\n")
		if err := os.WriteFile(path, append(header, body...), 0600); err != nil {
			return 0, fmt.Errorf("write %s: %w", path, err)
		}
	}
	return len(order), nil
}

// coerceMap normalises yaml.v2's map[interface{}]interface{} into the
// map[string]interface{} the rest of the agent expects.
func coerceMap(raw interface{}) (map[string]interface{}, bool) {
	if raw == nil {
		return nil, false
	}
	if m, ok := raw.(map[string]interface{}); ok {
		return m, true
	}
	mi, ok := raw.(map[interface{}]interface{})
	if !ok {
		return nil, false
	}
	out := make(map[string]interface{}, len(mi))
	for k, v := range mi {
		ks, _ := k.(string)
		if ks == "" {
			continue
		}
		out[ks] = v
	}
	return out, true
}

// safeFilenameComponent reduces a strategy name to characters safe
// for use as a filename. The loader matches strategies by their
// top-level YAML key, NOT the filename — so the filename can be
// cosmetic. We still sanitise to avoid OS quirks.
func safeFilenameComponent(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	s := b.String()
	if s == "" {
		return "strategy"
	}
	return s
}

// restoreBackup tries to put the original monolithic file back at
// configPath if anything went wrong mid-migration. Best-effort —
// errors are reported but don't change the exit code (the caller
// will exit non-zero anyway).
func restoreBackup(configPath, backupPath string) {
	data, err := os.ReadFile(backupPath) // #nosec G304 - path generated above
	if err != nil {
		fmt.Fprintf(os.Stderr, "config migrate: WARNING — backup restore failed (cannot read %s): %v\n", backupPath, err)
		return
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "config migrate: WARNING — backup restore failed (cannot write %s): %v\n", configPath, err)
		return
	}
	fmt.Fprintf(os.Stderr, "config migrate: backup restored from %s\n", backupPath)
}

// newCheckLogger builds the WARN-level module logger used by the
// migrate command to surface loader diagnostics (legacy detection,
// duplicate strategies).
func newCheckLogger(module string) *agentLogger.ModuleLogger {
	zlog := zerolog.New(os.Stderr).Level(zerolog.WarnLevel)
	base := (*agentLogger.Logger)(&zlog)
	return agentLogger.NewModuleLogger(base, module)
}
