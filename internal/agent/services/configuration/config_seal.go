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

// sealState threads the write provider and a collision guard through one seal
// pass (which spans every fragment file). seen maps a derived store key to the
// first plaintext sealed under it and the field that produced it, so a second
// field deriving the same key with a DIFFERENT value is reported as an explicit,
// actionable error instead of silently overwriting the first — the overwrite
// would otherwise pass unnoticed here and only surface as an opaque
// "resolved config differs" verify failure, retried on every boot.
type sealState struct {
	prov secret.Provider
	seen map[string]sealSeen
}

type sealSeen struct {
	value string
	where string
}

func newSealState() *sealState { return &sealState{seen: map[string]sealSeen{}} }

// provider lazily resolves the write backend on first use, so a config with no
// inline secret never initialises a store.
func (st *sealState) provider() (secret.Provider, error) {
	if st.prov != nil {
		return st.prov, nil
	}
	p, err := secret.Backend()
	if err != nil {
		return nil, err
	}
	st.prov = p
	return p, nil
}

// recordKey stores value under key, guarding against a collision (same derived
// key, different value). Returns the reference string to write into the config.
func (st *sealState) sealValue(key, value, where string) (string, error) {
	if prev, ok := st.seen[key]; ok && prev.value != value {
		return "", fmt.Errorf(
			"secret key collision: %s and %s both derive store key %q but hold different values; "+
				"give the probes/strategies distinct names so their secrets do not overwrite one another",
			prev.where, where, key)
	}
	p, err := st.provider()
	if err != nil {
		return "", err
	}
	if err := p.Set(key, secret.New(value)); err != nil {
		return "", fmt.Errorf("storing secret %q: %w", key, err)
	}
	st.seen[key] = sealSeen{value: value, where: where}
	return "${secret:" + key + "}", nil
}

