package configuration

// loader.go implements the multi-file configuration layout introduced
// in Sprint A. The agent boots from one of two arrangements,
// transparently:
//
//   Legacy monolithic — a single agent-config.yaml (or whatever path
//   --config-path points to) that contains top-level `probes:` and/or
//   `storage:` blocks. The existing single-file format. Detected
//   automatically; the *.d/ directories are IGNORED with a WARN log
//   so operators discover the convention without surprise.
//
//   Multi-file — `agent.yaml` containing ONLY global keys
//   (agent, cache, auto_update, log…) plus two sibling directories
//   `probes.d/` and `strategies.d/`. Each file in `probes.d/` is a
//   YAML array of probe configs; each file in `strategies.d/` carries
//   exactly one strategy (one top-level key). Files are loaded in
//   alphabetical order; `*.disabled` and dotfiles are skipped.
//
// Detection is based on the *content* of the top-level file, not its
// name — operators can still call it `agent.yaml` or stick with the
// legacy `agent-config.yaml`. The *.d/ directories are resolved as
// siblings of whatever path was passed in.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v2"

	"senhub-agent.go/internal/agent/services/logger"
)

// LoadFromDisk reads agent.yaml (path configPath) plus the optional
// `probes.d/` and `strategies.d/` directories that sit next to it,
// merges them, applies env/file substitution, and returns the final
// LocalConfigurationData ready to be consumed by LocalConfiguration.
//
// Operator-facing failure modes:
//
//   - configPath doesn't exist  → wrapped os error mentioning the path
//   - any .yaml file parses badly → wrapped error mentioning the file
//   - duplicate strategy across files → later file wins, WARN logged
//   - substitution failure with no default → wrapped error mentioning
//     the offending reference
//
// Backward compatibility: if configPath itself contains top-level
// `probes:` or `storage:`, the legacy monolithic path is taken. The
// *.d/ directories are NOT loaded in that case and a one-time WARN
// log surfaces the situation. Operators can migrate at their leisure
// by trimming the monolithic file down to just the global keys and
// distributing the rest across the *.d/ directories.
func LoadFromDisk(configPath string, log *logger.ModuleLogger) (LocalConfigurationData, error) {
	raw, err := os.ReadFile(configPath) // #nosec G304 - configPath comes from --config-path; opening it is the whole point
	if err != nil {
		return LocalConfigurationData{}, fmt.Errorf("reading %s: %w", configPath, err)
	}

	legacy, err := isLegacyMonolithic(raw)
	if err != nil {
		return LocalConfigurationData{}, fmt.Errorf("scanning %s for legacy markers: %w", configPath, err)
	}

	var data LocalConfigurationData
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return LocalConfigurationData{}, fmt.Errorf("parsing %s: %w", configPath, err)
	}

	baseDir := filepath.Dir(configPath)
	probesDir := filepath.Join(baseDir, "probes.d")
	strategiesDir := filepath.Join(baseDir, "strategies.d")

	if legacy {
		// Surface the situation once so operators can move at their
		// own pace — but don't break the agent. The .d/ directories
		// are deliberately not loaded: mixing layouts would produce
		// ambiguous "which list wins?" behaviour every audit log
		// reader would have to second-guess.
		if log != nil {
			probesDirExists := dirExists(probesDir)
			strategiesDirExists := dirExists(strategiesDir)
			if probesDirExists || strategiesDirExists {
				log.Warn().
					Str("config_path", configPath).
					Bool("probes_d_present", probesDirExists).
					Bool("strategies_d_present", strategiesDirExists).
					Msg("Legacy monolithic config detected (top-level probes:/storage: present) — *.d/ directories are IGNORED. Migrate by trimming probes and storage out of the top file.")
			}
		}
		if err := Substitute(&data); err != nil {
			return LocalConfigurationData{}, fmt.Errorf("substituting variables in %s: %w", configPath, err)
		}
		return data, nil
	}

	// Multi-file mode — pull probes and strategies from siblings.
	extraProbes, err := loadProbesD(probesDir)
	if err != nil {
		return LocalConfigurationData{}, err
	}
	extraStrategies, err := loadStrategiesD(strategiesDir, log)
	if err != nil {
		return LocalConfigurationData{}, err
	}

	merged := mergeConfigs(data, extraProbes, extraStrategies)
	if err := Substitute(&merged); err != nil {
		return LocalConfigurationData{}, fmt.Errorf("substituting variables: %w", err)
	}
	return merged, nil
}

