package configuration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v2"
)

// One output is one file under strategies.d/ with the strategy name as
// its single top-level key. The console manages these files the way it
// manages probe fragments, with two differences: a strategy exists at
// most once (the loader keeps one per name), and it is disabled by
// renaming the file to *.disabled, which the loader and the watcher
// already honour, rather than by a flag inside it.

var strategyNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// IsValidStrategyName accepts the shape every shipped strategy name has.
func IsValidStrategyName(name string) bool { return strategyNamePattern.MatchString(name) }

// StrategyFragment is what the console knows about one output file.
type StrategyFragment struct {
	Name    string
	Path    string
	Enabled bool
	Managed bool
	Params  StorageConfigParams
	// Error says why the file could not be read as one strategy; the
	// name is then the file's, and Params is nil.
	Error string
}

// ListStrategyFragments reads every strategies.d file, enabled or
// disabled, so the console can list an output the loader skips. A file
// that does not parse is listed with no params rather than hidden.
func ListStrategyFragments(configPath string) ([]StrategyFragment, error) {
	if err := refuseUnlessMultiFile(configPath); err != nil {
		return nil, err
	}
	dir := filepath.Join(filepath.Dir(configPath), "strategies.d")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var out []StrategyFragment
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		enabled := strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
		disabled := strings.HasSuffix(name, ".disabled")
		if !enabled && !disabled {
			continue
		}
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path) // #nosec G304 - path is under strategies.d/
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		var single map[string]StorageConfigParams
		if err := yaml.Unmarshal(raw, &single); err != nil {
			out = append(out, StrategyFragment{Name: strings.TrimSuffix(strings.TrimSuffix(name, ".disabled"), ".yaml"), Path: path, Enabled: enabled, Error: err.Error()})
			continue
		}
		if len(single) != 1 {
			out = append(out, StrategyFragment{Name: strings.TrimSuffix(strings.TrimSuffix(name, ".disabled"), ".yaml"), Path: path, Enabled: enabled, Error: fmt.Sprintf("expected exactly one top-level strategy key, got %d", len(single))})
			continue
		}
		for sname, params := range single {
			out = append(out, StrategyFragment{
				Name: sname, Path: path, Enabled: enabled,
				Managed: bytes.HasPrefix(raw, []byte(managedFragmentHeader)),
				Params:  convertMapTypes(map[string]interface{}(params)).(map[string]interface{}),
			})
		}
	}
	return out, nil
}

// findStrategyFile returns the current file of a strategy, enabled or
// disabled, or "" when none exists.
func findStrategyFile(configPath, name string) (string, error) {
	frags, err := ListStrategyFragments(configPath)
	if err != nil {
		return "", err
	}
	for _, f := range frags {
		if f.Name == name {
			return f.Path, nil
		}
	}
	return "", nil
}

// StrategyFragmentEnabled says whether an output's file is the enabled
// one. It lets a partial update keep the state the operator chose
// instead of assuming enabled.
func StrategyFragmentEnabled(configPath, name string) (bool, error) {
	frags, err := ListStrategyFragments(configPath)
	if err != nil {
		return false, err
	}
	for _, f := range frags {
		if f.Name == name {
			return f.Enabled, nil
		}
	}
	return false, fmt.Errorf("no output named %q under strategies.d", name)
}

// strategyFragmentPath is where the console creates a new output: after
// the 00-http.yaml the install writes, before nothing in particular.
func strategyFragmentPath(configPath, name string) string {
	return filepath.Join(filepath.Dir(configPath), "strategies.d", "50-"+safeFilenameComponent(name)+".yaml")
}

// CreateStrategyFragment writes a new output as its own managed file,
// under the .disabled name when it is created disabled so the watcher
// never starts it. It refuses a name already present in strategies.d
// (enabled or not) and the legacy layout. Secret values are sealed the
// way probe fragments seal theirs, under the key strategies.<name>.<path>.
func CreateStrategyFragment(configPath, name string, params StorageConfigParams, enabled bool, secretPaths []string) (string, error) {
	if err := refuseUnlessMultiFile(configPath); err != nil {
		return "", err
	}
	if !IsValidStrategyName(name) {
		return "", fmt.Errorf("output name %q: use lowercase letters, digits and underscores", name)
	}
	existing, err := findStrategyFile(configPath, name)
	if err != nil {
		return "", err
	}
	if existing != "" {
		return "", fmt.Errorf("an output %q already exists (%s); edit it instead", name, existing)
	}
	path := strategyFragmentPath(configPath, name)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists", path)
	}
	if !enabled {
		path += ".disabled"
	}
	DropNilValues(params)
	if err := sealParams("strategies."+name, params, secretPaths); err != nil {
		return "", err
	}
	return path, writeManagedStrategyFragment(path, name, params)
}

// UpdateStrategyFragment rewrites an output's file with the given
// params and moves it between enabled and disabled as asked. A file the
// operator wrote by hand is taken over: the console is the only writer
// of strategies.d once it has been used, and the install's own
// 00-http.yaml carries nothing but its defaults.
func UpdateStrategyFragment(configPath, name string, params StorageConfigParams, enabled bool, secretPaths []string) (string, error) {
	if err := refuseUnlessMultiFile(configPath); err != nil {
		return "", err
	}
	path, err := findStrategyFile(configPath, name)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("no output named %q under strategies.d", name)
	}
	existing, err := StrategyFragmentParams(configPath, name)
	if err != nil {
		return "", err
	}
	params = KeepStoredReferences(existing, params)
	DropNilValues(params)
	if err := sealParams("strategies."+name, params, secretPaths); err != nil {
		return "", err
	}
	target := path
	switch {
	case enabled && strings.HasSuffix(path, ".disabled"):
		target = strings.TrimSuffix(path, ".disabled")
	case !enabled && !strings.HasSuffix(path, ".disabled"):
		target = path + ".disabled"
	}
	if err := writeManagedStrategyFragmentAs(target, name, params, fileModeOr(path, 0o600)); err != nil {
		return "", err
	}
	if target != path {
		if err := os.Remove(path); err != nil {
			return "", fmt.Errorf("removing %s after writing %s: %w", path, target, err)
		}
	}
	return target, nil
}

// DeleteStrategyFragment removes an output's file, enabled or disabled.
func DeleteStrategyFragment(configPath, name string) (string, error) {
	path, err := findStrategyFile(configPath, name)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("no output named %q under strategies.d", name)
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("removing %s: %w", path, err)
	}
	return path, nil
}

func writeManagedStrategyFragment(path, name string, params StorageConfigParams) error {
	return writeManagedStrategyFragmentAs(path, name, params, fileModeOr(path, 0o600))
}

func writeManagedStrategyFragmentAs(path, name string, params StorageConfigParams, mode os.FileMode) error {
	body, err := yaml.Marshal(map[string]StorageConfigParams{name: params})
	if err != nil {
		return fmt.Errorf("encoding output %q: %w", name, err)
	}
	out := append([]byte(managedFragmentHeader+"\n\n"), body...)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := atomicWriteFile(path, out, mode); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
