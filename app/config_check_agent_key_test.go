package app

import (
	"strings"
	"testing"
)

// config check printed the agent key in clear, and its output is what
// operators paste into tickets. The key is the credential a monitoring
// tool reads the agent with.
func TestConfigCheckNeverPrintsTheAgentKey(t *testing.T) {
	key := "0f4c2a51-9b7e-4d3a-8c61-2e5f7a9b0d13"
	line, isErr, isWarn := agentKeyCheckLine(key)
	if strings.Contains(line, key) || strings.Contains(line, key[:8]) {
		t.Fatalf("config check line carries the agent key: %q", line)
	}
	if isErr || isWarn {
		t.Errorf("a UUID key was reported as a problem: %q", line)
	}
	if !strings.Contains(line, "[OK]") || !strings.Contains(line, "set") {
		t.Errorf("config check does not say the key is set: %q", line)
	}
}

func TestConfigCheckAgentKeyMissingAndMalformed(t *testing.T) {
	if _, isErr, _ := agentKeyCheckLine(""); !isErr {
		t.Error("a missing agent key was not reported as an error")
	}
	line, isErr, isWarn := agentKeyCheckLine("hunter2")
	if isErr || !isWarn {
		t.Errorf("a non-UUID key should warn, got error=%v warn=%v", isErr, isWarn)
	}
	if strings.Contains(line, "hunter2") {
		t.Errorf("config check line carries the agent key: %q", line)
	}
}
