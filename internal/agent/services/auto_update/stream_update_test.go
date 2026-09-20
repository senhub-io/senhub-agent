package auto_update

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"aead.dev/minisign"
)

// releaseArchive builds the ZIP the release pipeline publishes: one
// stored (uncompressed) entry holding the binary, big enough to pass the
// size floor of the updater.
func releaseArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "senhub-agent", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// prehashedSignature signs the way the release tool does from 0.6.0 on:
// over the BLAKE2b digest of the archive, which is what a streaming
// verification can check.
func prehashedSignature(t *testing.T, priv minisign.PrivateKey, archive []byte) []byte {
	t.Helper()
	r := minisign.NewReader(bytes.NewReader(archive))
	if _, err := io.ReadAll(r); err != nil {
		t.Fatal(err)
	}
	return r.Sign(priv)
}

func randomBinary(t *testing.T, size int) []byte {
	t.Helper()
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// releaseServer serves the archive and its detached signature over TLS,
// the only scheme the updater accepts.
func releaseServer(t *testing.T, archive, signature []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/senhub-agent.zip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/senhub-agent.zip.minisig", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(signature)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func updaterFor(t *testing.T, srv *httptest.Server, target string) *autoUpdate {
	t.Helper()
	return &autoUpdate{
		logger:     createTestModuleLogger(),
		httpClient: srv.Client(),
		targetPath: target,
	}
}

func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	return names
}

// The archive is written next to the binary, verified, and the binary
// swapped in, with nothing read whole into memory and nothing left
// behind.
func TestDoUpdate_StreamsTheArchiveThroughDisk(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the rename dance hides the old file on Windows; covered by the MSI path there")
	}
	priv := withSigningKey(t)
	binary := randomBinary(t, 1536*1024)
	archive := releaseArchive(t, binary)
	srv := releaseServer(t, archive, prehashedSignature(t, priv, archive))

	dir := t.TempDir()
	target := filepath.Join(dir, "senhub-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := updaterFor(t, srv, target).doUpdate(srv.URL + "/senhub-agent.zip"); err != nil {
		t.Fatalf("doUpdate: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binary) {
		t.Fatalf("target holds %d bytes, want the %d bytes of the new binary", len(got), len(binary))
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o750 {
		t.Errorf("mode = %v, want the previous binary's 0750 kept", info.Mode().Perm())
	}
	if left := leftovers(t, dir); len(left) != 0 {
		t.Errorf("files left next to the binary: %v", left)
	}
}

// A signature that does not match refuses the update before the
// archive is opened, and leaves the binary and the directory untouched.
func TestDoUpdate_RefusesATamperedArchiveBeforeOpeningIt(t *testing.T) {
	priv := withSigningKey(t)
	binary := randomBinary(t, 1536*1024)
	archive := releaseArchive(t, binary)
	signature := prehashedSignature(t, priv, archive)
	archive[len(archive)-1] ^= 0xFF
	srv := releaseServer(t, archive, signature)

	dir := t.TempDir()
	target := filepath.Join(dir, "senhub-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := updaterFor(t, srv, target).doUpdate(srv.URL + "/senhub-agent.zip")
	if err == nil || !strings.Contains(err.Error(), "REJECTED") {
		t.Fatalf("err = %v, want the update rejected", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "old binary" {
		t.Errorf("target was touched: %q", got)
	}
	if left := leftovers(t, dir); len(left) != 0 {
		t.Errorf("files left next to the binary: %v", left)
	}
}

// An archive without the agent binary is refused after verification and
// cleaned up.
func TestDoUpdate_RefusesAnArchiveWithoutTheBinary(t *testing.T) {
	priv := withSigningKey(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.CreateHeader(&zip.FileHeader{Name: "README", Method: zip.Store})
	_, _ = w.Write(randomBinary(t, 1536*1024))
	_ = zw.Close()
	archive := buf.Bytes()
	srv := releaseServer(t, archive, prehashedSignature(t, priv, archive))

	dir := t.TempDir()
	target := filepath.Join(dir, "senhub-agent")
	_ = os.WriteFile(target, []byte("old binary"), 0o755)

	err := updaterFor(t, srv, target).doUpdate(srv.URL + "/senhub-agent.zip")
	if err == nil || !strings.Contains(err.Error(), "does not contain senhub-agent binary") {
		t.Fatalf("err = %v", err)
	}
	if left := leftovers(t, dir); len(left) != 0 {
		t.Errorf("files left next to the binary: %v", left)
	}
}

// The releases published so far carry a legacy signature over the bytes
// themselves; they must keep installing, at the cost of the archive
// alone in memory.
func TestDoUpdate_AcceptsALegacySignature(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the rename dance hides the old file on Windows; covered by the MSI path there")
	}
	priv := withSigningKey(t)
	binary := randomBinary(t, 1536*1024)
	archive := releaseArchive(t, binary)
	srv := releaseServer(t, archive, minisign.Sign(priv, archive))

	dir := t.TempDir()
	target := filepath.Join(dir, "senhub-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := updaterFor(t, srv, target).doUpdate(srv.URL + "/senhub-agent.zip"); err != nil {
		t.Fatalf("doUpdate: %v", err)
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, binary) {
		t.Fatalf("target holds %d bytes, want the new binary", len(got))
	}
}
