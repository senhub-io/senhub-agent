package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheLicenseCodeCanComeFromStandardInput(t *testing.T) {
	f := filepath.Join(t.TempDir(), "in")
	if err := os.WriteFile(f, []byte("eyJ.token.sig\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"-", ""} {
		in, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		got, err := licenseCodeFrom(arg, in)
		_ = in.Close()
		if err != nil || got != "eyJ.token.sig" {
			t.Errorf("arg %q: got %q, %v", arg, got, err)
		}
	}
	if got, _ := licenseCodeFrom("eyJ.arg.sig", nil); got != "eyJ.arg.sig" {
		t.Errorf("the argument must still be accepted, got %q", got)
	}
}
