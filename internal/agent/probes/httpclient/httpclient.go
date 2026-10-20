// Package httpclient builds the *http.Client every HTTP-speaking probe
// uses, so that timeout, TLS verification, proxy and connection policy are
// decided in one place.
//
// The zero Options verify server certificates, follow redirects, honour
// the proxy environment (HTTP_PROXY, HTTPS_PROXY, NO_PROXY) and keep
// connections alive, like http.DefaultTransport. Anything less strict is
// an explicit field that a probe sets from its own configuration.
package httpclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// Options describes the client a probe needs. Timeout is the only
// required field.
type Options struct {
	// Timeout bounds a whole request, redirects and body read included.
	Timeout time.Duration

	// InsecureSkipVerify disables server certificate verification. A probe
	// sets it only from an operator-written configuration key. When true,
	// CAFile is not read.
	InsecureSkipVerify bool

	// CAFile is a PEM bundle that replaces the system roots as the trust
	// anchors for the target.
	CAFile string

	// CertFile and KeyFile present a client certificate. Both or neither.
	CertFile string
	KeyFile  string

	// DisableProxy ignores the proxy environment.
	DisableProxy bool

	// DisableKeepAlives opens a fresh connection for every request.
	DisableKeepAlives bool

	// DisableHTTP2 restricts the client to HTTP/1.1.
	DisableHTTP2 bool

	// DialContext replaces the dialer, for targets reached through a unix
	// socket or a named pipe.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)

	// NoRedirect returns the first response instead of following redirects.
	NoRedirect bool

	// Jar stores cookies across requests.
	Jar http.CookieJar
}

// NewTransport builds the transport described by o.
func NewTransport(o Options) (*http.Transport, error) {
	tlsCfg, err := tlsConfig(o)
	if err != nil {
		return nil, err
	}

	t := &http.Transport{
		TLSClientConfig:       tlsCfg,
		DisableKeepAlives:     o.DisableKeepAlives,
		ForceAttemptHTTP2:     !o.DisableHTTP2,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	if !o.DisableProxy {
		t.Proxy = http.ProxyFromEnvironment
	}
	if o.DialContext != nil {
		t.DialContext = o.DialContext
	} else {
		t.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	if o.DisableHTTP2 {
		t.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	}
	return t, nil
}

// NewClient builds the client described by o.
func NewClient(o Options) (*http.Client, error) {
	if o.Timeout <= 0 {
		return nil, errors.New("http client timeout must be positive")
	}
	t, err := NewTransport(o)
	if err != nil {
		return nil, err
	}
	c := &http.Client{Timeout: o.Timeout, Transport: t, Jar: o.Jar}
	if o.NoRedirect {
		c.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return c, nil
}

func tlsConfig(o Options) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if o.InsecureSkipVerify {
		cfg.InsecureSkipVerify = true // #nosec G402 - set only from an explicit operator opt-in
	} else if o.CAFile != "" {
		pem, err := os.ReadFile(o.CAFile)
		if err != nil {
			return nil, fmt.Errorf("reading CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parsing CA file %s: no certificate found", o.CAFile)
		}
		cfg.RootCAs = pool
	}

	if (o.CertFile == "") != (o.KeyFile == "") {
		return nil, errors.New("client certificate needs both cert and key files")
	}
	if o.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(o.CertFile, o.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("loading client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}
