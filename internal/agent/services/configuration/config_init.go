package configuration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// ApplyInstallOverrides sets the operator-provided agent.license and
// agent.global_tags in a freshly generated agent.yaml, in place, preserving
// the template comments. It backs the `config init` install path: the default
// config is generated first (offline model — there is no cloud backend or token
// to seed), then the few provisionable fields are written on top.
//
// Empty inputs are a no-op. The edit is node-level so the extensive documentation
// comments the generator writes into agent.yaml survive.
func ApplyInstallOverrides(configPath, license string, tags map[string]string) error {
	if license == "" && len(tags) == 0 {
		return nil
	}
	raw, err := os.ReadFile(configPath) // #nosec G304 - operator-provided config path
	if err != nil {
		return fmt.Errorf("reading %s: %w", configPath, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parsing %s: %w", configPath, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s: unexpected top-level shape", configPath)
	}
	root := doc.Content[0]

	agent := mappingChild(root, "agent")
	if agent == nil {
		// The generated agent.yaml always carries an agent block; if it is
		// missing, add one rather than fail the install.
		agent = &yaml.Node{Kind: yaml.MappingNode}
		appendPair(root, "agent", agent)
	}
	if agent.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: agent block is not a mapping", configPath)
	}

	if license != "" {
		setScalarField(agent, "license", license)
	}
	if len(tags) > 0 {
		setTagsField(agent, "global_tags", tags)
	}

	out, err := marshalDocument(&doc)
	if err != nil {
		return fmt.Errorf("re-encoding %s: %w", configPath, err)
	}
	if err := atomicWriteFile(configPath, out, fileModeOr(configPath, 0o600)); err != nil {
		return fmt.Errorf("writing %s: %w", configPath, err)
	}
	return nil
}

// SetLicenseField writes agent.license in configPath in place, at the node
// level, preserving every other block and the template comments. Unlike a full
// unmarshal / re-marshal of LocalConfigurationData, it never re-emits empty
// top-level probes:/storage: sequences — which on a multi-file install flip
// isLegacyMonolithic (loader.go) back to true and make LoadFromDisk silently
// ignore probes.d/ + strategies.d/. An empty license clears the field (free
// tier). This backs `license activate` and `license remove`.
func SetLicenseField(configPath, license string) error {
	raw, err := os.ReadFile(configPath) // #nosec G304 - operator-provided config path
	if err != nil {
		return fmt.Errorf("reading %s: %w", configPath, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parsing %s: %w", configPath, err)
	}

	var root *yaml.Node
	switch {
	case len(doc.Content) == 0:
		// Empty or comment-only file: start a fresh mapping document.
		root = &yaml.Node{Kind: yaml.MappingNode}
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{root}
	case doc.Content[0].Kind == yaml.MappingNode:
		root = doc.Content[0]
	default:
		return fmt.Errorf("%s: unexpected top-level shape", configPath)
	}

	agent := mappingChild(root, "agent")
	if agent == nil {
		agent = &yaml.Node{Kind: yaml.MappingNode}
		appendPair(root, "agent", agent)
	}
	if agent.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: agent block is not a mapping", configPath)
	}

	setScalarField(agent, "license", license)

	out, err := marshalDocument(&doc)
	if err != nil {
		return fmt.Errorf("re-encoding %s: %w", configPath, err)
	}
	if err := atomicWriteFile(configPath, out, fileModeOr(configPath, 0o600)); err != nil {
		return fmt.Errorf("writing %s: %w", configPath, err)
	}
	return nil
}

// mappingChild returns the value node for key within a mapping node, or nil.
func mappingChild(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// appendPair adds a key/value pair to a mapping node.
func appendPair(m *yaml.Node, key string, value *yaml.Node) {
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}

// setScalarField sets (or adds) key=value as a string scalar under m.
func setScalarField(m *yaml.Node, key, value string) {
	if v := mappingChild(m, key); v != nil {
		v.Kind = yaml.ScalarNode
		v.Tag = "!!str"
		v.Value = value
		v.Content = nil
		return
	}
	appendPair(m, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

// setTagsField sets (or adds) key as a string→string mapping under m, entries
// sorted for a deterministic file.
func setTagsField(m *yaml.Node, key string, tags map[string]string) {
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	tagsNode := &yaml.Node{Kind: yaml.MappingNode}
	for _, k := range keys {
		tagsNode.Content = append(tagsNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: tags[k]},
		)
	}
	if v := mappingChild(m, key); v != nil {
		*v = *tagsNode
		return
	}
	appendPair(m, key, tagsNode)
}

// WriteOTLPStrategyFragment writes strategies.d/10-otlp.yaml pointing at the
// given collector endpoint, so an unattended install can opt into OTLP push
// (metrics/logs/entities) without hand-editing YAML. No-op for an empty
// endpoint; it never overwrites an existing 10-otlp.yaml (config init only runs
// on a fresh install, but this keeps it safe if reused). protocol defaults to
// grpc — the operator can tune the rest of the strategy afterwards.
func WriteOTLPStrategyFragment(configDir, endpoint string) error {
	if endpoint == "" {
		return nil
	}
	dir := filepath.Join(configDir, "strategies.d")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	path := filepath.Join(dir, "10-otlp.yaml")
	if _, err := os.Stat(path); err == nil {
		return nil // already present, leave operator's fragment untouched
	}
	body := "# SenHub Agent — OTLP export strategy (provisioned by 'config init').\n" +
		"# Pushes metrics, logs and entities to an OpenTelemetry collector.\n" +
		"otlp:\n" +
		"  endpoint: " + endpoint + "\n" +
		"  protocol: grpc\n"
	if err := atomicWriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func marshalDocument(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
