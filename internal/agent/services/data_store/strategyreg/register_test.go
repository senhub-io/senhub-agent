package strategyreg

import (
	"reflect"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store"
)

// TestShippedStrategySet pins what this build can write to. Adding a sink
// is a deliberate act — the list here is the one an operator can name in
// `storage:`, and a strategy that appears without a decision (or vanishes
// from a refactor) shows up as a failing test rather than as a
// configuration that silently does nothing.
func TestShippedStrategySet(t *testing.T) {
	want := []string{"event", "http", "otlp", "prtg", "senhub"}
	if got := data_store.RegisteredStrategyNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("shipped strategies = %v, want %v", got, want)
	}
}
