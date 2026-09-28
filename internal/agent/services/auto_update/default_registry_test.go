package auto_update

import (
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

// The built-in default is already a base: normalising it must change
// nothing, or every update from an install without auto_update.url warns
// about a path the operator never wrote.
func TestDefaultRegistryURLIsABase(t *testing.T) {
	if fixed, changed := configuration.NormalizeRegistryURL(DEFAULT_REGISTRY_URL); changed {
		t.Errorf("DEFAULT_REGISTRY_URL %q normalises to %q", DEFAULT_REGISTRY_URL, fixed)
	}
}
