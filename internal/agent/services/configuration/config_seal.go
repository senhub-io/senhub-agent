package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/services/configuration/secret"
	"senhub-agent.go/internal/agent/services/logger"
)

// SealInlineSecrets implements the default "seal" policy: it finds plaintext
// secrets in the config, moves each into the OS-native store, and rewrites the
// field in place to a ${secret:<key>} reference. It is idempotent and a no-op
// when the config has no inline secret (no backup, no rewrite). It is
// layout-aware: it seals a monolithic file's probes, or every fragment under
// probes.d/ and strategies.d/.
//
// Safety net: each edited file is backed up 0600 BEFORE any change; edits are
// yaml.v3 node-level value replacements that preserve comments and order; after
// all files are written the WHOLE config is re-loaded with resolution and
// compared to the pre-seal load — the resolved ${secret:} values must equal the
// original plaintext. Any mismatch (or a load error) restores every backup and
// returns an error, so a faulty rewrite can never be left in place.
func SealInlineSecrets(configPath string, log *logger.ModuleLogger) error {
	baseDir := filepath.Dir(configPath)
	secret.SetConfigDir(baseDir)

	// Pre-seal snapshot (plaintext, no ${secret:} yet).
	before, err := LoadFromDisk(configPath, nil)
	if err != nil {
		return fmt.Errorf("loading config before seal: %w", err)
	}

	targets, err := sealTargets(configPath, baseDir)
	if err != nil {
		return err
	}

	var prov secret.Provider
	type backup struct{ path, backupPath string }
	var backups []backup
	total := 0

	restore := func() {
		for _, b := range backups {
			if data, e := os.ReadFile(b.backupPath); e == nil {
				_ = os.WriteFile(b.path, data, 0o600)
			}
		}
	}

	for _, path := range targets {
		n, backupPath, err := sealOneFile(path, &prov)
		if backupPath != "" {
			backups = append(backups, backup{path, backupPath})
		}
		if err != nil {
			restore()
			return fmt.Errorf("sealing %s: %w", path, err)
		}
		total += n
	}

	if total == 0 {
		return nil
	}

	// Verify: the resolved config must be identical to the pre-seal one.
	after, err := LoadFromDisk(configPath, nil)
	if err != nil {
		restore()
		return fmt.Errorf("seal verification: reloading sealed config failed (backups restored): %w", err)
	}
	if !reflect.DeepEqual(before, after) {
		restore()
		return fmt.Errorf("seal verification: resolved config differs from the original (backups restored)")
	}

	// Stamp the config as v3 now that secrets are referenced, so an older agent
	// refuses it rather than passing an unresolved ${secret:} literal to a probe.
	// Done AFTER the value-preserving verify so it cannot perturb that compare.
	if err := setRootConfigVersion(configPath, CurrentConfigVersion); err != nil && log != nil {
		log.Warn().Err(err).Msg("Sealed secrets but failed to stamp config_version")
	}

	if log != nil {
		name := "unknown"
		if prov != nil {
			name = prov.Name()
		}
		log.Info().Int("sealed", total).Str("backend", name).Msg("Sealed inline secrets into the OS-native store")
	}
	return nil
}

// sealTargets lists the files to scan, by layout.
func sealTargets(configPath, baseDir string) ([]string, error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", configPath, err)
	}
	legacy, err := isLegacyMonolithic(raw)
	if err != nil {
		return nil, err
	}
	if legacy {
		return []string{configPath}, nil
	}
	var out []string
	for _, dir := range []string{filepath.Join(baseDir, "probes.d"), filepath.Join(baseDir, "strategies.d")} {
		files, err := listYAMLFiles(dir)
		if err != nil {
			return nil, err
		}
		out = append(out, files...)
	}
	return out, nil
}

// sealOneFile parses one file, seals its inline secrets in place, and (only when
// something was sealed) backs the file up and writes it. Returns the count and
// the backup path (empty when nothing changed).
func sealOneFile(path string, prov *secret.Provider) (int, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, "", err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return 0, "", fmt.Errorf("parsing YAML: %w", err)
	}
	if len(doc.Content) == 0 {
		return 0, "", nil
	}

	sealed, err := sealDocument(&doc, prov)
	if err != nil {
		return sealed, "", err
	}
	if sealed == 0 {
		return 0, "", nil
	}

	// Backup BEFORE writing.
	backupPath := fmt.Sprintf("%s.backup.%s", path, time.Now().Format("20060102-150405"))
	if err := os.WriteFile(backupPath, data, 0o600); err != nil {
		return sealed, "", fmt.Errorf("writing backup: %w", err)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return sealed, backupPath, fmt.Errorf("marshalling sealed YAML: %w", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return sealed, backupPath, fmt.Errorf("writing sealed file: %w", err)
	}
	return sealed, backupPath, nil
}

