package logger

import (
	"strings"
	"testing"
)

func TestMaskValueHidesEverything(t *testing.T) {
	if got := maskValue("LAB-SECRET-pwd"); got != "***" {
		t.Errorf("maskValue = %q, want \"***\"", got)
	}
	out := MaskSensitiveData(`pwd=LAB-SECRET-pwd`)
	if strings.Contains(out, "LAB") {
		t.Errorf("MaskSensitiveData leaked LAB: %s", out)
	}
	if strings.Contains(out, "SECRET") {
		t.Errorf("MaskSensitiveData leaked SECRET: %s", out)
	}
}
