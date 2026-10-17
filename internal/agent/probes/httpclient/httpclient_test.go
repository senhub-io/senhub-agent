package httpclient

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tlsServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func caFile(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	block := &pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func get(t *testing.T, o Options, url string) error {
	t.Helper()
	c, err := NewClient(o)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func TestZeroOptionsVerifyCertificates(t *testing.T) {
	srv := tlsServer(t)
	if err := get(t, Options{Timeout: 5 * time.Second}, srv.URL); err == nil {
		t.Fatal("self-signed certificate accepted with default options")
	}
}

func TestInsecureSkipVerifyAcceptsSelfSigned(t *testing.T) {
	srv := tlsServer(t)
	if err := get(t, Options{Timeout: 5 * time.Second, InsecureSkipVerify: true}, srv.URL); err != nil {
		t.Fatalf("insecure request failed: %v", err)
	}
}

func TestCAFileAcceptsItsOwnServerOnly(t *testing.T) {
	srv := tlsServer(t)
	if err := get(t, Options{Timeout: 5 * time.Second, CAFile: caFile(t, srv)}, srv.URL); err != nil {
		t.Fatalf("request with matching CA failed: %v", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotAfter: time.Now().Add(time.Hour), IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(t.TempDir(), "unrelated.pem")
	if err := os.WriteFile(unrelated, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := get(t, Options{Timeout: 5 * time.Second, CAFile: unrelated}, srv.URL); err == nil {
		t.Fatal("server accepted although its certificate is not in the CA file")
	}
}

func TestInvalidOptions(t *testing.T) {
	srv := tlsServer(t)
	bad := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(bad, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]Options{
		"zero timeout":     {},
		"missing CA":       {Timeout: time.Second, CAFile: filepath.Join(t.TempDir(), "absent.pem")},
		"unparsable CA":    {Timeout: time.Second, CAFile: bad},
		"cert without key": {Timeout: time.Second, CertFile: caFile(t, srv)},
		"key without cert": {Timeout: time.Second, KeyFile: bad},
	}
	for name, o := range cases {
		if _, err := NewClient(o); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestInsecureSkipsCAFile(t *testing.T) {
	o := Options{Timeout: time.Second, InsecureSkipVerify: true, CAFile: filepath.Join(t.TempDir(), "absent.pem")}
	if _, err := NewClient(o); err != nil {
		t.Fatalf("CA file read despite insecure_skip_verify: %v", err)
	}
}

func TestClientCertificateIsPresented(t *testing.T) {
	var seen int
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = len(r.TLS.PeerCertificates)
		w.WriteHeader(http.StatusNoContent)
	}))
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	cert := srv.TLS.Certificates[0]
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0o600); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}

	o := Options{Timeout: 5 * time.Second, InsecureSkipVerify: true, CertFile: certPath, KeyFile: keyPath}
	if err := get(t, o, srv.URL); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if seen != 1 {
		t.Fatalf("server saw %d client certificates, want 1", seen)
	}
}

func TestTransportPolicy(t *testing.T) {
	def, err := NewTransport(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if def.Proxy == nil || def.DisableKeepAlives || !def.ForceAttemptHTTP2 {
		t.Error("zero options must honour proxy env, keep alive, and allow HTTP/2")
	}
	if def.TLSClientConfig.InsecureSkipVerify {
		t.Error("zero options must verify certificates")
	}

	strict, err := NewTransport(Options{DisableProxy: true, DisableKeepAlives: true, DisableHTTP2: true})
	if err != nil {
		t.Fatal(err)
	}
	if strict.Proxy != nil || !strict.DisableKeepAlives || strict.ForceAttemptHTTP2 || strict.TLSNextProto == nil {
		t.Error("disable flags not applied")
	}
}

func TestNoRedirectReturnsFirstResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/next", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{Timeout: 5 * time.Second, NoRedirect: true})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want 302", resp.StatusCode)
	}
}
