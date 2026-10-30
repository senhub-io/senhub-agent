package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// UnknownProbeKey is a key of a probe entry, outside params, that no
// field of ProbeConfig reads.
type UnknownProbeKey struct {
	File  string
	Probe string
	Key   string
}

// KnownProbeKeys lists the keys a probe entry accepts at its own level,
// taken from the yaml tags of ProbeConfig so a new field is known as soon
// as it exists.
func KnownProbeKeys() []string {
	return yamlKeysOf(reflect.TypeOf(ProbeConfig{}))
}

func yamlKeysOf(t reflect.Type) []string {
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("yaml"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = strings.ToLower(t.Field(i).Name)
		}
		keys = append(keys, name)
	}
	sort.Strings(keys)
	return keys
}

// FindUnknownProbeKeys reads the probe entries the loader reads for
// configPath (the probes: list of the file, or the probes.d fragments of
// a multi-file layout) and returns the keys of an entry that the decoder
// drops without a word.
func FindUnknownProbeKeys(configPath string) ([]UnknownProbeKey, error) {
	raw, err := os.ReadFile(configPath) // #nosec G304 - configPath comes from --config-path
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", configPath, err)
	}
	legacy, err := isLegacyMonolithic(raw)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, k := range KnownProbeKeys() {
		known[k] = true
	}
	if legacy {
		var doc struct {
			Probes []yaml.Node `yaml:"probes"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, err
		}
		return scanProbeNodes(configPath, doc.Probes, known), nil
	}
	files, err := listYAMLFiles(filepath.Join(filepath.Dir(configPath), "probes.d"))
	if err != nil {
		return nil, err
	}
	var found []UnknownProbeKey
	for _, path := range files {
		fragment, err := os.ReadFile(path) // #nosec G304 - path is under the configured probes.d/ directory
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		var nodes []yaml.Node
		if err := yaml.Unmarshal(fragment, &nodes); err != nil {
			return nil, newParseError(path, err)
		}
		found = append(found, scanProbeNodes(path, nodes, known)...)
	}
	return found, nil
}

func scanProbeNodes(file string, nodes []yaml.Node, known map[string]bool) []UnknownProbeKey {
	var found []UnknownProbeKey
	for i := range nodes {
		n := &nodes[i]
		if n.Kind != yaml.MappingNode {
			continue
		}
		name := ""
		var unknown []string
		for j := 0; j+1 < len(n.Content); j += 2 {
			key := n.Content[j].Value
			if key == "name" {
				name = n.Content[j+1].Value
			}
			if !known[key] && key != "<<" {
				unknown = append(unknown, key)
			}
		}
		for _, key := range unknown {
			found = append(found, UnknownProbeKey{File: file, Probe: name, Key: key})
		}
	}
	return found
}
