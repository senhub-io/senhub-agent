package event

import (
	"strings"
	"testing"
)

// The listener is an HTTP server. udp used to be accepted by the check
// and then ignored, so a configuration asking for it silently got tcp.
func TestUDPIsRefusedRatherThanIgnored(t *testing.T) {
	_, err := parseEventProbeConfig(map[string]interface{}{"port": 9999, "protocol": "udp"})
	if err == nil {
		t.Fatal("udp must be refused: the listener cannot serve it")
	}
	if !strings.Contains(err.Error(), "serves HTTP") {
		t.Errorf("the refusal must say why, got %v", err)
	}
	if _, err := parseEventProbeConfig(map[string]interface{}{"port": 9999, "protocol": "tcp"}); err != nil {
		t.Errorf("tcp must still be accepted: %v", err)
	}
}
