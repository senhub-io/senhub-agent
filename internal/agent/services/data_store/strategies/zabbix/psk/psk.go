// Package psk speaks the pre-shared-key profile of TLS 1.2 that Zabbix
// uses, which Go's standard library deliberately leaves out.
//
// Why this exists at all: Zabbix encrypts with certificates or with a
// pre-shared key, and PSK is what most installations reach for, because
// it needs no certificate authority. It is also the ONLY encryption
// Zabbix offers for autoregistration — that setting takes "none", "PSK"
// or both, and refuses a certificate — so without PSK an agent cannot
// register itself on a site that encrypts that step.
//
// Why writing it is reasonable rather than reckless: the profile is the
// smallest thing in TLS 1.2. One cipher suite family, no certificate, no
// chain to validate, no X.509 to parse, no resumption, no renegotiation,
// no client-auth branch. What remains is a key schedule from the
// standard PRF and an AEAD from crypto/cipher. The classes of defect
// that make hand-rolled TLS dangerous are mostly absent because the
// code paths that carry them do not exist here.
//
// Scope, deliberately closed:
//
//   - TLS 1.2 only. A server that insists on TLS 1.3 external PSK is not
//     served; both Zabbix lines accept this profile, measured on 7.0.30
//     and 8.0.0.
//   - TLS_PSK_WITH_AES_128_GCM_SHA256 and TLS_PSK_WITH_AES_256_GCM_SHA384
//     (RFC 5487), which is what Zabbix's own default cipher list offers.
//   - No PSK identity hint is acted on: the identity is configured, and
//     picking a key from a server-sent hint would let the peer choose
//     which secret we prove.
package psk

import (
	"errors"
	"fmt"
	"net"
	"time"
)

// Config is what both ends need. The key never appears in a
// configuration file: it is read from a file of its own, the way Zabbix
// does it, so its permissions carry the protection.
type Config struct {
	// Identity is sent in clear. It names which key is meant, it is not
	// a secret.
	Identity string

	// Key is the pre-shared key itself, already decoded from the hex
	// spelling Zabbix uses.
	Key []byte

	// Deadline bounds the handshake. Zero leaves whatever the caller set
	// on the connection.
	Deadline time.Duration
}

func (c Config) validate() error {
	if c.Identity == "" {
		return errors.New("psk: an identity is required; Zabbix matches the key by it")
	}
	if len(c.Identity) > maxIdentityLen {
		return fmt.Errorf("psk: identity is %d bytes, the protocol allows %d", len(c.Identity), maxIdentityLen)
	}
	// Zabbix refuses anything shorter than 32 hex characters, i.e. 16
	// bytes. Accepting less here would encrypt with a key the server
	// would have rejected, which reads as working until it does not.
	if len(c.Key) < minKeyLen {
		return fmt.Errorf("psk: key is %d bytes, Zabbix requires at least %d", len(c.Key), minKeyLen)
	}
	if len(c.Key) > maxKeyLen {
		return fmt.Errorf("psk: key is %d bytes, more than the %d this profile carries", len(c.Key), maxKeyLen)
	}
	return nil
}

const (
	minKeyLen      = 16
	maxKeyLen      = 256
	maxIdentityLen = 128
)

// Client runs the handshake as the side that connects, and returns a
// net.Conn carrying application data. The returned connection owns the
// one passed in: closing it closes the underlying socket.
func Client(raw net.Conn, cfg Config) (net.Conn, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return handshake(raw, cfg, roleClient)
}

// Server runs the handshake as the side that accepts. It is the same
// state machine mirrored, for the polled port.
func Server(raw net.Conn, cfg Config) (net.Conn, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return handshake(raw, cfg, roleServer)
}