// SealInlineSecrets implements the default "seal" policy: it finds plaintext
// secrets in the config, moves each into the OS-native store, and rewrites the
// field in place to a ${secret:<key>} reference. It is idempotent and a no-op
// when the config has no inline secret (no backup, no rewrite).
//
// It first HARMONISES the layout: a legacy monolithic config is split into the
// multi-file shape (agent.yaml + probes.d/ + strategies.d/) before sealing, so
// the whole fleet converges on one layout and inline secrets only ever live in
// fragments (plus the agent block in agent.yaml). The split carries its own
// backup + data-equality verification and is a no-op once the config is already
// multi-file. After it, every fragment under probes.d/ and strategies.d/ is
// sealed.
//
// Safety net: each edited file is backed up 0600 BEFORE any change; edits are
// yaml.v3 node-level value replacements that preserve comments and order; every
// rewrite is atomic (temp+fsync+rename); after all files are written the WHOLE
// config is re-loaded with resolution and compared to the pre-seal load — the
// resolved ${secret:} values must equal the original plaintext. Any mismatch
// (or a load error) restores every backup and returns an error, so a faulty
// rewrite can never be left in place.
func SealInlineSecrets(configPath string, log *logger.ModuleLogger) error {
	baseDir := filepath.Dir(configPath)
	secret.SetConfigDir(baseDir)

	// Version gate FIRST: never touch a config newer than this agent
	// understands. Sealing rewrites (and would stamp config_version down to 3),
	// which defeats the "refuse a newer config" load-time gate and silently
	// downgrades a rolled-back install. A read/parse error here is not fatal —
	// the seal's own load below surfaces it.
	if v, err := readRootConfigVersion(configPath); err == nil && v > CurrentConfigVersion {
		if log != nil {
			log.Warn().Int("config_version", v).Int("supported", CurrentConfigVersion).
				Msg("Config version is newer than this agent supports; skipping seal to avoid downgrading it")
		}
		return nil
	}

	// Harmonise first: convert a legacy monolithic config to the multi-file
	// layout so secrets are sealed into fragments, not a single file. The split
	// engine carries its own backup + data-equality verification; it is a no-op
	// once the config is already multi-file.
	harmoniseBackup := ""
	if raw, rerr := os.ReadFile(configPath); rerr == nil && HasMonolithicMarkers(raw) {
		res, merr := MigrateToMultiFile(configPath, log)
		if merr != nil {
			return fmt.Errorf("harmonising config to multi-file before seal: %w", merr)
		}
		harmoniseBackup = res.BackupPath
		if !res.AlreadyMultiFile && log != nil {
			log.Info().Str("backup", res.BackupPath).Int("strategies", res.StrategyCount).
				Msg("Harmonised monolithic config to multi-file layout before sealing")
		}
	}

	// Pre-seal snapshot (plaintext, no ${secret:} yet).
	before, err := LoadFromDisk(configPath, nil)
	if err != nil {
		return fmt.Errorf("loading config before seal: %w", err)
	}

	targets, err := sealTargets(configPath, baseDir)
	if err != nil {
		return err
	}

	st := newSealState()
	type backup struct{ path, backupPath string }
	var backups []backup
	total := 0

	// restore reverts every rewritten file from its pre-change backup, atomically
	// and preserving the original mode. It returns a human-readable failure per
	// file it could NOT restore (so the caller never claims "backups restored"
	// when it didn't). On a successful restore it removes the now-redundant
	// plaintext backup so a config that fails to seal on every boot does not
	// accumulate copies.
	restore := func() []string {
		var failures []string
		for _, b := range backups {
			data, e := os.ReadFile(b.backupPath)
			if e != nil {
				failures = append(failures, fmt.Sprintf("%s (reading backup %s: %v)", b.path, b.backupPath, e))
				continue
			}
			if e := atomicWriteFile(b.path, data, fileModeOr(b.path, 0o600)); e != nil {
				failures = append(failures, fmt.Sprintf("%s (restoring: %v)", b.path, e))
				continue
			}
			_ = os.Remove(b.backupPath)
		}
		return failures
	}

	// finalizeRestore rolls everything back and builds the returned error. It
	// only claims "backups restored" when every backup wrote back; otherwise it
	// names the files left half-sealed and logs at Error.
	finalizeRestore := func(base error) error {
		failures := restore()
		if len(failures) == 0 {
			return fmt.Errorf("%w (backups restored)", base)
		}
		if log != nil {
			log.Error().Strs("restore_failures", failures).
				Msg("Seal failed AND restoring backups failed; config may be left half-sealed")
		}
		return fmt.Errorf("%w; restoring backups ALSO failed, config may be left half-sealed: %s",
			base, strings.Join(failures, "; "))
	}

	for _, path := range targets {
		n, backupPath, err := sealOneFile(path, st)
		if backupPath != "" {
			backups = append(backups, backup{path, backupPath})
		}
		if err != nil {
			return finalizeRestore(fmt.Errorf("sealing %s: %w", path, err))
		}
		total += n
	}

	// The agent's own auth key lives in the main config file's agent block (not
	// in a probe), so seal it separately. The license is left in clear — it is a
	// JWT bound to the agent key, not a portable access secret — and its
	// on-disk home is the license.jwt sidecar (see config_license.go), not this
	// file, so there is nothing here to seal.
	n, backupPath, err := sealAgentKeyInFile(configPath, st)
	if backupPath != "" {
		backups = append(backups, backup{configPath, backupPath})
	}
	if err != nil {
		return finalizeRestore(fmt.Errorf("sealing agent.key: %w", err))
	}
	total += n

	if total == 0 {
		// Harmonisation may still have rewritten files as root; align ownership
		// with the config dir so a non-root service user can read them.
		reownAfterSeal(baseDir, log)
		return nil
	}

	// Verify: the resolved config must be identical to the pre-seal one.
	after, err := LoadFromDisk(configPath, nil)
	if err != nil {
		return finalizeRestore(fmt.Errorf("seal verification: reloading sealed config failed: %w", err))
	}
	if !reflect.DeepEqual(before, after) {
		return finalizeRestore(fmt.Errorf("seal verification: resolved config differs from the original"))
	}

	// Stamp the config as v3 now that secrets are referenced, so an older agent
	// refuses it rather than passing an unresolved ${secret:} literal to a probe.
	// Done AFTER the value-preserving verify so it cannot perturb that compare. A
	// stamp failure is FATAL: leaving ${secret:} references under a v2 label would
	// let a ≤0.4.x agent load it and hand the literal to a probe — exactly what
	// v3 exists to block — so we roll the whole seal back instead.
	if err := setRootConfigVersion(configPath, CurrentConfigVersion); err != nil {
		return finalizeRestore(fmt.Errorf("stamping config_version after seal: %w", err))
	}

	// Success: the pre-change backups are no longer a safety net but a plaintext
	// residue (they hold the secrets we just sealed). Scrub them so no password
	// survives in the config directory — the whole point of sealing. Only done
	// AFTER the verify passed, never before. The standalone `config migrate` CLI
	// keeps its own backup; here we own the harmonise backup because the seal
	// flow guarantees no-plaintext.
	for _, b := range backups {
		if err := os.Remove(b.backupPath); err != nil && !os.IsNotExist(err) && log != nil {
			log.Warn().Err(err).Str("file", b.backupPath).Msg("Could not remove seal backup (plaintext may linger)")
		}
	}
	if harmoniseBackup != "" {
		if err := os.Remove(harmoniseBackup); err != nil && !os.IsNotExist(err) && log != nil {
			log.Warn().Err(err).Str("file", harmoniseBackup).Msg("Could not remove harmonise backup (plaintext may linger)")
		}
	}

	// Align ownership: a privileged seal writes the store, the key and the
	// rewritten config files as root; chown them to match the config dir so the
	// non-root service daemon can read them at runtime.
	reownAfterSeal(baseDir, log)

	if log != nil {
		name := "unknown"
		if st.prov != nil {
			name = st.prov.Name()
		}
		log.Info().Int("sealed", total).Str("backend", name).Msg("Sealed inline secrets into the OS-native store")
	}
	return nil
}

