package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	if err := os.WriteFile(path, []byte(jwt+"\n"), 0o600); err != nil {
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
// second-guess it). The move is backed by a timestamped backup of agent.yaml
// and verified by reloading: on any mismatch the backup is restored and the
// freshly written sidecar removed, so a fault never changes the effective
// license.
func MigrateLicenseToSidecar(configPath string, log *logger.ModuleLogger) error {
	inline, err := readInlineLicense(configPath)
	if err != nil {
		return err
	}
	if inline == "" || strings.Contains(inline, "${") {
		return nil
	}

	raw, err := os.ReadFile(configPath) // #nosec G304 - operator-provided config path
	if err != nil {
		return fmt.Errorf("reading %s: %w", configPath, err)
	}
	backupPath := fmt.Sprintf("%s.backup.%s", configPath, time.Now().Format("20060102-150405"))
	if err := os.WriteFile(backupPath, raw, 0o600); err != nil {
		return fmt.Errorf("backing up %s before license migration: %w", configPath, err)
	}

	restore := func() {
		if data, e := os.ReadFile(backupPath); e == nil {
			_ = atomicWriteFile(configPath, data, fileModeOr(configPath, 0o600))
		}
		_ = os.Remove(LicenseSidecarPath(configPath))
	}

	// WriteLicenseSidecar writes the sidecar (0600) and clears the inline field.
	if err := WriteLicenseSidecar(configPath, inline); err != nil {
		restore()
		_ = os.Remove(backupPath)
		return fmt.Errorf("migrating inline license to sidecar: %w", err)
	}

	// Verify: the effective license after the move must be unchanged.
	after, err := LoadFromDisk(configPath, nil)
	if err != nil || after.Agent.License != inline {
		restore()
		_ = os.Remove(backupPath)
		if err != nil {
			return fmt.Errorf("verifying license migration (reload failed): %w", err)
		}
		return fmt.Errorf("verifying license migration: effective license changed after move")
	}

	_ = os.Remove(backupPath)
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
