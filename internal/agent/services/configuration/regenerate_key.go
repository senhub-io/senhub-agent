package configuration

import (
	"crypto/rand"
	"fmt"

	"gopkg.in/yaml.v3"
)

// NewAgentKey mints a new agent key: a random UUID v4, the entropy level the
// instance id derivation and the PRTG URLs assume.
func NewAgentKey() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating the agent key: %w", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}

// ReplaceAgentKey overwrites agent.key in the main configuration file with
// newKey, in plaintext and double quotes, whether the previous value was
// plaintext or a ${secret:...} reference. The next start seals the new value
// and overwrites the old secret, because sealing writes through Set on the
// same vault key. Comments and key order are preserved by the node-based
// rewrite.
func ReplaceAgentKey(configPath, newKey string) error {
	_, err := rewriteFile(configPath, func(raw []byte) ([]byte, error) {
		var doc yaml.Node
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", configPath, err)
		}
		root := documentRoot(&doc)
		agent := mappingValue(root, "agent")
		if agent == nil || agent.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s has no agent block", configPath)
		}
		node := mappingValue(agent, "key")
		if node == nil {
			return nil, fmt.Errorf("%s has no agent.key", configPath)
		}
		setScalarString(node, newKey)
		out, err := yaml.Marshal(&doc)
		if err != nil {
			return nil, fmt.Errorf("rendering %s with the new agent key: %w", configPath, err)
		}
		return out, nil
	})
	if err != nil {
		return fmt.Errorf("replacing agent.key in %s: %w", configPath, err)
	}
	return nil
}

// ReplaceAdminKey overwrites admin_key on the http output with newKey, in
// plaintext and double quotes, wherever that output lives (main file or
// strategies.d fragment).
func ReplaceAdminKey(configPath, newKey string) error {
	path, _, _, err := findHTTPOutput(configPath)
	if err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("no http output found next to %s", configPath)
	}
	_, err = rewriteFile(path, func(raw []byte) ([]byte, error) {
		var doc yaml.Node
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		node := mappingValue(httpMappingIn(&doc), "admin_key")
		if node == nil {
			return nil, fmt.Errorf("%s has no admin_key on its http output", path)
		}
		setScalarString(node, newKey)
		out, err := yaml.Marshal(&doc)
		if err != nil {
			return nil, fmt.Errorf("rendering %s with the new administration key: %w", path, err)
		}
		return out, nil
	})
	if err != nil {
		return fmt.Errorf("replacing admin_key in %s: %w", path, err)
	}
	return nil
}

// documentRoot returns the root node of a parsed document, nil for an empty
// file.
func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc != nil && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return nil
}

// setScalarString turns node into a double-quoted string scalar, whatever it
// held before (plaintext or ${secret:...} reference).
func setScalarString(node *yaml.Node, value string) {
	node.Kind = yaml.ScalarNode
	node.Tag = "!!str"
	node.Value = value
	node.Style = yaml.DoubleQuotedStyle
}
