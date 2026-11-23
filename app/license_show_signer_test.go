package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/license"
)

func TestPrintLicenseSigner(t *testing.T) {
	t.Run("delegated licence names platform kid and delegation expiry", func(t *testing.T) {
		var out bytes.Buffer
		printLicenseSigner(&out, &license.License{
			Platform:            "lab-bench",
			DelegationKID:       "k-2026",
			DelegationExpiresAt: time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC),
		})
		for _, want := range []string{"platform lab-bench", "Delegation kid: k-2026", "Delegation expires at: Sat, 02 Jan 2027"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("output %q lacks %q", out.String(), want)
			}
		}
	})
	t.Run("root-signed licence says so", func(t *testing.T) {
		var out bytes.Buffer
		printLicenseSigner(&out, &license.License{})
		if !strings.Contains(out.String(), "the root key") || strings.Contains(out.String(), "Delegation") {
			t.Errorf("output %q", out.String())
		}
	})
}
