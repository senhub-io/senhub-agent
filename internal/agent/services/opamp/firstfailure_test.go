package opamp

import (
	"strings"
	"testing"
)

func TestFirstFailureMessageNamesARefusal(t *testing.T) {
	if got := firstFailureMessage("websocket: bad handshake"); !strings.Contains(got, "refused") {
		t.Errorf("bad handshake reads %q, want it to mention a refusal", got)
	}
	if got := firstFailureMessage("dial tcp 192.0.2.1:443: connect: connection refused"); !strings.Contains(got, "unreachable") {
		t.Errorf("dial error reads %q, want unreachable", got)
	}
}
