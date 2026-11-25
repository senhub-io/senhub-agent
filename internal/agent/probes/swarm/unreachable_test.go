package swarm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
)

func TestCollectSocketUnreachableReturnsErrorWithPoints(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "no-such-docker.sock")
	p, err := NewSwarmProbe(map[string]interface{}{"socket_path": socketPath},
		logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatalf("NewSwarmProbe: %v", err)
	}

	points, collectErr := p.Collect()
	if collectErr == nil {
		t.Fatal("Collect returned nil error for an unreachable Docker socket")
	}
	if !strings.Contains(collectErr.Error(), "did not answer") {
		t.Fatalf("error does not mention the unanswered socket: %v", collectErr)
	}

	found := false
	for _, point := range points {
		if point.Name == "senhub.swarm.up" && point.Value == 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("points do not carry senhub.swarm.up=0: %+v", points)
	}
}

func TestSwarmProbeKeepsPartialResults(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "no-such-docker.sock")
	p, err := NewSwarmProbe(map[string]interface{}{"socket_path": socketPath},
		logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatalf("NewSwarmProbe: %v", err)
	}
	partial, ok := p.(types.PartialResultsProbe)
	if !ok {
		t.Fatal("the swarm probe does not implement types.PartialResultsProbe")
	}
	if !partial.KeepsPartialResults() {
		t.Fatal("KeepsPartialResults returned false; availability points of a failed cycle would be dropped")
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("the test socket unexpectedly exists: %v", err)
	}
}
