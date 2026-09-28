package configuration

import (
	"regexp"
	"strings"
	"testing"
)

// The vectors come from Python's uuid.uuid5 over the same namespace: the
// derivation is a standard RFC 4122 UUID v5, which any backend can parse.
func TestAgentInstanceIDIsAStandardUUIDv5(t *testing.T) {
	vectors := map[string]string{
		"d0000000-0000-4000-8000-000000000001": "17986334-29c6-5d88-9c5e-42ea561fc603",
		"e313cd19-45d9-4711-8b09-3f58ac6e7595": "0ef1a045-4be0-5c98-ae22-3e399f8a13f0",
	}
	v5 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for key, want := range vectors {
		got := AgentInstanceID(key)
		if got != want {
			t.Errorf("AgentInstanceID(%s) = %s, want %s", key, got, want)
		}
		if !v5.MatchString(got) {
			t.Errorf("%s is not a version 5, RFC 4122 variant UUID", got)
		}
		if strings.Contains(got, key) {
			t.Errorf("the instance id carries the key")
		}
	}
	if AgentInstanceID("") != "" {
		t.Error("no key must give no instance id")
	}
}
