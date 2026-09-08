package configuration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"

	"senhub-agent.go/internal/agent/services/configuration/secret"
)

// managedFragmentHeader marks a probes.d fragment written by the web
// console. The console rewrites only files carrying it: a fragment an
// operator wrote by hand, with its comments and its order, is never
// reformatted by a round-trip; taking it over means removing the line.
const managedFragmentHeader = "# Managed by the SenHub web console. Edit it from the console, or remove this line to maintain it by hand."

// ProbeFragmentPath is where the console keeps one probe instance: one
// file per instance, after the 00-host.yaml the install writes.
func ProbeFragmentPath(configPath, name string) string {
	return filepath.Join(filepath.Dir(configPath), "probes.d", "50-"+safeFilenameComponent(name)+".yaml")
}

// IsManagedProbeFragment reports whether path is a fragment the console
// wrote, by its header.
func IsManagedProbeFragment(path string) bool {
	raw, err := os.ReadFile(path) // #nosec G304 - path is under probes.d/
	if err != nil {
		return false
	}
	return bytes.HasPrefix(raw, []byte(managedFragmentHeader))
}

// refuseUnlessMultiFile rejects the legacy monolithic layout, where
// probes.d/ is ignored by the loader and a fragment would do nothing.
func refuseUnlessMultiFile(configPath string) error {
	raw, err := os.ReadFile(configPath) // #nosec G304 - operator-provided config path
	if err != nil {
		return fmt.Errorf("reading %s: %w", configPath, err)
	}
	legacy, err := isLegacyMonolithic(raw)
	if err != nil {
		return err
	}
	if legacy {
		return fmt.Errorf("this is a legacy monolithic configuration; probes.d is ignored — run 'config migrate' first")
	}
	return nil
}

// CreateProbeFragment writes a new probe instance as its own managed
// fragment. It refuses an invalid name, a name already configured
// anywhere (two probes of one name: only the first starts, and their
// secrets collide in the store), an existing file at the target path,
// and the legacy layout. Values under secretPaths (dotted param paths),
// and any string param whose leaf key looks sensitive, are stored in
// the secret store and referenced from the file; the file never holds
// a credential in clear.
func CreateProbeFragment(configPath string, p ProbeConfig, secretPaths []string) (string, error) {
	if err := refuseUnlessMultiFile(configPath); err != nil {
		return "", err
	}
	if !IsValidProbeName(p.Name) {
		return "", fmt.Errorf("probe name %q: use letters, digits, hyphens and underscores, starting with a letter or digit", p.Name)
	}
	if p.Type == "" {
		return "", fmt.Errorf("probe %q: type is required", p.Name)
	}
	cfg, err := LoadFromDisk(configPath, nil)
	if err != nil {
		return "", fmt.Errorf("loading the configuration: %w", err)
	}
	for _, existing := range cfg.Probes {
		if existing.Name == p.Name {
			return "", fmt.Errorf("a probe named %q already exists; names must be unique", p.Name)
		}
	}
	path := ProbeFragmentPath(configPath, p.Name)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists", path)
	}
	DropNilValues(p.Params)
	if err := sealProbeParams(&p, secretPaths); err != nil {
		return "", err
	}
	return path, writeManagedProbeFragment(path, p)
}

// UpdateProbeFragment rewrites a managed fragment with the given probe.
// The name is the identity of the file; it cannot be changed here.
func UpdateProbeFragment(configPath string, p ProbeConfig, secretPaths []string) (string, error) {
	if err := refuseUnlessMultiFile(configPath); err != nil {
		return "", err
	}
	path := ProbeFragmentPath(configPath, p.Name)
	if !IsManagedProbeFragment(path) {
		return "", fmt.Errorf("%s is not managed by the console (missing, or written by hand); edit it by hand or remove it first", path)
	}
	existing, err := ReadProbeFragmentParams(configPath, p.Name)
	if err != nil {
		return "", err
	}
	p.Params = KeepStoredReferences(existing, p.Params)
	DropNilValues(p.Params)
	if err := sealProbeParams(&p, secretPaths); err != nil {
		return "", err
	}
	return path, writeManagedProbeFragment(path, p)
}

// DeleteProbeFragment removes a managed fragment. A hand-written file
// is left alone with an error naming it.
func DeleteProbeFragment(configPath, name string) (string, error) {
	path := ProbeFragmentPath(configPath, name)
	if !IsManagedProbeFragment(path) {
		return "", fmt.Errorf("%s is not managed by the console; remove it by hand", path)
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("removing %s: %w", path, err)
	}
	return path, nil
}

// writeManagedProbeFragment serialises one probe as a one-element list
// under the managed header, atomically. yaml.v2 is used deliberately:
// the loader reads with it, so what is written is what is read.
func writeManagedProbeFragment(path string, p ProbeConfig) error {
	body, err := yaml.Marshal([]ProbeConfig{p})
	if err != nil {
		return fmt.Errorf("encoding probe %q: %w", p.Name, err)
	}
	out := append([]byte(managedFragmentHeader+"\n\n"), body...)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := atomicWriteFile(path, out, fileModeOr(path, 0o600)); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// sealProbeParams moves credentials out of p.Params into the secret
// store and leaves ${secret:...} references, with the same store keys
// the boot-time sealer derives (<probe name>.<param path>), so a value
// typed in the console and one sealed at boot land in the same place.
// A value that is already a ${...} reference is left as written.
func sealProbeParams(p *ProbeConfig, secretPaths []string) error {
	if p.Params == nil {
		return nil
	}
	return sealParams(p.Name, p.Params, secretPaths)
}

// sealParams moves the credentials of one params map into the secret
// store, under <instance>.<dotted path>, leaving references behind. A
// secret path that names a mapping (the headers of an OTLP output)
// seals every value inside it.
func sealParams(instance string, params map[string]interface{}, secretPaths []string) error {
	want := map[string]bool{}
	for _, sp := range secretPaths {
		want[sp] = true
	}
	var prov secret.Provider
	var walk func(path []string, m map[string]interface{}, inSecret bool) error
	walk = func(path []string, m map[string]interface{}, inSecret bool) error {
		for k, v := range m {
			full := append(append([]string{}, path...), k)
			dotted := strings.Join(full, ".")
			switch val := v.(type) {
			case map[string]interface{}:
				if err := walk(full, val, inSecret || want[dotted]); err != nil {
					return err
				}
			case string:
				if !(inSecret || want[dotted] || secret.IsSensitiveKey(k)) || val == "" || strings.HasPrefix(val, "${") {
					continue
				}
				if prov == nil {
					backend, err := secret.Backend()
					if err != nil {
						return fmt.Errorf("no secret store to hold %q: %w — reference it with ${env:...} or ${file:...} instead", dotted, err)
					}
					prov = backend
				}
				key := secret.SanitizeKey(instance + "." + dotted)
				if err := prov.Set(key, secret.New(val)); err != nil {
					return fmt.Errorf("storing secret %q: %w", key, err)
				}
				m[k] = "${secret:" + key + "}"
			}
		}
		return nil
	}
	if params == nil {
		return nil
	}
	return walk(nil, params, false)
}
