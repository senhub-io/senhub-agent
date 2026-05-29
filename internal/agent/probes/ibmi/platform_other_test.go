//go:build !linux

package ibmi

import (
	"strings"
	"testing"

	"senhub-agent.go/probesdk/cliargs"
	"senhub-agent.go/probesdk/logger"
)

// TestNewIBMiProbe_RefusesOnNonLinux pins the platform-gate behaviour:
// on every non-linux build the constructor must fail fast, before any
// bridge subprocess is spawned, with an error string the operator can
// act on (mentions the probe name + the current platform + the
// remediation).
func TestNewIBMiProbe_RefusesOnNonLinux(t *testing.T) {
	t.Parallel()

	if platformSupported {
		t.Fatalf("platformSupported is true on a non-linux build — platform_other.go is mis-tagged")
	}

	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	_, err := NewIBMiProbe(map[string]interface{}{
		"host":     "pub400.com",
		"user":     "u",
		"password": "p",
	}, baseLogger)
	if err == nil {
		t.Fatal("expected NewIBMiProbe to fail on non-linux, got nil error")
	}
	msg := err.Error()
	for _, want := range []string{"ibmi probe", "linux"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q is missing fragment %q — operator-facing message must explain why it failed", msg, want)
		}
	}
}
