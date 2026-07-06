package configuration

// Monolithic → multi-file harmonisation. Converts a legacy single
// agent-config.yaml (top-level `probes:` + `storage:`) into the
// 0.2.x+ layout: agent.yaml (globals) + probes.d/ + strategies.d/.
//
// This lives in the configuration domain — not the CLI layer — so the
// boot-time seal can force every install onto one layout before
// sealing secrets, and the `agent config migrate` CLI is a thin
// wrapper over the same engine.

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	"senhub-agent.go/internal/agent/services/logger"
)

// MigrateResult summarises what MigrateToMultiFile did.
// AlreadyMultiFile is true when no migration was needed (idempotent
// no-op). BackupPath is set when the migration touched disk — it
// points at the timestamped copy of the source file. StrategyCount
// counts the files written under strategies.d/.
type MigrateResult struct {
	AlreadyMultiFile bool
	BackupPath       string
	StrategyCount    int
	WroteProbes      bool
}

// MigrateToMultiFile converts a monolithic configuration into the
// multi-file layout. It is the testable engine behind both the
// `agent config migrate` CLI and the boot-time harmonisation step.
//
// Steps:
//  1. Read configPath; refuse if missing.
//  2. If the file is already multi-file (no top-level probes/storage),
//     report AlreadyMultiFile and return — idempotent no-op.
//  3. Snapshot the merged view via LoadFromDisk (for the post-write
//     equality check).
//  4. Backup the source to <configPath>.pre-multi-file.<ts>.
//  5. Rewrite configPath as agent.yaml (globals only).
//  6. Write probes.d/00-host.yaml (the full probes list).
//  7. Write one strategies.d/NN-<name>.yaml per strategy, in order.
//  8. Verify the new layout loads to the same merged data as the
//     snapshot; on mismatch, restore the backup and return an error.
//
// Source-file comments are not carried into the fragments — they live
// in the backup. Fragments get their own header comments.
func MigrateToMultiFile(configPath string, log *logger.ModuleLogger) (MigrateResult, error) {
	result := MigrateResult{}
	if configPath == "" {
		return result, fmt.Errorf("no config path provided")
	}

	raw, err := os.ReadFile(configPath) // #nosec G304 - operator-provided config path
	if err != nil {
		return result, fmt.Errorf("cannot read %s: %w", configPath, err)
	}

	if !HasMonolithicMarkers(raw) {
		result.AlreadyMultiFile = true
		return result, nil
	}

	// Snapshot the merged view BEFORE we touch anything.
	before, err := LoadFromDisk(configPath, log)
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
		restoreSplitBackup(configPath, backupPath, log)
		return result, fmt.Errorf("parse source: %w", err)
	}

	probesRaw := srcMap["probes"]
	storageRaw := srcMap["storage"]
	delete(srcMap, "probes")
	delete(srcMap, "storage")

	// New agent.yaml (globals only).
	globalsYAML, err := yaml.Marshal(srcMap)
	if err != nil {
		restoreSplitBackup(configPath, backupPath, log)
		return result, fmt.Errorf("marshal globals: %w", err)
	}
	agentYAML := append([]byte("# SenHub Agent — globals (migrated to multi-file layout on "+timestamp+")\n"+
		"# Probes live in probes.d/ ; storage strategies in strategies.d/.\n"+
		"# Original monolithic file kept as "+filepath.Base(backupPath)+".\n\n"), globalsYAML...)
	if err := atomicWriteFile(configPath, agentYAML, 0600); err != nil {
		restoreSplitBackup(configPath, backupPath, log)
		return result, fmt.Errorf("write agent.yaml: %w", err)
	}

	configDir := filepath.Dir(configPath)
	probesDir := filepath.Join(configDir, "probes.d")
	strategiesDir := filepath.Join(configDir, "strategies.d")
	if err := os.MkdirAll(probesDir, 0750); err != nil {
		restoreSplitBackup(configPath, backupPath, log)
		return result, fmt.Errorf("mkdir %s: %w", probesDir, err)
	}
	if err := os.MkdirAll(strategiesDir, 0750); err != nil {
		restoreSplitBackup(configPath, backupPath, log)
		return result, fmt.Errorf("mkdir %s: %w", strategiesDir, err)
	}

	// probes.d/00-host.yaml — the whole probes list as a YAML array.
	if probesRaw != nil {
		probesYAML, err := yaml.Marshal(probesRaw)
		if err != nil {
			restoreSplitBackup(configPath, backupPath, log)
			return result, fmt.Errorf("marshal probes: %w", err)
		}
		probesPath := filepath.Join(probesDir, "00-host.yaml")
		body := append([]byte("# Probes migrated from monolithic config on "+timestamp+".\n"+
			"# Add new probes by creating additional files in this directory\n"+
			"# (e.g. 10-mydb.yaml). Files load alphabetically.\n\n"), probesYAML...)
		if err := atomicWriteFile(probesPath, body, 0600); err != nil {
			restoreSplitBackup(configPath, backupPath, log)
			return result, fmt.Errorf("write %s: %w", probesPath, err)
		}
		result.WroteProbes = true
	}

	// strategies.d/NN-<name>.yaml — one file per strategy.
	count, err := splitStrategies(storageRaw, strategiesDir, timestamp)
	if err != nil {
		restoreSplitBackup(configPath, backupPath, log)
		return result, err
	}
	result.StrategyCount = count

	// Equality check.
	after, err := LoadFromDisk(configPath, log)
	if err != nil {
		restoreSplitBackup(configPath, backupPath, log)
		return result, fmt.Errorf("post-write load failed: %w", err)
	}
	if !reflect.DeepEqual(before, after) {
		restoreSplitBackup(configPath, backupPath, log)
		return result, fmt.Errorf("post-write data drifted from pre-write snapshot — backup restored")
	}

	return result, nil
}

