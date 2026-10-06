package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/services/logger"
)

// licenseSidecarName is the fixed filename of the license sidecar, resolved
// next to the agent config (license.jwt beside agent.yaml). Keeping the license
// in a dedicated file rather than inline in agent.yaml lets an operator receive
// it as a single file and drop it in place, with no risk of mangling a very
// long JWT on copy-paste into YAML. The token stays in clear on disk: it is a
// JWT bound to the agent key, not a portable access secret, so it is
// deliberately excluded from the ${secret:} seal.
const licenseSidecarName = "license.jwt"

// LicenseSidecarPath returns the absolute path of the license sidecar for a
// given config path: license.jwt in the same directory as agent.yaml. The
// directory is derived exactly like probes.d/ and strategies.d/ so the three
// resolve consistently.
func LicenseSidecarPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), licenseSidecarName)
}

// applyLicenseSidecar fills cfg.Agent.License from the sidecar file when the
// inline field is empty. Precedence: an inline value — including a resolved
// ${file:}/${secret:} reference, since this runs after Substitute — always
// wins; the sidecar is the fallback so dropping a license.jwt next to the
// config works without editing YAML.
//
// A present-but-unreadable sidecar fails the load rather than silently
// downgrading to the free tier, which would disable paid probes without a
// trace.
func applyLicenseSidecar(cfg *LocalConfigurationData, configPath string) error {
	if cfg.Agent.License != "" {
		return nil
	}
	path := LicenseSidecarPath(configPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading license sidecar %s: %w", path, err)
	}
	cfg.Agent.License = strings.TrimSpace(string(data))
	return nil
}

// WriteLicenseSidecar writes the JWT to the license sidecar next to configPath
// with 0600 permissions and clears any inline agent.license so the sidecar is
// the single source of truth. It backs `license activate`.
func WriteLicenseSidecar(configPath, jwt string) error {
	path := LicenseSidecarPath(configPath)
	if err := atomicWriteFile(path, []byte(jwt+"\n"), 0o600); err != nil {
		return fmt.Errorf("writing license sidecar %s: %w", path, err)
	}
	if err := SetLicenseField(configPath, ""); err != nil {
		return fmt.Errorf("clearing inline license in %s: %w", configPath, err)
	}
	return nil
}

// RemoveLicenseSidecar deletes the license sidecar (if present) and clears any
// inline agent.license. It backs `license remove`.
func RemoveLicenseSidecar(configPath string) error {
	path := LicenseSidecarPath(configPath)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing license sidecar %s: %w", path, err)
	}
	if err := SetLicenseField(configPath, ""); err != nil {
		return fmt.Errorf("clearing inline license in %s: %w", configPath, err)
	}
	return nil
}

// readInlineLicense returns the raw agent.license scalar from configPath
// without substitution, so a ${file:}/${secret:} reference is returned
// verbatim (not resolved) and can be distinguished from a literal JWT.
func readInlineLicense(configPath string) (string, error) {
	raw, err := os.ReadFile(configPath) // #nosec G304 - operator-provided config path
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", configPath, err)
	}
	var doc struct {
		Agent struct {
			License string `yaml:"license"`
		} `yaml:"agent"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("parsing %s: %w", configPath, err)
	}
	return doc.Agent.License, nil
}

// MigrateLicenseToSidecar moves an inline plaintext license out of agent.yaml
// into the license.jwt sidecar, so every install converges on the file-based
// license. It backs the boot reconciliation (called alongside the inline-secret
// seal): an install carrying a JWT inline in agent.yaml is converted on the
// next start with no operator action.
//
// It is a no-op when the inline field is empty (free tier or already migrated)
// or is a ${...} reference (the operator deliberately points elsewhere — do not
// second-guess it). The move is verified by reloading: on any mismatch the
// inline field is put back by a guarded node-level edit and the freshly
// written sidecar removed, so a fault never changes the effective license
// and never overwrites an operator's concurrent edit.
func MigrateLicenseToSidecar(configPath string, log *logger.ModuleLogger) error {
	inline, err := readInlineLicense(configPath)
	if err != nil {
		return err
	}
	if inline == "" || strings.Contains(inline, "${") {
		return nil
	}

	// Undoing puts back only what this migration changed: the inline
	// license field (through the same guarded node-level edit that
	// cleared it) and the sidecar. Restoring a whole pre-migration copy of
	// agent.yaml would overwrite whatever an operator saved in between.
	restore := func(cause error) error {
		var failures []string
		if err := SetLicenseField(configPath, inline); err != nil {
			failures = append(failures, fmt.Sprintf("putting the inline license back: %v", err))
		}
		if err := os.Remove(LicenseSidecarPath(configPath)); err != nil && !os.IsNotExist(err) {
			failures = append(failures, fmt.Sprintf("removing the sidecar: %v", err))
		}
		if len(failures) > 0 {
			return fmt.Errorf("%w; rolling back ALSO failed: %s", cause, strings.Join(failures, "; "))
		}
		return cause
	}

	// WriteLicenseSidecar writes the sidecar (0600) and clears the inline field.
	if err := WriteLicenseSidecar(configPath, inline); err != nil {
		return restore(fmt.Errorf("migrating inline license to sidecar: %w", err))
	}

	// Verify: the effective license after the move must be unchanged.
	after, err := LoadFromDisk(configPath, nil)
	if err != nil {
		return restore(fmt.Errorf("verifying license migration (reload failed): %w", err))
	}
	if after.Agent.License != inline {
		return restore(fmt.Errorf("verifying license migration: effective license changed after move"))
	}

	if log != nil {
		log.Info().Str("sidecar", LicenseSidecarPath(configPath)).
			Msg("Migrated inline license to the license.jwt sidecar")
	}
	return nil
}

// ResolveEffectiveLicense returns the license that is active for a given config
// path and inline value: the inline value (with substitution applied) if
// non-empty, otherwise the sidecar contents, otherwise empty (free tier). It
// lets `license show` reflect the same resolution the loader applies at boot.
func ResolveEffectiveLicense(configPath, inline string) (string, error) {
	if inline != "" {
		return SubstituteString(inline)
	}
	path := LicenseSidecarPath(configPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading license sidecar %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}