// readRootConfigVersion returns the top-level config_version scalar (0 when the
// field is absent). Used to gate the seal against a config newer than this agent.
func readRootConfigVersion(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var doc struct {
		ConfigVersion int `yaml:"config_version"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return 0, err
	}
	return doc.ConfigVersion, nil
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
func sealOneFile(path string, st *sealState) (int, string, error) {
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

	sealed, err := sealDocument(&doc, st)
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

	out, err := marshalNode(&doc)
	if err != nil {
		return sealed, backupPath, fmt.Errorf("marshalling sealed YAML: %w", err)
	}
	if err := atomicWriteFile(path, out, fileModeOr(path, 0o600)); err != nil {
		return sealed, backupPath, fmt.Errorf("writing sealed file: %w", err)
	}
	return sealed, backupPath, nil
}

// sealDocument routes by layout: a probes.d sequence, a monolithic map (with a
// "probes" and/or "storage" block), or a strategies.d single-strategy map.
func sealDocument(doc *yaml.Node, st *sealState) (int, error) {
	root := doc.Content[0]
	switch root.Kind {
	case yaml.SequenceNode:
		// probes.d fragment: an array of probe maps.
		return sealProbeSeq(root, st)
	case yaml.MappingNode:
		probesVal := mapValue(root, "probes")
		storage := mapValue(root, "storage")
		hasProbes := probesVal != nil && probesVal.Kind == yaml.SequenceNode
		// Monolithic when either block is present. Route storage independently of
		// probes so a monolithic file carrying only `storage:` is still sealed
		// (previously it fell through to the strategies.d handler, which iterates
		// top-level keys and skips the storage sequence, leaving its secrets
		// plaintext).
		if hasProbes || storage != nil {
			total := 0
			if hasProbes {
				n, err := sealProbeSeq(probesVal, st)
				total += n
				if err != nil {
					return total, err
				}
			}
			if storage != nil {
				var m int
				var err error
				switch storage.Kind {
				case yaml.SequenceNode:
					// Monolithic storage is a SEQUENCE of {name, params} (like probes).
					m, err = sealProbeSeq(storage, st)
				case yaml.MappingNode:
					m, err = sealStrategyMap(storage, st)
				}
				total += m
				if err != nil {
					return total, err
				}
			}
			return total, nil
		}
		// strategies.d fragment: {strategyName: params}.
		return sealStrategyMap(root, st)
	}
	return 0, nil
}

func sealProbeSeq(seq *yaml.Node, st *sealState) (int, error) {
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
		n, err := sealMapping(instance, nil, params, st)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func sealStrategyMap(m *yaml.Node, st *sealState) (int, error) {
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
		n, err := sealMapping(instance, nil, params, st)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// sealMapping recursively seals sensitive plaintext scalars in a mapping node.
func sealMapping(instance string, path []string, node *yaml.Node, st *sealState) (int, error) {
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
				where := instance + "." + strings.Join(child, ".")
				key := secret.SanitizeKey(where)
				ref, err := st.sealValue(key, v.Value, where)
				if err != nil {
					return total, err
				}
				v.SetString(ref)
				total++
			}
		case yaml.MappingNode:
			n, err := sealMapping(instance, child, v, st)
			total += n
			if err != nil {
				return total, err
			}
		case yaml.SequenceNode:
			for idx, item := range v.Content {
				if item.Kind == yaml.MappingNode {
					n, err := sealMapping(instance, append(append([]string(nil), child...), strconv.Itoa(idx)), item, st)
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

// isPlaintextScalar reports whether a scalar is a real inline secret to seal:
// non-empty and containing NO ${...} reference. A value with even a mid-string
// reference (e.g. "pre${env:X}post") is skipped — the pre-seal Substitute
// expands it, so sealing the raw form would make the post-seal verify fail (the
// stored raw string is not re-expanded on resolve) and retry the seal forever.
func isPlaintextScalar(v string) bool {
	t := strings.TrimSpace(v)
	return t != "" && !strings.Contains(t, "${")
}

// marshalNode renders an edited yaml.v3 node tree with a 2-space indent (the
// common style for the agent's configs), so the seal rewrite does not reflow the
// operator's indentation. Comments and key order are preserved by the node tree.
func marshalNode(doc *yaml.Node) ([]byte, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// sealAgentKeyInFile seals the agent.key (when it is an inline plaintext value)
// in the main config file, keyed as "agent.key", and rewrites it to
// ${secret:agent.key}. The agent.license field is deliberately left untouched:
// the license lives in clear in the license.jwt sidecar (config_license.go),
// not in this file. Returns the count (0 or 1) and the backup path.
func sealAgentKeyInFile(path string, st *sealState) (int, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, "", err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return 0, "", fmt.Errorf("parsing YAML: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return 0, "", nil
	}
	agent := mapValue(doc.Content[0], "agent")
	if agent == nil || agent.Kind != yaml.MappingNode {
		return 0, "", nil
	}
	keyNode := mapValue(agent, "key")
	if keyNode == nil || keyNode.Kind != yaml.ScalarNode || !isPlaintextScalar(keyNode.Value) {
		return 0, "", nil
	}
	ref, err := st.sealValue("agent.key", keyNode.Value, "agent.key")
	if err != nil {
		return 0, "", err
	}
	backupPath := fmt.Sprintf("%s.backup.%s", path, time.Now().Format("20060102-150405"))
	if err := os.WriteFile(backupPath, data, 0o600); err != nil {
		return 1, "", fmt.Errorf("writing backup: %w", err)
	}
	keyNode.SetString(ref)
	out, err := marshalNode(&doc)
	if err != nil {
		return 1, backupPath, fmt.Errorf("marshalling: %w", err)
	}
	if err := atomicWriteFile(path, out, fileModeOr(path, 0o600)); err != nil {
		return 1, backupPath, fmt.Errorf("writing file: %w", err)
	}
	return 1, backupPath, nil
}

// setRootConfigVersion sets (or adds) the top-level config_version scalar in the
// main config file to v, preserving the rest of the file (yaml.v3 node edit). It
// NEVER lowers an existing value — a config already stamped newer than v is left
// untouched, so the seal cannot downgrade a rolled-back install's version.
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
		if existing, aerr := strconv.Atoi(strings.TrimSpace(cv.Value)); aerr == nil && existing > v {
			return nil // never downgrade a newer config
		}
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
	out, err := marshalNode(&doc)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, out, fileModeOr(path, 0o600))
}
