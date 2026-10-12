package auto_update

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
)

// flakyReleaseServer cuts the archive body short for the first cuts
// requests, then serves it whole. It records the headers of the archive
// requests.
func flakyReleaseServer(t *testing.T, archive, signature []byte, cuts int32) (*httptest.Server, *atomic.Int32, *atomic.Pointer[http.Header]) {
	t.Helper()
	var served atomic.Int32
	var last atomic.Pointer[http.Header]
	mux := http.NewServeMux()
	mux.HandleFunc("/senhub-agent.zip", func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Clone()
		last.Store(&h)
		n := served.Add(1)
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
		if n <= cuts {
			_, _ = w.Write(archive[:len(archive)/2])
			panic(http.ErrAbortHandler)
		}
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/senhub-agent.zip.minisig", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(signature)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv, &served, &last
}

func noWait() *time.Duration {
	d := time.Duration(0)
	return &d
}

// A body cut mid-stream is retried from the first byte: two cuts then a
// whole archive gives a verified update (#590).
func TestDoUpdate_RetriesACutBody(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the rename dance hides the old file on Windows; covered by the MSI path there")
	}
	priv := withSigningKey(t)
	binary := randomBinary(t, 1536*1024)
	archive := releaseArchive(t, binary)
	srv, served, lastHeaders := flakyReleaseServer(t, archive, prehashedSignature(t, priv, archive), 2)

	dir := t.TempDir()
	target := filepath.Join(dir, "senhub-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o750); err != nil {
		t.Fatal(err)
	}
	u := updaterFor(t, srv, target)
	u.downloadWait = noWait()

	if err := u.doUpdate(srv.URL + "/senhub-agent.zip"); err != nil {
		t.Fatalf("doUpdate after two cut downloads: %v", err)
	}
	if got := served.Load(); got != 3 {
		t.Errorf("archive requested %d times, want 3", got)
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, binary) {
		t.Errorf("target holds %d bytes, want the new %d-byte binary", len(got), len(binary))
	}
	if left := leftovers(t, dir); len(left) != 0 {
		t.Errorf("files left next to the binary: %v", left)
	}
	headers := *lastHeaders.Load()
	if ua, want := headers.Get("User-Agent"), "senhub-agent/"+cliArgs.Version; ua != want {
		t.Errorf("User-Agent = %q, want %q", ua, want)
	}
	if enc := headers.Get("Accept-Encoding"); enc != "identity" {
		t.Errorf("Accept-Encoding = %q, want identity so the archive is not recompressed", enc)
	}
}

// A download that never completes gives up after the bounded number of
// attempts, and leaves nothing behind.
func TestDoUpdate_GivesUpAfterBoundedRetries(t *testing.T) {
	priv := withSigningKey(t)
	archive := releaseArchive(t, randomBinary(t, 1536*1024))
	srv, served, _ := flakyReleaseServer(t, archive, prehashedSignature(t, priv, archive), 100)

	dir := t.TempDir()
	target := filepath.Join(dir, "senhub-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o750); err != nil {
		t.Fatal(err)
	}
	u := updaterFor(t, srv, target)
	u.downloadWait = noWait()

	if err := u.doUpdate(srv.URL + "/senhub-agent.zip"); err == nil {
		t.Fatal("a download that is always cut must fail")
	}
	if got := served.Load(); got != archiveDownloadTries {
		t.Errorf("archive requested %d times, want %d", got, archiveDownloadTries)
	}
	if left := leftovers(t, dir); len(left) != 0 {
		t.Errorf("files left next to the binary: %v", left)
	}
	if got, _ := os.ReadFile(target); string(got) != "old binary" {
		t.Error("the binary must be untouched by a failed download")
	}
}