// HasMonolithicMarkers detects a top-level `probes:` or `storage:`
// block — the signal that the file is still in the legacy monolithic
// layout. We unmarshal into a tiny shape rather than scanning bytes so
// YAML edge cases (comments, multi-line literals) don't confuse the
// detection.
func HasMonolithicMarkers(raw []byte) bool {
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
// per-strategy files under strategiesDir. yaml.v2 unmarshals a
// list-of-maps as []interface{} of map[interface{}]interface{}; we
// coerce each entry into a {Name, Params} pair and emit one file per
// strategy, named NN-<name>.yaml where NN preserves source order.
func splitStrategies(storageRaw interface{}, strategiesDir, timestamp string) (int, error) {
	if storageRaw == nil {
		return 0, nil
	}
	list, ok := storageRaw.([]interface{})
	if !ok {
		return 0, fmt.Errorf("source `storage:` block is not a list (%T)", storageRaw)
	}

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

	// Dedup: two storage entries with the same name. The loader
	// tolerates the same name across files, picking the later one; we
	// keep the LAST occurrence to match that "later wins" semantics.
	finalByName := map[string]entry{}
	order := []string{}
	for _, e := range entries {
		if _, seen := finalByName[e.name]; !seen {
			order = append(order, e.name)
		}
		finalByName[e.name] = e
	}

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
		if err := atomicWriteFile(path, append(header, body...), 0600); err != nil {
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

// safeFilenameComponent reduces a strategy name to characters safe for
// a filename. The loader matches strategies by their top-level YAML
// key, not the filename, so the filename is cosmetic — we still
// sanitise to avoid OS quirks.
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

// restoreSplitBackup tries to put the original monolithic file back at
// configPath if anything went wrong mid-migration. Best-effort: it
// logs failures through the module logger when one is supplied, else
// falls back to stderr so the operator running the CLI still sees it.
func restoreSplitBackup(configPath, backupPath string, log *logger.ModuleLogger) {
	data, err := os.ReadFile(backupPath) // #nosec G304 - path generated above
	if err != nil {
		warnRestore(log, fmt.Sprintf("backup restore failed (cannot read %s): %v", backupPath, err))
		return
	}
	if err := atomicWriteFile(configPath, data, 0600); err != nil {
		warnRestore(log, fmt.Sprintf("backup restore failed (cannot write %s): %v", configPath, err))
		return
	}
	warnRestore(log, fmt.Sprintf("backup restored from %s", backupPath))
}

func warnRestore(log *logger.ModuleLogger, msg string) {
	if log != nil {
		log.Warn().Msg(msg)
		return
	}
	fmt.Fprintf(os.Stderr, "config migrate: %s\n", msg)
}
