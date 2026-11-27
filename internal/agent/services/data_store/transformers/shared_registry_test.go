package transformers

import (
	"testing"

	"senhub-agent.go/internal/agent/services/logger"
)

// TestSharedRegistryIsOnePerProcess checks that two calls to SharedRegistry
// return the same non-nil registry instance.
func TestSharedRegistryIsOnePerProcess(t *testing.T) {
	first := SharedRegistry(nil)
	second := SharedRegistry(nil)

	if first == nil || second == nil {
		t.Fatal("SharedRegistry returned nil")
	}
	if first != second {
		t.Fatalf("expected the same registry instance, got %p and %p", first, second)
	}
	_ = logger.NewModuleLogger(nil, "test")
}
