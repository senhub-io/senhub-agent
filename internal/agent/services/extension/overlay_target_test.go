package extension

import "testing"

func TestIsTargetParam(t *testing.T) {
	for _, key := range []string{"host", "url", "base_url", "dsn", "port"} {
		if !IsTargetParam(key) {
			t.Errorf("IsTargetParam(%q) = false, want true", key)
		}
	}
	for _, key := range []string{"password", "interval", "timeout"} {
		if IsTargetParam(key) {
			t.Errorf("IsTargetParam(%q) = true, want false", key)
		}
	}
}