// isLegacyMonolithic reports whether raw contains a top-level
// `probes:` or `storage:` key. We unmarshal into a tiny shape rather
// than scanning the raw bytes — comments, multi-line literals or
// other YAML edge cases would all confuse a naive byte scan, while
// the structural unmarshal is robust by construction.
func isLegacyMonolithic(raw []byte) (bool, error) {
	var probe struct {
		Probes  []interface{} `yaml:"probes"`
		Storage []interface{} `yaml:"storage"`
	}
	if err := yaml.Unmarshal(raw, &probe); err != nil {
		return false, err
	}
	return probe.Probes != nil || probe.Storage != nil, nil
}

// loadProbesD reads every NN-*.yaml file in dir (alphabetical order),
// parses each as a YAML array of probe configs, and returns the
// concatenated list. Files starting with `.` and files ending with
// `.disabled` are skipped — operators can disable a fragment by
// renaming it rather than commenting every line out.
//
// An empty or absent directory is a valid configuration (zero
// probes); only filesystem errors and parse errors abort the boot.
func loadProbesD(dir string) ([]ProbeConfig, error) {
	files, err := listYAMLFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	var all []ProbeConfig
	for _, path := range files {
		raw, err := os.ReadFile(path) // #nosec G304 - path is under the configured probes.d/ directory
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		var batch []ProbeConfig
		if err := yaml.Unmarshal(raw, &batch); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		// fixYAMLTypes-style coercion is done after the merge by the
		// caller; here we just append.
		all = append(all, batch...)
	}
	return all, nil
}

// loadStrategiesD reads every NN-*.yaml file in dir (alphabetical
// order). Each file has exactly ONE top-level key which is the
// strategy name; the value is the strategy's parameter map. Disabling
// a strategy is done by renaming the file to `*.disabled`.
//
// Duplicate strategies across files: later file wins (alphabetically
// later filename) and a WARN log surfaces the override so operators
// can spot accidental shadowing during their `agent config show`
// review.
func loadStrategiesD(dir string, log *logger.ModuleLogger) ([]StorageConfig, error) {
	files, err := listYAMLFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	// Preserve insertion order while detecting duplicates: name → last
	// index in `out`. We rewrite the slot for a duplicate rather than
	// appending, so the final slice has at most one entry per name.
	byName := map[string]int{}
	var out []StorageConfig

	for _, path := range files {
		raw, err := os.ReadFile(path) // #nosec G304 - path is under the configured strategies.d/ directory
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		// A single-strategy file is a one-entry YAML map:
		// `prometheus:\n  bind_address: …`
		var single map[string]StorageConfigParams
		if err := yaml.Unmarshal(raw, &single); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		if len(single) == 0 {
			// Empty file or all-comments — silent skip is fine,
			// rename to .disabled if the operator wanted explicit
			// opt-out.
			continue
		}
		if len(single) > 1 {
			return nil, fmt.Errorf("parsing %s: expected exactly one top-level strategy key, got %d (use one file per strategy)", path, len(single))
		}
		for name, params := range single {
			cfg := StorageConfig{Name: name, Params: params}
			if existingIdx, dup := byName[name]; dup {
				if log != nil {
					log.Warn().
						Str("strategy", name).
						Str("overriding_file", path).
						Msg("Duplicate strategy across strategies.d/ files; later file wins")
				}
				out[existingIdx] = cfg
			} else {
				byName[name] = len(out)
				out = append(out, cfg)
			}
		}
	}
	return out, nil
}

// listYAMLFiles returns the absolute paths of every *.yaml / *.yml
// file in dir, alphabetically sorted, with dotfiles and *.disabled
// entries filtered out. An absent directory returns an empty list
// without error — empty probes.d/ is a valid zero-probe config and
// boot must succeed.
func listYAMLFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading directory %s: %w", dir, err)
	}

	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".disabled") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}
	sort.Strings(paths)
	return paths, nil
}

// mergeConfigs returns a copy of base with probes and strategies
// appended. The base file (agent.yaml) may also carry its own
// `probes:` or `storage:` lists in multi-file mode for the rare
// operator who has one probe inline and the rest in .d/ — in that
// case the inline entries come FIRST, then the .d/ entries in
// alphabetical-file order. The order doesn't affect runtime
// behaviour (the registry keys by name) but it is the order in
// which `agent config show` will print them.
func mergeConfigs(base LocalConfigurationData, probes []ProbeConfig, strategies []StorageConfig) LocalConfigurationData {
	out := base
	out.Probes = append(append([]ProbeConfig{}, base.Probes...), probes...)
	out.Storage = append(append([]StorageConfig{}, base.Storage...), strategies...)
	return out
}

// dirExists is a one-liner predicate kept here so the call site
// reads naturally — we don't care WHY the directory is missing
// (permission denied, never created, …), only whether the WARN log
// should fire.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
