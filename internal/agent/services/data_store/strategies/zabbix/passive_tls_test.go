package zabbix

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

// writeSelfSigned puts a certificate and its key on disk and returns
// their paths, the way an operator would.
func writeSelfSigned(t *testing.T, dir, name string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath = filepath.Join(dir, name+".crt")
	keyPath = filepath.Join(dir, name+".key")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func passiveTLSConfig(t *testing.T, tlsCfg PassiveTLSConfig) Config {
	t.Helper()
	cfg := testConfig("127.0.0.1:10051")
	cfg.Passive = PassiveConfig{
		Enabled: true, BindAddress: "127.0.0.1", Port: 0,
		Allow: []string{"127.0.0.0/8"}, TLS: tlsCfg,
	}
	return cfg
}

func TestThePolledPortIsEncryptedWhenAsked(t *testing.T) {
	dir := t.TempDir()
	cert, key := writeSelfSigned(t, dir, "agent")

	pl, err := newPassiveListener(passiveTLSConfig(t, PassiveTLSConfig{
		Enabled: true, CertFile: cert, KeyFile: key,
	}), func(string) (string, bool) { return "", false }, logger.NewModuleLogger(testLogger(), "test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := pl.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer pl.stop()

	conn, err := tls.Dial("tcp", pl.addr(), &tls.Config{RootCAs: poolOf(t, cert), ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatalf("the listener refused an encrypted poll: %v", err)
	}
	defer conn.Close()
	if err := conn.Handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	// The same plain dialect the other tests use, carried over TLS.
	if _, err := conn.Write([]byte("agent.ping\n")); err != nil {
		t.Fatal(err)
	}
	raw, err := readFrame(conn)
	if err != nil {
		t.Fatalf("reading the reply: %v", err)
	}
	if strings.TrimSpace(string(raw)) != "1" {
		t.Fatalf("agent.ping answered %q over TLS", raw)
	}
}

func TestAPlainPollIsRefusedOnAnEncryptedPort(t *testing.T) {
	dir := t.TempDir()
	cert, key := writeSelfSigned(t, dir, "agent")

	pl, err := newPassiveListener(passiveTLSConfig(t, PassiveTLSConfig{
		Enabled: true, CertFile: cert, KeyFile: key,
	}), func(string) (string, bool) { return "", false }, logger.NewModuleLogger(testLogger(), "test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := pl.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer pl.stop()

	conn, err := net.DialTimeout("tcp", pl.addr(), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	// The bytes go out, the handshake fails, and nothing readable comes
	// back: an encrypted port must not answer a poll in the clear.
	_, _ = conn.Write([]byte("agent.ping\n"))
	if raw, err := readFrame(conn); err == nil {
		t.Fatalf("a plain poll was answered with %q on an encrypted port", raw)
	}
}

func TestAnAuthorityMakesTheAgentDemandOneBack(t *testing.T) {
	dir := t.TempDir()
	cert, key := writeSelfSigned(t, dir, "agent")
	otherCert, _ := writeSelfSigned(t, dir, "stranger")

	// The agent trusts only the stranger's authority, so its own
	// certificate does not let it in: a poller must present one signed
	// by what ca_file names.
	pl, err := newPassiveListener(passiveTLSConfig(t, PassiveTLSConfig{
		Enabled: true, CertFile: cert, KeyFile: key, CAFile: otherCert,
	}), func(string) (string, bool) { return "", false }, logger.NewModuleLogger(testLogger(), "test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := pl.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer pl.stop()

	conn, err := tls.Dial("tcp", pl.addr(), &tls.Config{RootCAs: poolOf(t, cert), ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12})
	if err == nil {
		defer conn.Close()
		// Under TLS 1.3 the server's refusal reaches the client on the
		// first exchange rather than during the handshake, so the poll
		// has to be attempted for the rejection to surface.
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		if _, werr := conn.Write([]byte("agent.ping\n")); werr != nil {
			err = werr
		} else if _, rerr := readFrame(conn); rerr != nil {
			err = rerr
		}
	}
	if err == nil {
		t.Fatal("a poller with no certificate was let in although an authority is configured")
	}
}

func TestACertificateThatCannotBeReadStopsTheAgent(t *testing.T) {
	_, err := newPassiveListener(passiveTLSConfig(t, PassiveTLSConfig{
		Enabled: true, CertFile: "/nowhere/agent.crt", KeyFile: "/nowhere/agent.key",
	}), func(string) (string, bool) { return "", false }, logger.NewModuleLogger(testLogger(), "test"))
	if err == nil {
		t.Fatal("the listener started without the certificate it was told to present, which means it served in clear")
	}
	if !strings.Contains(err.Error(), "cert_file") {
		t.Errorf("error = %v; it should name what could not be read", err)
	}
}

func TestEncryptingThePolledPortNeedsACertificate(t *testing.T) {
	_, err := parsePassiveTLS(map[string]interface{}{"enabled": true})
	if err == nil {
		t.Fatal("passive.tls.enabled alone was accepted; the listener would have served in clear")
	}
	if !strings.Contains(err.Error(), "cert_file") {
		t.Errorf("error = %v; it should say what is missing", err)
	}
	if _, err := parsePassiveTLS(map[string]interface{}{"enabled": false}); err != nil {
		t.Errorf("a disabled block needs no certificate: %v", err)
	}
}

// poolOf trusts the certificate this test wrote, so the client verifies
// the agent properly instead of skipping the check.
func poolOf(t *testing.T, certPath string) *x509.CertPool {
	t.Helper()
	raw, err := os.ReadFile(certPath) // #nosec G304 - a path this test just wrote
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		t.Fatalf("%s holds no certificate", certPath)
	}
	return pool
}
