package transformers

import (
	"fmt"
	"io/fs"
	"strings"

	"gopkg.in/yaml.v2"
)

// DefaultNagiosConfigYAML returns the raw bytes of the curated Nagios
// checks configuration shipped with the agent
// (definitions/nagios.yaml). The HTTP strategy serves it as the
// out-of-the-box Nagios config when the operator provides none.
func DefaultNagiosConfigYAML() ([]byte, error) {
	return definitionFiles.ReadFile("definitions/nagios.yaml")
}

// DefinitionMetricNames returns, per probe definition shipped in the
// embedded definitions/ directory, the set of raw metric names the
// probe emits (the `name:` field of each metric entry). Files that are
// not probe definitions (nagios.yaml, lookups.yaml, shared/) are
// skipped. Used by structural tests to verify cross-references such as
// Nagios check channels.
func DefinitionMetricNames() (map[string][]string, error) {
	entries, err := fs.ReadDir(definitionFiles, "definitions")
	if err != nil {
		return nil, fmt.Errorf("listing embedded definitions: %w", err)
	}

	names := make(map[string][]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := definitionFiles.ReadFile("definitions/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("reading embedded definition %s: %w", entry.Name(), err)
		}
		var def ProbeDefinition
		if err := yaml.Unmarshal(data, &def); err != nil {
			return nil, fmt.Errorf("parsing embedded definition %s: %w", entry.Name(), err)
		}
		if def.ProbeName == "" || len(def.Metrics) == 0 {
			continue
		}
		for _, m := range def.Metrics {
			if m.Name != "" {
				names[def.ProbeName] = append(names[def.ProbeName], m.Name)
			}
		}
	}
	return names, nil
}
