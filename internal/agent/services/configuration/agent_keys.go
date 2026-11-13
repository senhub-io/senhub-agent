package configuration

import (
	"fmt"
	"os"
	"reflect"
	"sort"

	"gopkg.in/yaml.v3"
)

// KnownAgentKeys lists the keys of the agent block the loader reads: the
// yaml tag names of LocalAgentConfig.
func KnownAgentKeys() []string {
	keys := rootYAMLKeysOf(reflect.TypeOf(LocalAgentConfig{}))
	sort.Strings(keys)
	return keys
}

// FindUnknownAgentKeys reads the top-level file of configPath (not the
// *.d fragments) and returns, sorted, the keys of the agent block no
// field reads: a misspelt key the loader drops without a word.
func FindUnknownAgentKeys(configPath string) ([]string, error) {
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
	var agent *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "agent" {
			agent = root.Content[i+1]
			break
		}
	}
	if agent == nil || agent.Kind != yaml.MappingNode {
		return nil, nil
	}
	known := map[string]bool{}
	for _, k := range KnownAgentKeys() {
		known[k] = true
	}
	var unknown []string
	for i := 0; i+1 < len(agent.Content); i += 2 {
		key := agent.Content[i].Value
		if key == "<<" || known[key] {
			continue
		}
		unknown = append(unknown, key)
	}
	sort.Strings(unknown)
	return unknown, nil
}
