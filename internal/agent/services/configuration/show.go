package configuration

// show.go backs `agent config show`. It produces the merged
// configuration tree in three flavours:
//
//   ShowRaw      — exactly what's on disk after the merge, with
//                  ${env:..} / ${file:..} references PRESERVED.
//                  Useful for "is my YAML loaded the way I expect?"
//                  reviews and for diffing two layouts before going
//                  live.
//
//   ShowResolved — references resolved against the current
//                  environment / filesystem. This is what the agent
//                  actually sees at boot. Default mode.
//
//   ShowRedact   — resolved, but values that came from a ${file:..}
//                  reference AND values whose YAML field name
//                  matches (?i)(key|token|password|secret) are
//                  replaced with `***`. Safe to copy/paste into a
//                  support ticket or commit to source control.
//
// All three flavours marshal through yaml.v3 with map keys sorted
// alphabetically so two runs produce byte-identical output (modulo
// substituted environment values) — diffability is the whole point.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/services/configuration/secret"
	"senhub-agent.go/internal/agent/services/logger"
)

// ShowMode picks the rendering strategy. Zero value is ShowResolved
// so callers that forget to set it still get a sensible default.
type ShowMode int

const (
	ShowResolved ShowMode = iota
	ShowRaw
	ShowRedact
)

// secretFieldPattern matches the YAML field names whose VALUE should
// be masked in ShowRedact mode. The pattern uses substring matching
// (no `^…$` anchors) so composite names like `api_key`, `auth_token`,
// `db_password`, `client_secret` are all caught. Case-insensitive.
var secretFieldPattern = regexp.MustCompile(`(?i)(key|token|password|passphrase|secret|community|credential)`)

// fileRefPattern recognises a `${file:..}` reference inside a raw
// (pre-substitution) string. The trailing capture is intentionally
// permissive — the redact pass doesn't care what's inside, only
// whether a file-backed reference was there.
var fileRefPattern = regexp.MustCompile(`\$\{file:[^}]+\}`)

// secretRefPattern recognises a `${secret:..}` reference. Its resolved value is
// a plaintext secret, so a field carrying one is masked even when its NAME does
// not look like a secret.
var secretRefPattern = regexp.MustCompile(`\$\{secret:[^}]+\}`)

// LoadForShow returns the merged configuration tree for `agent config
// show` rendering. The mode parameter selects raw / resolved /
// redacted; see ShowMode constants for the contract of each.
//
// On error the path that broke is included in the message so the
// operator can edit the offending file without trial-and-error.
func LoadForShow(configPath string, mode ShowMode, log *logger.ModuleLogger) (LocalConfigurationData, error) {
	raw, err := loadMerged(configPath, log)
	if err != nil {
		return LocalConfigurationData{}, err
	}
	if mode == ShowRaw {
		return raw, nil
	}

	// Re-load to keep `raw` pristine for the redact mode; cheap I/O,
	// avoids a deep-copy routine over the LocalConfigurationData
	// shape (which has nested map[string]interface{} payloads).
	resolved, err := loadMerged(configPath, log)
	if err != nil {
		return LocalConfigurationData{}, err
	}
	if err := Substitute(&resolved); err != nil {
		return LocalConfigurationData{}, fmt.Errorf("substituting variables: %w", err)
	}

	if mode == ShowResolved {
		return resolved, nil
	}
	// ShowRedact: walk raw + resolved in lockstep, masking every
	// string that was either file-backed or sits under a secret-
	// named key.
	redactInPlace(reflect.ValueOf(&raw).Elem(), reflect.ValueOf(&resolved).Elem(), "")
	return resolved, nil
}

// MarshalSortedYAML serializes data to YAML with map keys sorted
// alphabetically. Used by every `agent config show` output mode so
// diffs are stable across runs.
//
// Implementation note: yaml.v3 emits struct fields in declaration
// order (already deterministic) but iterates Go maps in random order
// (Go runtime guarantee). We render into a yaml.Node tree, sort each
// MappingNode's key/value pairs, then marshal the node back to bytes.
func MarshalSortedYAML(data interface{}) ([]byte, error) {
	var node yaml.Node
	if err := node.Encode(data); err != nil {
		return nil, fmt.Errorf("encoding to YAML node: %w", err)
	}
	sortNode(&node)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		return nil, fmt.Errorf("encoding sorted node: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// sortNode walks a yaml.Node tree and sorts every MappingNode's
// key/value pairs alphabetically by key. Idempotent — calling twice
// is a no-op on the second pass.
func sortNode(n *yaml.Node) {
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			sortNode(c)
		}
	case yaml.MappingNode:
		// Content is [k0, v0, k1, v1, …]; sort pairs by key.Value
		// while preserving the (k, v) adjacency.
		pairs := make([][2]*yaml.Node, 0, len(n.Content)/2)
		for i := 0; i < len(n.Content); i += 2 {
			pairs = append(pairs, [2]*yaml.Node{n.Content[i], n.Content[i+1]})
		}
		sort.SliceStable(pairs, func(i, j int) bool {
			return pairs[i][0].Value < pairs[j][0].Value
		})
		n.Content = n.Content[:0]
		for _, p := range pairs {
			n.Content = append(n.Content, p[0], p[1])
			sortNode(p[1])
		}
	}
}

