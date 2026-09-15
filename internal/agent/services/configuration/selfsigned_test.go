package configuration

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// "tls.enabled: true" with no certificate used to be a silent outage: the
// listener could not start and the service still reported healthy (#879).
// The pair is now produced, and it has to be one a listener can actually
// load — a file that exists but does not parse would trade a silent outage
// for a noisy one.
func TestEnsureSelfSignedCertProducesAUsablePair(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "certs", "agent-cert.pem")
	keyPath := filepath.Join(dir, "certs", "agent-key.pem")

	if err := EnsureSelfSignedCert(certPath, keyPath, []string{"localhost"}); err != nil {
		t.Fatalf("generating into a directory that does not exist yet: %v", err)
	}
	if _, err := tls.LoadX509KeyPair(certPath, keyPath); err != nil {
		t.Fatalf("the generated pair does not load as a TLS keypair: %v", err)
	}

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat on the generated key: %v", err)
	}
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0600 {
		t.Errorf("private key written with mode %#o, want 0600", perm)
	}
}

// An operator who replaced the generated pair with their own must keep it.
// Regenerating on every start would quietly undo that, and they would find
// out through a browser warning rather than through us.
func TestEnsureSelfSignedCertLeavesAnExistingPairAlone(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "agent-cert.pem")
	keyPath := filepath.Join(dir, "agent-key.pem")

	if err := EnsureSelfSignedCert(certPath, keyPath, []string{"localhost"}); err != nil {
		t.Fatalf("first generation: %v", err)
	}
	before, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("reading the first certificate: %v", err)
	}

	if err := EnsureSelfSignedCert(certPath, keyPath, []string{"localhost"}); err != nil {
		t.Fatalf("second call on an existing pair: %v", err)
	}
	after, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("reading the certificate again: %v", err)
	}
	if string(before) != string(after) {
		t.Error("an existing certificate was overwritten; an operator's own pair would have been lost")
	}
}
