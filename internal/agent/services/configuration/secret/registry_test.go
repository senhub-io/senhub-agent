package secret

import "testing"

// TestBackend_RefusesEmptyConfigDir pins the m4 guard: reaching the backend
// before a config dir is recorded must error, never write key material relative
// to the process CWD. The check precedes the sync.Once, so a later
// correctly-ordered call can still succeed.
func TestBackend_RefusesEmptyConfigDir(t *testing.T) {
	SetProvider(nil)
	SetConfigDir("")
	t.Cleanup(func() { SetProvider(nil) })

	if _, err := Backend(); err == nil {
		t.Fatal("Backend with an empty config dir must error, not create key material in CWD")
	}
}
