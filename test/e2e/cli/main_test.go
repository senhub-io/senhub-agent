//go:build e2e

package cli

import (
	"archive/zip"
	"bytes"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"aead.dev/minisign"
)

var (
	bin         string
	oldBin      string
	certFile    string
	registryURL string

	filesMu sync.Mutex
	files   = map[string][]byte{}
)

func TestMain(m *testing.M) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		fmt.Println("cli e2e: needs Linux and root; skipped")
		os.Exit(0)
	}

	dir, err := os.MkdirTemp("", "cli-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			fmt.Fprintln(os.Stderr, "cli e2e: go.mod not found")
			os.RemoveAll(dir)
			os.Exit(1)
		}
		wd = parent
	}
	repoRoot := wd

	pub, priv, err := minisign.GenerateKey(nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	pubText, err := pub.MarshalText()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.RemoveAll(dir)
		os.Exit(1)
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(serveRegistry))
	registryURL = srv.URL
	certFile = filepath.Join(dir, "registry.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(certFile, pemBytes, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		srv.Close()
		os.RemoveAll(dir)
		os.Exit(1)
	}

	build := func(out, version string) error {
		ldflags := "-X 'senhub-agent.go/internal/agent/cliArgs.Version=" + version + "' -X 'senhub-agent.go/internal/agent/services/auto_update.signingPublicKey=" + string(pubText) + "' -X 'senhub-agent.go/internal/agent/services/auto_update.DEFAULT_REGISTRY_URL=" + registryURL + "'"
		cmd := exec.Command("go", "build", "-o", out, "-ldflags", ldflags, "./cmd/agent")
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("build %s failed: %v\n%s\n", out, err, output)
		}
		return err
	}

	bin = filepath.Join(dir, "agent-rc5")
	oldBin = filepath.Join(dir, "agent-065")
	if err := build(bin, "0.7.0-rc.5"); err != nil {
		srv.Close()
		os.RemoveAll(dir)
		os.Exit(1)
	}
	if err := build(oldBin, "0.6.5"); err != nil {
		srv.Close()
		os.RemoveAll(dir)
		os.Exit(1)
	}

	files["/releases/releases.json"] = []byte(`[{"name":"latest","version":"0.6.5"},{"name":"0.6.5","version":"0.6.5"},{"name":"0.6.4","version":"0.6.4"}]`)
	files["/releases/beta/releases.json"] = []byte(`[{"name":"latest-beta","version":"0.7.0-rc.6"},{"name":"0.7.0-rc.6","version":"0.7.0-rc.6"},{"name":"0.7.0-rc.5","version":"0.7.0-rc.5"}]`)
	for _, v := range []string{"0.6.5", "0.6.4", "0.7.0-rc.6", "0.7.0-rc.5"} {
		files["/download/"+v+"/metadata.json"] = []byte(`{"name":"` + v + `","version":"` + v + `"}`)
	}

	oldBytes, err := os.ReadFile(oldBin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		srv.Close()
		os.RemoveAll(dir)
		os.Exit(1)
	}
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	header := &zip.FileHeader{Name: "senhub-agent", Method: zip.Deflate}
	header.SetMode(0o755)
	w, err := zw.CreateHeader(header)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		srv.Close()
		os.RemoveAll(dir)
		os.Exit(1)
	}
	if _, err := w.Write(oldBytes); err != nil {
		fmt.Fprintln(os.Stderr, err)
		srv.Close()
		os.RemoveAll(dir)
		os.Exit(1)
	}
	if err := zw.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		srv.Close()
		os.RemoveAll(dir)
		os.Exit(1)
	}
	zipPath := "/download/0.6.5/senhub-agent-linux-" + runtime.GOARCH + ".zip"
	files[zipPath] = zipBuf.Bytes()
	files[zipPath+".minisig"] = minisign.Sign(priv, zipBuf.Bytes())

	code := m.Run()
	srv.Close()
	os.RemoveAll(dir)
	os.Exit(code)
}

func serveRegistry(w http.ResponseWriter, r *http.Request) {
	filesMu.Lock()
	data, ok := files[r.URL.Path]
	filesMu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(r.URL.Path, ".json") {
		w.Header().Set("Content-Type", "application/json")
	}
	_, _ = w.Write(data)
}

func run(t *testing.T, exe string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Stdin = bytes.NewReader(nil)
	cmd.Env = append(os.Environ(), "SSL_CERT_FILE="+certFile, "HOME="+t.TempDir())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("start %s: %v", exe, err)
		}
	}
	return stdout.String(), stderr.String(), code
}

func copyBin(t *testing.T, exe string) string {
	t.Helper()
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "senhub-agent")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func withIncludeBeta(t *testing.T, on bool) {
	t.Helper()
	const path = "/etc/senhub-agent/agent.yaml"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	prev, err := os.ReadFile(path)
	existed := err == nil
	content := []byte("config_version: 3\nagent:\n  name: e2e\nauto_update:\n  include_beta: " + strconv.FormatBool(on) + "\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.WriteFile(path, prev, 0o644)
		} else {
			_ = os.Remove(path)
		}
	})
}