// loadMerged is LoadFromDisk minus the Substitute() call. We keep it
// unexported because the only legitimate non-substituted callers are
// the show paths in this file — boot must always substitute or it
// would ship `${env:DB_PASSWORD}` to the database.
func loadMerged(configPath string, log *logger.ModuleLogger) (LocalConfigurationData, error) {
	raw, err := os.ReadFile(configPath) // #nosec G304 - configPath is the operator-supplied --config-path
	if err != nil {
		return LocalConfigurationData{}, fmt.Errorf("reading %s: %w", configPath, err)
	}
	legacy, err := isLegacyMonolithic(raw)
	if err != nil {
		return LocalConfigurationData{}, fmt.Errorf("scanning %s for legacy markers: %w", configPath, err)
	}
	var data LocalConfigurationData
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return LocalConfigurationData{}, fmt.Errorf("parsing %s: %w", configPath, err)
	}

	// Record the config directory so a ${secret:} reference the show path
	// resolves can locate the OS-native secret backend — mirroring the boot
	// loader (LoadFromDisk). Without it, `config show --resolved/--redact` on a
	// SEALED install resolves secrets against an empty backend dir and crashes.
	// Recording the path carries no secret and initialises no backend.
	baseDir := filepath.Dir(configPath)
	secret.SetConfigDir(baseDir)

	if legacy {
		return data, nil
	}
	probes, err := loadProbesD(filepath.Join(baseDir, "probes.d"))
	if err != nil {
		return LocalConfigurationData{}, err
	}
	strategies, err := loadStrategiesD(filepath.Join(baseDir, "strategies.d"), log)
	if err != nil {
		return LocalConfigurationData{}, err
	}
	return mergeConfigs(data, probes, strategies), nil
}

// redactInPlace traverses the resolved tree and replaces leaf string
// values with "***" when either:
//
//   - the value came from a ${file:..} reference (detected by
//     looking at the matching position in the raw tree), OR
//   - the most recent named context (struct YAML tag or map key)
//     matches secretFieldPattern.
//
// Both trees must have the same shape (same struct types, parallel
// slice lengths). They will, because they were loaded from the same
// configPath in loadMerged.
func redactInPlace(rawV, resV reflect.Value, currentName string) {
	if !rawV.IsValid() || !resV.IsValid() {
		return
	}
	switch rawV.Kind() {
	case reflect.Ptr:
		if rawV.IsNil() || resV.IsNil() {
			return
		}
		redactInPlace(rawV.Elem(), resV.Elem(), currentName)

	case reflect.Interface:
		// An interface{} value can't be mutated via Elem() (the
		// inner payload is not addressable). Allocate a settable
		// holder of the concrete type, redact in it, and write it
		// back through the interface position.
		if rawV.IsNil() || resV.IsNil() {
			return
		}
		rawConcrete := rawV.Elem()
		resConcrete := resV.Elem()
		holder := reflect.New(resConcrete.Type()).Elem()
		holder.Set(resConcrete)
		redactInPlace(rawConcrete, holder, currentName)
		if resV.CanSet() {
			resV.Set(holder)
		}

	case reflect.Struct:
		for i := 0; i < rawV.NumField(); i++ {
			field := rawV.Type().Field(i)
			yamlName := yamlFieldName(field)
			redactInPlace(rawV.Field(i), resV.Field(i), yamlName)
		}

	case reflect.Map:
		// Walk keys in any order; map values aren't addressable but
		// can be replaced via SetMapIndex on resV.
		for _, key := range rawV.MapKeys() {
			rawVal := rawV.MapIndex(key)
			resVal := resV.MapIndex(key)
			if !resVal.IsValid() {
				continue
			}
			keyName := ""
			if key.Kind() == reflect.String {
				keyName = key.String()
			} else {
				keyName = fmt.Sprintf("%v", key.Interface())
			}
			tmp := reflect.New(resVal.Type()).Elem()
			tmp.Set(resVal)
			redactInPlace(rawVal, tmp, keyName)
			resV.SetMapIndex(key, tmp)
		}

	case reflect.Slice, reflect.Array:
		n := rawV.Len()
		if resV.Len() < n {
			n = resV.Len()
		}
		for i := 0; i < n; i++ {
			redactInPlace(rawV.Index(i), resV.Index(i), currentName)
		}

	case reflect.String:
		raw := rawV.String()
		// Two redact triggers (OR): the raw was file-backed, or the
		// containing field/key is named like a secret. We mask
		// regardless of whether the resolved value happens to be
		// empty — an empty secret is still a secret slot.
		hasFileRef := fileRefPattern.MatchString(raw)
		hasSecretRef := secretRefPattern.MatchString(raw)
		nameLooksSecret := currentName != "" && secretFieldPattern.MatchString(currentName)
		if !hasFileRef && !hasSecretRef && !nameLooksSecret {
			return
		}
		if !resV.CanSet() {
			return
		}
		resV.SetString("***")
	}
}

// yamlFieldName returns the yaml-tag name for a struct field, falling
// back to the lowercased field name (which is what yaml.v3 itself
// uses when no tag is present).
func yamlFieldName(f reflect.StructField) string {
	if tag := f.Tag.Get("yaml"); tag != "" && tag != "-" {
		// Split on `,` to drop options like `,omitempty`.
		if idx := strings.IndexByte(tag, ','); idx >= 0 {
			return tag[:idx]
		}
		return tag
	}
	// Fall back to JSON tag for structs imported from
	// remoteConfiguration.go which use json tags (StorageConfig,
	// ProbeConfig). yaml.v3 itself reads `yaml` first then no tag;
	// for redact purposes we still want json-tag-named fields like
	// `password` to be detected.
	if tag := f.Tag.Get("json"); tag != "" && tag != "-" {
		if idx := strings.IndexByte(tag, ','); idx >= 0 {
			return tag[:idx]
		}
		return tag
	}
	return strings.ToLower(f.Name)
}
