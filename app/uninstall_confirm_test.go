package app

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"senhub-agent.go/internal/cliexit"
)

func TestConfirmUninstallWithoutTerminalFails(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	if _, err := w.WriteString("y\n"); err != nil {
		t.Fatalf("write to pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}

	var out, errOut bytes.Buffer
	proceed, code := confirmUninstall(r, &out, &errOut)

	if proceed {
		t.Fatal("proceed = true, want false for non-terminal stdin")
	}
	if code != cliexit.Failure {
		t.Fatalf("code = %d, want cliexit.Failure (%d)", code, cliexit.Failure)
	}
	if !strings.Contains(errOut.String(), "confirmation required: rerun with --yes") {
		t.Fatalf("errOut = %q, want it to contain %q", errOut.String(), "confirmation required: rerun with --yes")
	}
	if strings.Contains(out.String(), "Uninstall cancelled") {
		t.Fatalf("out = %q, must not contain %q", out.String(), "Uninstall cancelled")
	}
}
