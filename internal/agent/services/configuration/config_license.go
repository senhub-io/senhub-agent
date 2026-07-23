package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
