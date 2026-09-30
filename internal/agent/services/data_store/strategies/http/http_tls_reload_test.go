package http

import (
	"context"
	"crypto/tls"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
)

// Adding or removing the tls block at runtime logged a successful update
// while the listener kept its old protocol until a bind change or a
// restart: plain HTTP after enabling TLS, HTTPS after removing it.
func TestUpdateConfiguration_TLSToggleSwitchesServedProtocol(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "agent-cert.pem")
	keyFile := filepath.Join(dir, "agent-key.pem")
	if err := configuration.EnsureSelfSignedCert(certFile, keyFile, []string{"127.0.0.1"}); err != nil {
		t.Fatalf("generating test certificate: %v", err)
	}

	port := reservePort(t)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	plain := map[string]interface{}{
		"port":         port,
		"bind_address": "127.0.0.1",
	}
	withTLS := map[string]interface{}{
		"port":         port,
		"bind_address": "127.0.0.1",
		"tls": map[string]interface{}{
			"enabled":   true,
			"cert_file": certFile,
			"key_file":  keyFile,
		},
	}

	strategy := newServerTestStrategy(t, port)
	if err := strategy.serverManager.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		_ = strategy.serverManager.Shutdown(context.Background())
	})
	if servesTLS(addr) {
		t.Fatal("server speaks TLS before TLS was configured")
	}

	if err := strategy.UpdateConfiguration(withTLS); err != nil {
		t.Fatalf("enabling TLS: %v", err)
	}
	if !servesTLS(addr) {
		t.Fatal("TLS enabled at runtime, but the listener still serves plain HTTP")
	}

	if err := strategy.UpdateConfiguration(plain); err != nil {
		t.Fatalf("removing TLS: %v", err)
	}
	if servesTLS(addr) {
		t.Fatal("TLS removed at runtime, but the listener still serves HTTPS")
	}
}

func reservePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("releasing the reserved port: %v", err)
	}
	return port
}

// servesTLS reports whether a TLS handshake completes on addr. Start binds
// synchronously, so the connection waits in the backlog until the server
// goroutine accepts it; a plain HTTP server answers the ClientHello with a
// 400 and the handshake fails.
func servesTLS(addr string) bool {
	dialer := &net.Dialer{Timeout: 3 * time.Second, Deadline: time.Now().Add(3 * time.Second)}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // self-signed test certificate
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
