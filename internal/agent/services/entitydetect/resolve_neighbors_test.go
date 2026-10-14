package entitydetect

import (
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

func TestResolveNeighborsEnabledFromGlobalBlock(t *testing.T) {
	cfg := Resolve(&configuration.EntitiesConfig{
		Enabled:   true,
		Neighbors: &configuration.EntitiesNeighborsConfig{Enabled: true},
	}, nil, "agent")

	if !cfg.NeighborsEnabled {
		t.Fatal("NeighborsEnabled = false, want true from global entities.neighbors.enabled")
	}
}

func TestResolveNeighborsDefaultOff(t *testing.T) {
	cfg := Resolve(nil, nil, "agent")
	if cfg.NeighborsEnabled {
		t.Fatal("NeighborsEnabled = true, want false by default")
	}
}

func TestResolveNeighborsOTLPFallbackDefaultOff(t *testing.T) {
	storage := []configuration.StorageConfig{{
		Name: "otlp",
		Params: map[string]interface{}{
			"signals": map[string]interface{}{
				"entities": map[string]interface{}{
					"enabled": true,
				},
			},
		},
	}}

	cfg := Resolve(nil, storage, "agent")
	if cfg.NeighborsEnabled {
		t.Fatal("NeighborsEnabled = true, want false when OTLP does not enable neighbors")
	}
}
