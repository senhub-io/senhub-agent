package configuration

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
	"strings"
	"testing"
	"time"
)

func newTestCertFile(t *testing.T) string {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	serial := big.NewInt(1)
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "opamp-config-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if pemBytes == nil {
		t.Fatal("failed to encode certificate PEM")
	}

	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pemBytes, 0644); err != nil {
		t.Fatal(err)
	}

	return path
}

func hasErrPrefix(errs []error, prefix string) bool {
	for _, err := range errs {
		if strings.HasPrefix(err.Error(), prefix) {
			return true
		}
	}
	return false
}

func hasErrContains(errs []error, substr string) bool {
	for _, err := range errs {
		if strings.Contains(err.Error(), substr) {
			return true
		}
	}
	return false
}

func TestValidateOpAMP(t *testing.T) {
	missingCA := filepath.Join(t.TempDir(), "missing-ca.pem")
	invalidCA := filepath.Join(t.TempDir(), "invalid-ca.pem")
	if err := os.WriteFile(invalidCA, []byte("not a pem"), 0644); err != nil {
		t.Fatal(err)
	}
	validCA := newTestCertFile(t)

	cases := []struct {
		name         string
		cfg          *OpAMPConfig
		wantErrCount int
		wantPrefixes []string
		wantContains []string
		check        func(*testing.T, *OpAMPConfig, []error)
	}{
		{
			name:         "nil config",
			cfg:          nil,
			wantErrCount: 0,
		},
		{
			name: "disabled ignores garbage",
			cfg: &OpAMPConfig{
				Enabled:           false,
				Endpoint:          "wss://h/v1/opamp",
				Token:             "",
				Transport:         "grpc",
				HeartbeatInterval: "abc",
				TLS:               OpAMPTLSConfig{CAFile: missingCA},
			},
			wantErrCount: 0,
		},
		{
			name: "valid websocket default",
			cfg: &OpAMPConfig{
				Enabled:  true,
				Endpoint: "wss://opamp.example.com/v1/opamp",
				Token:    "alid_a_x",
			},
			wantErrCount: 0,
			check: func(t *testing.T, c *OpAMPConfig, errs []error) {
				norm := c.Normalized()
				if norm.Transport != OpAMPTransportWebSocket {
					t.Fatalf("Normalized().Transport = %q, want %q", norm.Transport, OpAMPTransportWebSocket)
				}

				d, err := norm.Heartbeat()
				if err != nil {
					t.Fatalf("Heartbeat() unexpected error: %v", err)
				}
				if d != DefaultOpAMPHeartbeatInterval {
					t.Fatalf("Heartbeat() = %s, want %s", d, DefaultOpAMPHeartbeatInterval)
				}
			},
		},
		{
			name: "valid http explicit",
			cfg: &OpAMPConfig{
				Enabled:   true,
				Endpoint:  "https://opamp.example.com/v1/opamp",
				Token:     "t",
				Transport: "http",
			},
			wantErrCount: 0,
		},
		{
			name: "transport normalized",
			cfg: &OpAMPConfig{
				Enabled:   true,
				Endpoint:  "https://opamp.example.com/v1/opamp",
				Token:     "t",
				Transport: "HTTP ",
			},
			wantErrCount: 0,
			check: func(t *testing.T, c *OpAMPConfig, errs []error) {
				if got := c.Normalized().Transport; got != OpAMPTransportHTTP {
					t.Fatalf("Normalized().Transport = %q, want %q", got, OpAMPTransportHTTP)
				}
			},
		},
		{
			name: "empty endpoint and token",
			cfg: &OpAMPConfig{
				Enabled: true,
			},
			wantErrCount: 2,
			wantPrefixes: []string{"opamp.endpoint", "opamp.token"},
		},
		{
			name: "websocket with https endpoint",
			cfg: &OpAMPConfig{
				Enabled:   true,
				Endpoint:  "https://h/v1/opamp",
				Token:     "t",
				Transport: "websocket",
			},
			wantErrCount: 1,
			wantPrefixes: []string{`opamp.endpoint: scheme "https"`},
			wantContains: []string{"does not match transport websocket"},
		},
		{
			name: "http with wss endpoint",
			cfg: &OpAMPConfig{
				Enabled:   true,
				Endpoint:  "wss://h/v1/opamp",
				Token:     "t",
				Transport: "http",
			},
			wantErrCount: 1,
			wantPrefixes: []string{`opamp.endpoint: scheme "wss"`},
			wantContains: []string{"does not match transport http"},
		},
		{
			name: "unsupported transport",
			cfg: &OpAMPConfig{
				Enabled:   true,
				Endpoint:  "wss://opamp.example.com/v1/opamp",
				Token:     "t",
				Transport: "grpc",
			},
			wantErrCount: 1,
			wantPrefixes: []string{"opamp.transport"},
		},
		{
			name: "clear remote endpoint refused",
			cfg: &OpAMPConfig{
				Enabled:  true,
				Endpoint: "ws://opamp.example.com/v1/opamp",
				Token:    "t",
			},
			wantErrCount: 1,
			wantContains: []string{"sends the token in clear"},
		},
		{
			name: "loopback ipv4",
			cfg: &OpAMPConfig{
				Enabled:   true,
				Endpoint:  "ws://127.0.0.1:4320/v1/opamp",
				Token:     "t",
				Transport: "websocket",
			},
			wantErrCount: 0,
		},
		{
			name: "loopback localhost",
			cfg: &OpAMPConfig{
				Enabled:   true,
				Endpoint:  "ws://localhost:4320/v1/opamp",
				Token:     "t",
				Transport: "websocket",
			},
			wantErrCount: 0,
		},
		{
			name: "loopback ipv6",
			cfg: &OpAMPConfig{
				Enabled:   true,
				Endpoint:  "ws://[::1]:4320/v1/opamp",
				Token:     "t",
				Transport: "websocket",
			},
			wantErrCount: 0,
		},
		{
			name: "loopback localhost uppercase",
			cfg: &OpAMPConfig{
				Enabled:   true,
				Endpoint:  "http://LOCALHOST/v1/opamp",
				Token:     "t",
				Transport: "http",
			},
			wantErrCount: 0,
		},
		{
			name: "plaintext remote allowed by insecure skip verify",
			cfg: &OpAMPConfig{
				Enabled:  true,
				Endpoint: "ws://opamp.example.com/v1/opamp",
				Token:    "t",
				TLS:      OpAMPTLSConfig{InsecureSkipVerify: true},
			},
			wantErrCount: 0,
		},
		{
			name: "heartbeat 10s",
			cfg: &OpAMPConfig{
				Enabled:           true,
				Endpoint:          "wss://opamp.example.com/v1/opamp",
				Token:             "t",
				HeartbeatInterval: "10s",
			},
			wantErrCount: 0,
			check: func(t *testing.T, c *OpAMPConfig, errs []error) {
				d, err := c.Normalized().Heartbeat()
				if err != nil {
					t.Fatalf("Heartbeat() unexpected error: %v", err)
				}
				if d != 10*time.Second {
					t.Fatalf("Heartbeat() = %s, want %s", d, 10*time.Second)
				}
			},
		},
		{
			name: "heartbeat too short",
			cfg: &OpAMPConfig{
				Enabled:           true,
				Endpoint:          "wss://opamp.example.com/v1/opamp",
				Token:             "t",
				HeartbeatInterval: "500ms",
			},
			wantErrCount: 1,
			wantPrefixes: []string{"opamp.heartbeat_interval"},
		},
		{
			name: "heartbeat invalid",
			cfg: &OpAMPConfig{
				Enabled:           true,
				Endpoint:          "wss://opamp.example.com/v1/opamp",
				Token:             "t",
				HeartbeatInterval: "abc",
			},
			wantErrCount: 1,
			wantPrefixes: []string{"opamp.heartbeat_interval"},
		},
		{
			name: "missing ca file",
			cfg: &OpAMPConfig{
				Enabled:  true,
				Endpoint: "wss://opamp.example.com/v1/opamp",
				Token:    "t",
				TLS:      OpAMPTLSConfig{CAFile: missingCA},
			},
			wantErrCount: 1,
			wantPrefixes: []string{"opamp.tls.ca_file"},
		},
		{
			name: "invalid ca file",
			cfg: &OpAMPConfig{
				Enabled:  true,
				Endpoint: "wss://opamp.example.com/v1/opamp",
				Token:    "t",
				TLS:      OpAMPTLSConfig{CAFile: invalidCA},
			},
			wantErrCount: 1,
			wantPrefixes: []string{"opamp.tls.ca_file"},
			wantContains: []string{"no certificate parsed"},
		},
		{
			name: "valid ca file",
			cfg: &OpAMPConfig{
				Enabled:  true,
				Endpoint: "wss://opamp.example.com/v1/opamp",
				Token:    "t",
				TLS:      OpAMPTLSConfig{CAFile: validCA},
			},
			wantErrCount: 0,
		},
		{
			name: "does not leak secrets",
			cfg: &OpAMPConfig{
				Enabled:           true,
				Endpoint:          "wss://user:pw@h/v1/opamp?k=SECRETQ",
				Token:             "alid_a_SECRET123",
				Transport:         "http",
				HeartbeatInterval: "abc",
				TLS:               OpAMPTLSConfig{CAFile: missingCA},
			},
			wantErrCount: 3,
			check: func(t *testing.T, c *OpAMPConfig, errs []error) {
				msgs := make([]string, 0, len(errs))
				for _, err := range errs {
					msgs = append(msgs, err.Error())
				}
				joined := strings.Join(msgs, "\n")

				for _, secret := range []string{"alid_a_SECRET123", "pw@", "SECRETQ"} {
					if strings.Contains(joined, secret) {
						t.Errorf("joined error messages unexpectedly contain %q: %s", secret, joined)
					}
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateOpAMP(tc.cfg)
			if len(got) != tc.wantErrCount {
				t.Fatalf("ValidateOpAMP() returned %d errors, want %d: %v", len(got), tc.wantErrCount, got)
			}

			for _, prefix := range tc.wantPrefixes {
				if !hasErrPrefix(got, prefix) {
					t.Errorf("ValidateOpAMP() errors do not contain an error with prefix %q: %v", prefix, got)
				}
			}

			for _, substr := range tc.wantContains {
				if !hasErrContains(got, substr) {
					t.Errorf("ValidateOpAMP() errors do not contain %q: %v", substr, got)
				}
			}

			if tc.check != nil {
				tc.check(t, tc.cfg, got)
			}
		})
	}
}

func TestOpAMPConfigHeartbeat(t *testing.T) {
	cases := []struct {
		name          string
		cfg           OpAMPConfig
		want          time.Duration
		wantErrPrefix string
	}{
		{
			name: "default",
			cfg:  OpAMPConfig{},
			want: DefaultOpAMPHeartbeatInterval,
		},
		{
			name: "10s",
			cfg:  OpAMPConfig{HeartbeatInterval: "10s"},
			want: 10 * time.Second,
		},
		{
			name:          "500ms",
			cfg:           OpAMPConfig{HeartbeatInterval: "500ms"},
			wantErrPrefix: "opamp.heartbeat_interval",
		},
		{
			name:          "abc",
			cfg:           OpAMPConfig{HeartbeatInterval: "abc"},
			wantErrPrefix: "opamp.heartbeat_interval",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.cfg.Heartbeat()
			if tc.wantErrPrefix != "" {
				if err == nil || !strings.HasPrefix(err.Error(), tc.wantErrPrefix) {
					t.Fatalf("Heartbeat() error = %v, want prefix %q", err, tc.wantErrPrefix)
				}
				return
			}

			if err != nil {
				t.Fatalf("Heartbeat() unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Heartbeat() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestOpAMPConfigPlaintextAllowed(t *testing.T) {
	cases := []struct {
		name string
		cfg  OpAMPConfig
		want bool
	}{
		{
			name: "remote false",
			cfg:  OpAMPConfig{Endpoint: "ws://opamp.example.com"},
			want: false,
		},
		{
			name: "localhost true",
			cfg:  OpAMPConfig{Endpoint: "ws://localhost:4320"},
			want: true,
		},
		{
			name: "ipv4 loopback true",
			cfg:  OpAMPConfig{Endpoint: "ws://127.0.0.1:4320"},
			want: true,
		},
		{
			name: "ipv6 loopback true",
			cfg:  OpAMPConfig{Endpoint: "ws://[::1]:4320"},
			want: true,
		},
		{
			name: "insecure true",
			cfg:  OpAMPConfig{Endpoint: "ws://opamp.example.com", TLS: OpAMPTLSConfig{InsecureSkipVerify: true}},
			want: true,
		},
		{
			name: "unparseable false",
			cfg:  OpAMPConfig{Endpoint: "://bad", TLS: OpAMPTLSConfig{InsecureSkipVerify: true}},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.PlaintextAllowed(); got != tc.want {
				t.Fatalf("PlaintextAllowed() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRedactEndpoint(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "masks password and drops query fragment",
			in:   "wss://user:pw@h:1/v1/opamp?token=abc#f",
			want: "wss://user:xxxxx@h:1/v1/opamp",
		},
		{
			name: "unparseable",
			in:   "://bad",
			want: "(unparseable endpoint)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RedactEndpoint(tc.in); got != tc.want {
				t.Fatalf("RedactEndpoint(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
