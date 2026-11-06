package configuration

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/services/extension"
)

// KnownRootKeys lists the top-level keys of agent.yaml the loader
// reads: the yaml tag names of LocalConfigurationData (an inline field
// contributes none) plus every block a registered extension claims.
func KnownRootKeys() []string {
	seen := map[string]bool{}
	var keys []string
	for _, k := range rootYAMLKeysOf(reflect.TypeOf(LocalConfigurationData{})) {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for block := range extension.ConfigBlocks() {
		if !seen[block] {
			seen[block] = true
			keys = append(keys, block)
		}
	}
	sort.Strings(keys)
	return keys
}

// rootYAMLKeysOf is yamlKeysOf without the inline fields: a field
// tagged ",inline" spreads its own keys at the top level and names no
// key of its own.
func rootYAMLKeysOf(t reflect.Type) []string {
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("yaml")
		if strings.Contains(tag, ",inline") {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = strings.ToLower(t.Field(i).Name)
		}
		keys = append(keys, name)
	}
	return keys
}

// FindUnknownRootKeys reads the top-level file of configPath (not the
// *.d fragments) and returns, sorted, the top-level mapping keys no
// field or extension reads: a misspelt block name the loader drops
// without a word.
func FindUnknownRootKeys(configPath string) ([]string, error) {
	raw, err := os.ReadFile(configPath) // #nosec G304 - configPath comes from --config-path
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", configPath, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("reading %s: %w", configPath, err)
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil
	}
	known := map[string]bool{}
	for _, k := range KnownRootKeys() {
		known[k] = true
	}
	var unknown []string
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		if key == "<<" || known[key] {
			continue
		}
		unknown = append(unknown, key)
	}
	sort.Strings(unknown)
	return unknown, nil
}
