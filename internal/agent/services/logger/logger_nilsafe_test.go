package logger

import "testing"

// TestNewModuleLogger_NilBaseLogger pins the nil-safety contract:
// constructing a module logger from a nil base must not panic — probe
// constructors are routinely exercised with a nil logger by tests and
// embedding code (enterprise#15: SIGSEGV before any DB work).
func TestNewModuleLogger_NilBaseLogger(t *testing.T) {
	ml := NewModuleLogger(nil, "test.module")
	if ml == nil {
		t.Fatal("NewModuleLogger returned nil")
	}
	// Must be usable without panicking.
	ml.Info().Str("k", "v").Msg("no-op write on nop logger")
}
