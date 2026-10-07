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

// RangeDefinitions parses the embedded probe definitions one file at a
// time and hands each to fn, so a caller that only distils an index out of
// them never holds more than one parsed definition at once. Files that are
// not probe definitions (nagios.yaml, lookups.yaml, shared/) are skipped.
func RangeDefinitions(fn func(def ProbeDefinition)) error {
	entries, err := fs.ReadDir(definitionFiles, "definitions")
	if err != nil {
		return fmt.Errorf("listing embedded definitions: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := definitionFiles.ReadFile("definitions/" + entry.Name())
		if err != nil {
			return fmt.Errorf("reading embedded definition %s: %w", entry.Name(), err)
		}
		var def ProbeDefinition
		if err := yaml.Unmarshal(data, &def); err != nil {
			return fmt.Errorf("parsing embedded definition %s: %w", entry.Name(), err)
		}
		if def.ProbeName == "" || len(def.Metrics) == 0 {
			continue
		}
		fn(def)
	}
	return nil
}

// Definitions returns every probe definition shipped in the embedded
// definitions/ directory, keyed by probe name. It holds them all at once;
// a caller on the agent's running path uses RangeDefinitions instead.
func Definitions() (map[string]ProbeDefinition, error) {
	defs := make(map[string]ProbeDefinition)
	err := RangeDefinitions(func(def ProbeDefinition) {
		defs[def.ProbeName] = def
	})
	if err != nil {
		return nil, err
	}
	return defs, nil
}

// DefinitionMetrics returns, per probe definition shipped in the
// embedded definitions/ directory, the full metric definitions. Used
// by structural tests to verify cross-references and unit semantics
// across sink formats.
func DefinitionMetrics() (map[string][]MetricDefinition, error) {
	defs, err := Definitions()
	if err != nil {
		return nil, err
	}
	metrics := make(map[string][]MetricDefinition, len(defs))
	for probe, def := range defs {
		metrics[probe] = def.Metrics
	}
	return metrics, nil
}

// DefinitionMetricNames returns, per probe definition shipped in the
// embedded definitions/ directory, the set of raw metric names the
// probe emits (the `name:` field of each metric entry). Used by
// structural tests to verify cross-references such as Nagios check
// channels.
func DefinitionMetricNames() (map[string][]string, error) {
	metrics, err := DefinitionMetrics()
	if err != nil {
		return nil, err
	}
	names := make(map[string][]string, len(metrics))
	for probe, defs := range metrics {
		for _, m := range defs {
			if m.Name != "" {
				names[probe] = append(names[probe], m.Name)
			}
		}
	}
	return names, nil
}
