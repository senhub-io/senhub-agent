package redis

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// writeTestCertKey generates a self-signed certificate and writes the
// PEM-encoded certificate and private key into dir.
func writeTestCertKey(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "redis-probe-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}

	certPath = filepath.Join(dir, "client.crt")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("writing cert: %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshaling key: %v", err)
	}
	keyPath = filepath.Join(dir, "client.key")
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("writing key: %v", err)
	}
	return certPath, keyPath
}

// TestParseConfig_TLSFiles verifies the three mTLS path fields are parsed.
func TestParseConfig_TLSFiles(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"tls":           true,
		"tls_cert_file": "/etc/redis/client.crt",
		"tls_key_file":  "/etc/redis/client.key",
		"tls_ca_file":   "/etc/redis/ca.pem",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TLSCertFile != "/etc/redis/client.crt" {
		t.Errorf("TLSCertFile = %q, want /etc/redis/client.crt", cfg.TLSCertFile)
	}
	if cfg.TLSKeyFile != "/etc/redis/client.key" {
		t.Errorf("TLSKeyFile = %q, want /etc/redis/client.key", cfg.TLSKeyFile)
	}
	if cfg.TLSCAFile != "/etc/redis/ca.pem" {
		t.Errorf("TLSCAFile = %q, want /etc/redis/ca.pem", cfg.TLSCAFile)
	}
}

// TestParseConfig_TLSCertWithoutKey verifies that supplying only one half
// of the client certificate pair is rejected instead of silently ignored.
func TestParseConfig_TLSCertWithoutKey(t *testing.T) {
	cases := []map[string]interface{}{
		{"tls": true, "tls_cert_file": "/etc/redis/client.crt"},
		{"tls": true, "tls_key_file": "/etc/redis/client.key"},
	}
	for _, raw := range cases {
		if _, err := parseConfig(raw); err == nil {
			t.Errorf("parseConfig(%v): expected error, got nil", raw)
		}
	}
}

// TestTLSClientConfig_ClientCertAndCA verifies that cert+key populate
// tls.Config.Certificates and that tls_ca_file populates RootCAs.
func TestTLSClientConfig_ClientCertAndCA(t *testing.T) {
	certPath, keyPath := writeTestCertKey(t, t.TempDir())

	cfg := probeConfig{
		Host:        "redis.example.com",
		TLS:         true,
		TLSCertFile: certPath,
		TLSKeyFile:  keyPath,
		TLSCAFile:   certPath,
	}
	tlsCfg, err := cfg.tlsClientConfig()
	if err != nil {
		t.Fatalf("tlsClientConfig: %v", err)
	}
	if len(tlsCfg.Certificates) != 1 {
		t.Errorf("Certificates: want 1 entry, got %d", len(tlsCfg.Certificates))
	}
	if tlsCfg.RootCAs == nil {
		t.Error("RootCAs should be populated from tls_ca_file")
	}
	if tlsCfg.ServerName != "redis.example.com" {
		t.Errorf("ServerName = %q, want redis.example.com", tlsCfg.ServerName)
	}
}

// TestTLSClientConfig_NoClientMaterial verifies back-compat: without the
// new fields the tls.Config carries neither Certificates nor RootCAs.
func TestTLSClientConfig_NoClientMaterial(t *testing.T) {
	cfg := probeConfig{Host: "10.0.0.1", TLS: true}
	tlsCfg, err := cfg.tlsClientConfig()
	if err != nil {
		t.Fatalf("tlsClientConfig: %v", err)
	}
	if len(tlsCfg.Certificates) != 0 {
		t.Errorf("Certificates: want none, got %d", len(tlsCfg.Certificates))
	}
	if tlsCfg.RootCAs != nil {
		t.Error("RootCAs should be nil without tls_ca_file (system pool)")
	}
}

// TestTLSClientConfig_BadFiles verifies that unreadable or invalid PEM
// material yields an error instead of a half-configured tls.Config.
func TestTLSClientConfig_BadFiles(t *testing.T) {
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.pem")
	if err := os.WriteFile(garbage, []byte("not a pem"), 0o600); err != nil {
		t.Fatalf("writing garbage file: %v", err)
	}

	cases := []probeConfig{
		{TLS: true, TLSCertFile: filepath.Join(dir, "missing.crt"), TLSKeyFile: filepath.Join(dir, "missing.key")},
		{TLS: true, TLSCAFile: filepath.Join(dir, "missing-ca.pem")},
		{TLS: true, TLSCAFile: garbage},
	}
	for i, cfg := range cases {
		if _, err := cfg.tlsClientConfig(); err == nil {
			t.Errorf("case %d: expected error, got nil", i)
		}
	}
}

// TestNewRedisProbe_TLSClientCert verifies the construction path: a probe
// configured with tls + cert/key gets a tls.Config with the client
// certificate loaded, and a broken path fails construction.
func TestNewRedisProbe_TLSClientCert(t *testing.T) {
	certPath, keyPath := writeTestCertKey(t, t.TempDir())
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})

	p, err := NewRedisProbe(map[string]interface{}{
		"tls":           true,
		"tls_cert_file": certPath,
		"tls_key_file":  keyPath,
	}, baseLogger)
	if err != nil {
		t.Fatalf("NewRedisProbe: %v", err)
	}
	rp := p.(*redisProbe)
	if rp.tlsConfig == nil || len(rp.tlsConfig.Certificates) != 1 {
		t.Errorf("probe tlsConfig should carry 1 client certificate, got %+v", rp.tlsConfig)
	}

	if _, err := NewRedisProbe(map[string]interface{}{
		"tls":           true,
		"tls_cert_file": certPath,
		"tls_key_file":  filepath.Join(t.TempDir(), "missing.key"),
	}, baseLogger); err == nil {
		t.Error("NewRedisProbe with a missing key file should fail construction")
	}
}
