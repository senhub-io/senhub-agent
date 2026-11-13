package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	yamlv2 "gopkg.in/yaml.v2"
	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/services/configuration/secret"
	"senhub-agent.go/internal/agent/services/logger"
)

// RedactedFile is one loaded configuration file, redacted the way
// `config show --redact` redacts it.
type RedactedFile struct {
	// Name is relative to the configuration directory and slash-separated
	// ("agent.yaml", "probes.d/01-system.yaml"); "environment" holds the
	// probes the SENHUB_PROBE_* variables declare or change.
	Name string
	// Body is the YAML `config show --redact` would print for this file.
	Body []byte
}

// RedactedConfigFiles returns the configuration at configPath one entry
// per loaded file. Each body goes through redactedCopy, the pass behind
// `config show --redact`, so the two views cannot mask differently.
func RedactedConfigFiles(configPath string, log *logger.ModuleLogger) ([]RedactedFile, error) {
	raw, err := os.ReadFile(configPath) // #nosec G304 - configPath is the operator-supplied --config-path
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", configPath, err)
	}
	legacy, err := isLegacyMonolithic(raw)
	if err != nil {
		return nil, fmt.Errorf("scanning %s for legacy markers: %w", configPath, err)
	}

	baseDir := filepath.Dir(configPath)
	secret.SetConfigDir(baseDir)

	if legacy {
		data, err := LoadForShow(configPath, ShowRedact, log)
		if err != nil {
			return nil, err
		}
		body, err := MarshalSortedYAML(&data)
		if err != nil {
			return nil, fmt.Errorf("marshalling %s: %w", configPath, err)
		}
		return []RedactedFile{{Name: filepath.Base(configPath), Body: body}}, nil
	}

	var out []RedactedFile

	top, err := redactedCopy(loadTopFile(configPath))
	if err != nil {
		return nil, err
	}
	topBody, err := MarshalSortedYAML(&top)
	if err != nil {
		return nil, fmt.Errorf("marshalling %s: %w", configPath, err)
	}
	out = append(out, RedactedFile{Name: filepath.Base(configPath), Body: topBody})

	probesDir := filepath.Join(baseDir, "probes.d")
	probeFiles, err := listYAMLFiles(probesDir)
	if err != nil {
		return nil, err
	}
	for _, path := range probeFiles {
		redacted, err := redactedCopy(loadProbeFile(path))
		if err != nil {
			return nil, err
		}
		body, err := MarshalSortedYAML(redacted.Probes)
		if err != nil {
			return nil, fmt.Errorf("marshalling %s: %w", path, err)
		}
		out = append(out, RedactedFile{
			Name: filepath.ToSlash(filepath.Join("probes.d", filepath.Base(path))),
			Body: body,
		})
	}

	strategiesDir := filepath.Join(baseDir, "strategies.d")
	strategyFiles, err := listYAMLFiles(strategiesDir)
	if err != nil {
		return nil, err
	}
	for _, path := range strategyFiles {
		redacted, err := redactedCopy(loadStrategyFile(path))
		if err != nil {
			return nil, err
		}
		params := make(map[string]StorageConfigParams, len(redacted.Storage))
		for _, sc := range redacted.Storage {
			params[sc.Name] = sc.Params
		}
		body, err := MarshalSortedYAML(params)
		if err != nil {
			return nil, fmt.Errorf("marshalling %s: %w", path, err)
		}
		out = append(out, RedactedFile{
			Name: filepath.ToSlash(filepath.Join("strategies.d", filepath.Base(path))),
			Body: body,
		})
	}

	envFiles, err := envRedactedFile(configPath, log)
	if err != nil {
		return nil, err
	}
	out = append(out, envFiles...)

	return out, nil
}

func loadTopFile(configPath string) func() (LocalConfigurationData, error) {
	return func() (LocalConfigurationData, error) {
		raw, err := os.ReadFile(configPath) // #nosec G304 - configPath is the operator-supplied --config-path
		if err != nil {
			return LocalConfigurationData{}, fmt.Errorf("reading %s: %w", configPath, err)
		}
		var data LocalConfigurationData
		if err := yaml.Unmarshal(raw, &data); err != nil {
			return LocalConfigurationData{}, fmt.Errorf("parsing %s: %w", configPath, err)
		}
		return normalizeYAMLTypes(data), nil
	}
}

func loadProbeFile(path string) func() (LocalConfigurationData, error) {
	return func() (LocalConfigurationData, error) {
		raw, err := os.ReadFile(path) // #nosec G304 - path is under the configured probes.d/ directory
		if err != nil {
			return LocalConfigurationData{}, fmt.Errorf("reading %s: %w", path, err)
		}
		var batch []ProbeConfig
		if err := yamlv2.Unmarshal(raw, &batch); err != nil {
			return LocalConfigurationData{}, newParseError(path, err)
		}
		return normalizeYAMLTypes(LocalConfigurationData{Probes: batch}), nil
	}
}

func loadStrategyFile(path string) func() (LocalConfigurationData, error) {
	return func() (LocalConfigurationData, error) {
		raw, err := os.ReadFile(path) // #nosec G304 - path is under the configured strategies.d/ directory
		if err != nil {
			return LocalConfigurationData{}, fmt.Errorf("reading %s: %w", path, err)
		}
		var single map[string]StorageConfigParams
		if err := yamlv2.Unmarshal(raw, &single); err != nil {
			return LocalConfigurationData{}, newParseError(path, err)
		}
		if len(single) > 1 {
			return LocalConfigurationData{}, newParseError(path,
				fmt.Errorf("expected exactly one top-level strategy key, got %d (use one file per strategy)", len(single)))
		}

		storage := make([]StorageConfig, 0, len(single))
		names := make([]string, 0, len(single))
		for name := range single {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			storage = append(storage, StorageConfig{Name: name, Params: single[name]})
		}
		return normalizeYAMLTypes(LocalConfigurationData{Storage: storage}), nil
	}
}

func envRedactedFile(configPath string, log *logger.ModuleLogger) ([]RedactedFile, error) {
	if len(EnvProbeVariables()) == 0 {
		return nil, nil
	}

	merged, err := LoadForShow(configPath, ShowRedact, log)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(merged.Probes))
	for _, p := range merged.Probes {
		names = append(names, p.Name)
	}
	src := EnvProbeSources(names)

	kept := make([]ProbeConfig, 0)
	for _, p := range merged.Probes {
		if _, ok := src[p.Name]; ok {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return nil, nil
	}

	body, err := MarshalSortedYAML(kept)
	if err != nil {
		return nil, fmt.Errorf("marshalling environment probes: %w", err)
	}
	return []RedactedFile{{Name: "environment", Body: body}}, nil
}