// sealDocument routes by layout: a probes.d sequence, a monolithic map (with a
// "probes" sequence), or a strategies.d single-strategy map.
func sealDocument(doc *yaml.Node, prov *secret.Provider) (int, error) {
	root := doc.Content[0]
	switch root.Kind {
	case yaml.SequenceNode:
		// probes.d fragment: an array of probe maps.
		return sealProbeSeq(root, prov)
	case yaml.MappingNode:
		if probesVal := mapValue(root, "probes"); probesVal != nil && probesVal.Kind == yaml.SequenceNode {
			// Monolithic: probes (+ storage handled best-effort below).
			n, err := sealProbeSeq(probesVal, prov)
			if err != nil {
				return n, err
			}
			if storage := mapValue(root, "storage"); storage != nil && storage.Kind == yaml.MappingNode {
				m, err := sealStrategyMap(storage, prov)
				n += m
				if err != nil {
					return n, err
				}
			}
			return n, nil
		}
		// strategies.d fragment: {strategyName: params}.
		return sealStrategyMap(root, prov)
	}
	return 0, nil
}

func sealProbeSeq(seq *yaml.Node, prov *secret.Provider) (int, error) {
	total := 0
	for _, probe := range seq.Content {
		if probe.Kind != yaml.MappingNode {
			continue
		}
		instance := scalarValue(mapValue(probe, "name"))
		if instance == "" {
			instance = scalarValue(mapValue(probe, "type"))
		}
		params := mapValue(probe, "params")
		if params == nil || params.Kind != yaml.MappingNode {
			continue
		}
		n, err := sealMapping(instance, nil, params, prov)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func sealStrategyMap(m *yaml.Node, prov *secret.Provider) (int, error) {
	total := 0
	for i := 0; i+1 < len(m.Content); i += 2 {
		instance := m.Content[i].Value
		val := m.Content[i+1]
		if val.Kind != yaml.MappingNode {
			continue
		}
		// A strategy fragment may nest params under "params" or carry them flat.
		params := val
		if p := mapValue(val, "params"); p != nil && p.Kind == yaml.MappingNode {
			params = p
		}
		n, err := sealMapping(instance, nil, params, prov)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// sealMapping recursively seals sensitive plaintext scalars in a mapping node.
func sealMapping(instance string, path []string, node *yaml.Node, prov *secret.Provider) (int, error) {
	if node.Kind != yaml.MappingNode {
		return 0, nil
	}
	total := 0
	for i := 0; i+1 < len(node.Content); i += 2 {
		k := node.Content[i].Value
		v := node.Content[i+1]
		child := append(append([]string(nil), path...), k)
		switch v.Kind {
		case yaml.ScalarNode:
			if secret.IsSensitiveKey(k) && isPlaintextScalar(v.Value) {
				key := secret.SanitizeKey(instance + "." + strings.Join(child, "."))
				p, err := ensureSealProvider(prov)
				if err != nil {
					return total, err
				}
				if err := p.Set(key, secret.New(v.Value)); err != nil {
					return total, fmt.Errorf("storing secret %q: %w", key, err)
				}
				v.SetString("${secret:" + key + "}")
				total++
			}
		case yaml.MappingNode:
			n, err := sealMapping(instance, child, v, prov)
			total += n
			if err != nil {
				return total, err
			}
		case yaml.SequenceNode:
			for idx, item := range v.Content {
				if item.Kind == yaml.MappingNode {
					n, err := sealMapping(instance, append(append([]string(nil), child...), strconv.Itoa(idx)), item, prov)
					total += n
					if err != nil {
						return total, err
					}
				}
			}
		}
	}
	return total, nil
}

func ensureSealProvider(prov *secret.Provider) (secret.Provider, error) {
	if *prov != nil {
		return *prov, nil
	}
	p, err := secret.Backend()
	if err != nil {
		return nil, err
	}
	*prov = p
	return p, nil
}

// mapValue returns the value node for key in a mapping node, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
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

func scalarValue(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func isPlaintextScalar(v string) bool {
	t := strings.TrimSpace(v)
	return t != "" && !strings.HasPrefix(t, "${")
}

// setRootConfigVersion sets (or adds) the top-level config_version scalar in the
// main config file to v, preserving the rest of the file (yaml.v3 node edit).
func setRootConfigVersion(path string, v int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil // not a mapping root: nothing to stamp
	}
	root := doc.Content[0]
	if cv := mapValue(root, "config_version"); cv != nil {
		cv.Kind = yaml.ScalarNode
		cv.Tag = "!!int"
		cv.Style = 0
		cv.Value = strconv.Itoa(v)
	} else {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "config_version"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(v)},
		)
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}
