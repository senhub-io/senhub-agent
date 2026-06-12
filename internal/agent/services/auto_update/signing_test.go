package auto_update

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"aead.dev/minisign"
	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// withSigningKey installs a freshly generated keypair for the duration
// of the test and returns the private key for signing fixtures.
func withSigningKey(t *testing.T) minisign.PrivateKey {
	t.Helper()
	pub, priv, err := minisign.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pubText, err := pub.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	old := signingPublicKey
	signingPublicKey = string(pubText)
	t.Cleanup(func() { signingPublicKey = old })
	return priv
}

func TestVerifyArchiveSignature_Valid(t *testing.T) {
	priv := withSigningKey(t)
	archive := []byte("release archive bytes")

	if err := verifyArchiveSignature(archive, minisign.Sign(priv, archive)); err != nil {
		t.Errorf("valid signature rejected: %v", err)
	}
}

func TestVerifyArchiveSignature_TamperedArchive(t *testing.T) {
	priv := withSigningKey(t)
	archive := []byte("release archive bytes")
	sig := minisign.Sign(priv, archive)

	tampered := append([]byte{}, archive...)
	tampered[0] ^= 0xFF
	if err := verifyArchiveSignature(tampered, sig); err == nil {
		t.Fatal("tampered archive accepted")
	}
}

func TestVerifyArchiveSignature_WrongKey(t *testing.T) {
	withSigningKey(t) // embedded key
	_, otherPriv, err := minisign.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	archive := []byte("release archive bytes")

	if err := verifyArchiveSignature(archive, minisign.Sign(otherPriv, archive)); err == nil {
		t.Fatal("signature from a different key accepted")
	}
}

func TestVerifyArchiveSignature_FailClosedWithoutKey(t *testing.T) {
	old := signingPublicKey
	signingPublicKey = ""
	t.Cleanup(func() { signingPublicKey = old })

	if err := verifyArchiveSignature([]byte("x"), []byte("y")); err == nil {
		t.Fatal("build without embedded key must refuse to self-update")
	}
}

func TestVerifyArchiveSignature_GarbageSignature(t *testing.T) {
	withSigningKey(t)
	if err := verifyArchiveSignature([]byte("archive"), []byte("not a minisign signature")); err == nil {
		t.Fatal("garbage signature accepted")
	}
}

// ---- doUpdate integration: rejection paths (#266) ----

// fixtureArchive builds an in-memory release ZIP containing a fake
// agent binary padded over doUpdate's 1 MiB minimum-size check.
func fixtureArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// Stored (uncompressed) entry so the archive stays over doUpdate's
	// 1 MiB minimum regardless of padding content.
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "senhub-agent", Method: zip.Store})
	if err != nil {
		t.Fatalf("zip.CreateHeader: %v", err)
	}
	pad := make([]byte, 2*1024*1024)
	for i := range pad {
		pad[i] = byte(i * 7 % 251)
	}
	if _, err := w.Write(pad); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func newSigningTestUpdater(t *testing.T, srv *httptest.Server) *autoUpdate {
	t.Helper()
	zlog := zerolog.New(os.Stderr)
	return &autoUpdate{
		logger:     logger.NewModuleLogger((*logger.Logger)(&zlog), "service.auto_update.test"),
		httpClient: srv.Client(),
	}
}

func TestDoUpdate_RejectsTamperedArchive(t *testing.T) {
	agentstate.ResetUpdateRejectedForTest()
	t.Cleanup(agentstate.ResetUpdateRejectedForTest)

	priv := withSigningKey(t)
	archive := fixtureArchive(t)
	sig := minisign.Sign(priv, archive)
	// Tamper AFTER signing: flip one byte inside the served archive.
	tampered := append([]byte{}, archive...)
	tampered[len(tampered)/2] ^= 0xFF

	mux := http.NewServeMux()
	mux.HandleFunc("/dl/agent.zip", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(tampered)
	})
	mux.HandleFunc("/dl/agent.zip.minisig", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(sig)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	au := newSigningTestUpdater(t, srv)
	err := au.doUpdate(srv.URL + "/dl/agent.zip")
	if err == nil {
		t.Fatal("tampered archive applied")
	}
	if !strings.Contains(err.Error(), "REJECTED") {
		t.Errorf("error should mark the rejection loudly: %v", err)
	}
	if got := agentstate.GetUpdateRejectedByReason()["signature_invalid"]; got != 1 {
		t.Errorf("signature_invalid counter = %d, want 1", got)
	}
}

func TestDoUpdate_RejectsMissingSignature(t *testing.T) {
	agentstate.ResetUpdateRejectedForTest()
	t.Cleanup(agentstate.ResetUpdateRejectedForTest)

	withSigningKey(t)
	archive := fixtureArchive(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/dl/agent.zip", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive)
	})
	// No .minisig route: the registry never published a signature.
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	au := newSigningTestUpdater(t, srv)
	err := au.doUpdate(srv.URL + "/dl/agent.zip")
	if err == nil {
		t.Fatal("unsigned archive applied")
	}
	if got := agentstate.GetUpdateRejectedByReason()["signature_unavailable"]; got != 1 {
		t.Errorf("signature_unavailable counter = %d, want 1", got)
	}
}

func TestFetchVersionMetadata_RejectsNon200(t *testing.T) {
	mux := http.NewServeMux()
	// Any path returns 404 with a JSON error body that would decode
	// into an empty-but-valid VersionMetadata.
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "not found"}`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	if _, err := fetchVersionMetadata(srv.Client(), srv.URL, "9.9.9"); err == nil {
		t.Fatal("non-200 metadata response accepted")
	}
}
