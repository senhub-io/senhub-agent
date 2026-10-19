package app

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// A piped stdin (a script fed to `bash -s`) must not be read: the next
// line is the script's next command, not an answer.
func TestConfirmFromDoesNotReadANonTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := w.WriteString("y\necho next-command\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()

	var errOut bytes.Buffer
	ok, interactive := confirmFrom(r, &errOut)
	if ok || interactive {
		t.Fatalf("confirmFrom(pipe) = %v, %v; want false, false", ok, interactive)
	}
	if !strings.Contains(errOut.String(), "--yes") {
		t.Errorf("message %q should say how to confirm", errOut.String())
	}
	rest := make([]byte, 64)
	n, _ := r.Read(rest)
	if got := string(rest[:n]); got != "y\necho next-command\n" {
		t.Errorf("stdin was consumed: %q left", got)
	}
}
