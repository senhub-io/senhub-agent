package configuration

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ReadAgentGovernance returns the top-level governance block of the main
// configuration file exactly as written, before any ${env:} or ${file:}
// substitution, so a page can show and edit what the file says and not
// what a variable happened to hold. Absent block: nil.
func ReadAgentGovernance(configPath string) (map[string]interface{}, error) {
	raw, err := os.ReadFile(configPath) // #nosec G304 - operator-provided config path
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", configPath, err)
	}
	var doc struct {
		Governance map[string]interface{} `yaml:"governance"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", configPath, err)
	}
	return doc.Governance, nil
}

// GovernanceReferences lists the dotted paths of the values of a
// governance block that are references (${env:}, ${file:}, ${secret:})
// instead of literals. A page must not rewrite those: saving the
// resolved value would replace the reference with it.
func GovernanceReferences(block map[string]interface{}) []string {
	var out []string
	var walk func(prefix string, v interface{})
	walk = func(prefix string, v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			for k, c := range t {
				walk(prefix+"."+k, c)
			}
		case string:
			if strings.Contains(t, "${") {
				out = append(out, strings.TrimPrefix(prefix, "."))
			}
		}
	}
	walk("", block)
	sort.Strings(out)
	return out
}

// SetAgentGovernance replaces the top-level governance block of the main
// configuration file, at the node level so comments, key order and the
// other blocks stay as they were and an empty probes: or storage: is
// never introduced (which would flip a multi-file install to the legacy
// layout). An empty block removes the key. The write is atomic and the
// running agent follows it through the configuration watcher.
func SetAgentGovernance(configPath string, block map[string]interface{}) error {
	_, err := rewriteFile(configPath, func(raw []byte) ([]byte, error) {
		var doc yaml.Node
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", configPath, err)
		}
		if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s: unexpected top-level shape", configPath)
		}
		root := doc.Content[0]

		idx := -1
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value == "governance" {
				idx = i
				break
			}
		}
		if len(block) == 0 {
			if idx < 0 {
				return nil, nil
			}
			root.Content = append(root.Content[:idx], root.Content[idx+2:]...)
		} else {
			var node yaml.Node
			if err := node.Encode(block); err != nil {
				return nil, fmt.Errorf("encoding governance: %w", err)
			}
			if idx >= 0 {
				node.HeadComment = root.Content[idx+1].HeadComment
				node.LineComment = root.Content[idx+1].LineComment
				root.Content[idx+1] = &node
			} else {
				appendPair(root, "governance", &node)
			}
		}
		out, err := marshalDocument(&doc)
		if err != nil {
			return nil, fmt.Errorf("re-encoding %s: %w", configPath, err)
		}
		return out, nil
	})
	if err != nil {
		return fmt.Errorf("setting governance in %s: %w", configPath, err)
	}
	return nil
}
