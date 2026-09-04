package configuration

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// FindStrategyFragment returns the path of the strategies.d/ fragment whose
// single top-level key is strategyName (e.g. "http"), for the multi-file
// layout. It returns "" with no error when the multi-file layout is in use
// but no such fragment exists, and an error when the config is the legacy
// monolithic layout (where strategies live inline under storage: and a
// targeted edit is a different, unsupported operation for now).
func FindStrategyFragment(configPath, strategyName string) (string, error) {
	raw, err := os.ReadFile(configPath) // #nosec G304 - operator-provided config path
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", configPath, err)
	}
	legacy, err := isLegacyMonolithic(raw)
	if err != nil {
		return "", err
	}
	if legacy {
		return "", fmt.Errorf("this is a legacy monolithic configuration; 'config set' edits the multi-file layout — run 'config migrate' first")
	}
	dir := filepath.Join(filepath.Dir(configPath), "strategies.d")
	files, err := listYAMLFiles(dir)
	if err != nil {
		return "", err
	}
	for _, path := range files {
		fragRaw, err := os.ReadFile(path) // #nosec G304 - path is under strategies.d/
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", path, err)
		}
		var single map[string]yaml.Node
		if err := yaml.Unmarshal(fragRaw, &single); err != nil {
			continue // a malformed sibling must not hide the one we want
		}
		if _, ok := single[strategyName]; ok {
			return path, nil
		}
	}
	return "", nil
}

// SetStrategyScalar sets param=value (as a scalar with the given YAML tag,
// e.g. "!!int" or "!!str") under the strategyName block of its strategies.d/
// fragment, preserving the rest of the file — comments, key order and sibling
// strategies — through a yaml.Node round-trip. The write is atomic.
//
// The running agent picks the change up through the configuration watcher, so
// no restart is needed. It backs both `config set` and the web-UI settings
// page.
func SetStrategyScalar(configPath, strategyName, param, value, tag string) error {
	fragPath, err := FindStrategyFragment(configPath, strategyName)
	if err != nil {
		return err
	}
	if fragPath == "" {
		return fmt.Errorf("no %q strategy fragment found under strategies.d/", strategyName)
	}
	raw, err := os.ReadFile(fragPath) // #nosec G304 - path resolved from strategies.d/
	if err != nil {
		return fmt.Errorf("reading %s: %w", fragPath, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parsing %s: %w", fragPath, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s: unexpected shape, expected a single %q mapping", fragPath, strategyName)
	}
	root := doc.Content[0]
	block := mappingChild(root, strategyName)
	if block == nil || block.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: no %q mapping to edit", fragPath, strategyName)
	}
	setTypedScalarField(block, param, value, tag)

	out, err := marshalDocument(&doc)
	if err != nil {
		return fmt.Errorf("re-encoding %s: %w", fragPath, err)
	}
	if err := atomicWriteFile(fragPath, out, fileModeOr(fragPath, 0o600)); err != nil {
		return fmt.Errorf("writing %s: %w", fragPath, err)
	}
	return nil
}

// setTypedScalarField sets (or adds) key=value as a scalar with the given tag
// under m. Unlike setScalarField (which always writes a quoted string), this
// lets a caller write an unquoted integer ("!!int") so a numeric param such as
// the HTTP port round-trips as a number the strategy parser accepts.
func setTypedScalarField(m *yaml.Node, key, value, tag string) {
	if v := mappingChild(m, key); v != nil {
		v.Kind = yaml.ScalarNode
		v.Tag = tag
		v.Value = value
		v.Content = nil
		return
	}
	appendPair(m, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value})
}
