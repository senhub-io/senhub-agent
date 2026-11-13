package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestRefreshUnitStepNeverStopsTheUpdate(t *testing.T) {
	log := zerolog.Nop()
	line, failed := refreshUnitStep(func(string) (string, error) {
		return "", errors.New("daemon-reload failed")
	}, "/usr/local/bin/senhub-agent", &log)
	if !failed {
		t.Fatal("a failed refresh must be reported")
	}
	if !strings.Contains(line, "daemon-reload failed") {
		t.Fatalf("line = %q, want the cause", line)
	}

	line, failed = refreshUnitStep(func(string) (string, error) {
		return "Systemd unit refreshed from the new binary: /etc/systemd/system/senhub-agent.service", nil
	}, "/usr/local/bin/senhub-agent", &log)
	if failed || line == "" {
		t.Fatalf("line = %q, failed = %v, want the refresh line and no failure", line, failed)
	}
}
