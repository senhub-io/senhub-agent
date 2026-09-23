package psk

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	return k
}

// pair runs a client and a server over a socket pair and returns both
// ends, or the errors they failed with.
func pair(t *testing.T, clientCfg, serverCfg Config) (net.Conn, net.Conn, error, error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	type result struct {
		c   net.Conn
		err error
	}
	serverDone := make(chan result, 1)
	go func() {
		raw, acceptErr := ln.Accept()
		if acceptErr != nil {
			serverDone <- result{err: acceptErr}
			return
		}
		c, hErr := Server(raw, serverCfg)
		serverDone <- result{c: c, err: hErr}
	}()

	raw, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	clientConn, clientErr := Client(raw, clientCfg)
	s := <-serverDone
	return clientConn, s.c, clientErr, s.err
}

func TestHandshakeCarriesApplicationDataBothWays(t *testing.T) {
	key := testKey(t)
	cfg := Config{Identity: "senhub", Key: key, Deadline: 5 * time.Second}

	c, s, cErr, sErr := pair(t, cfg, cfg)
	if cErr != nil || sErr != nil {
		t.Fatalf("handshake failed: client=%v server=%v", cErr, sErr)
	}
	defer c.Close()
	defer s.Close()

	// Client to server, then the other way, so both directions' keys and
	// sequence numbers are exercised rather than assumed symmetric.
	if _, err := c.Write([]byte("ZBXD request")); err != nil {
		t.Fatalf("client write: %v", err)
	}
	got := make([]byte, 12)
	if _, err := io.ReadFull(s, got); err != nil {
		t.Fatalf("server read: %v", err)
	}
	if string(got) != "ZBXD request" {
		t.Errorf("server read %q", got)
	}

	if _, err := s.Write([]byte("response")); err != nil {
		t.Fatalf("server write: %v", err)
	}
	got = make([]byte, 8)
	if _, err := io.ReadFull(c, got); err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(got) != "response" {
		t.Errorf("client read %q", got)
	}
}

// A record longer than one TLS record proves the write path splits and
// the read path reassembles, which a short exchange never touches.
func TestPayloadLargerThanOneRecord(t *testing.T) {
	key := testKey(t)
	cfg := Config{Identity: "senhub", Key: key, Deadline: 10 * time.Second}
	c, s, cErr, sErr := pair(t, cfg, cfg)
	if cErr != nil || sErr != nil {
		t.Fatalf("handshake failed: client=%v server=%v", cErr, sErr)
	}
	defer c.Close()
	defer s.Close()

	payload := bytes.Repeat([]byte("senhub"), maxPlaintext/3)
	go func() { _, _ = c.Write(payload) }()

	got := make([]byte, len(payload))
	if _, err := io.ReadFull(s, got); err != nil {
		t.Fatalf("server read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Error("the payload did not survive the round trip")
	}
}

func TestAMismatchedKeyIsRefused(t *testing.T) {
	c, s, cErr, sErr := pair(t,
		Config{Identity: "senhub", Key: testKey(t), Deadline: 5 * time.Second},
		Config{Identity: "senhub", Key: testKey(t), Deadline: 5 * time.Second})
	if c != nil {
		c.Close()
	}
	if s != nil {
		s.Close()
	}
	if cErr == nil && sErr == nil {
		t.Fatal("two different keys completed a handshake")
	}
	// The failure must name the key rather than read as a network error,
	// because "it does not connect" is what an operator will report.
	joined := errText(cErr) + " " + errText(sErr)
	if !strings.Contains(joined, "key") && !strings.Contains(joined, "authenticate") {
		t.Errorf("failure does not point at the key: client=%v server=%v", cErr, sErr)
	}
}

func TestAMismatchedIdentityIsRefused(t *testing.T) {
	key := testKey(t)
	c, s, _, sErr := pair(t,
		Config{Identity: "senhub", Key: key, Deadline: 5 * time.Second},
		Config{Identity: "another", Key: key, Deadline: 5 * time.Second})
	if c != nil {
		c.Close()
	}
	if s != nil {
		s.Close()
	}
	if sErr == nil {
		t.Fatal("the listener accepted an identity it has no key for")
	}
	if !strings.Contains(sErr.Error(), "identity") {
		t.Errorf("server error does not name the identity: %v", sErr)
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestConfigRefusesWhatZabbixWouldReject(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"no identity", Config{Key: make([]byte, 32)}, "identity"},
		{"key too short", Config{Identity: "senhub", Key: make([]byte, 8)}, "at least"},
		{"key too long", Config{Identity: "senhub", Key: make([]byte, 512)}, "more than"},
		{"identity too long", Config{Identity: strings.Repeat("x", 200), Key: make([]byte, 32)}, "identity is"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.validate()
			if err == nil {
				t.Fatal("accepted a configuration Zabbix would refuse")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// Both suites must produce a working session: they differ in PRF hash,
// key length and transcript hash together, so one working says nothing
// about the other.
func TestBothCipherSuitesCompleteAHandshake(t *testing.T) {
	saved := offered
	defer func() { offered = saved }()

	for _, s := range []suite{suiteAES128, suiteAES256} {
		t.Run(hex.EncodeToString([]byte{byte(s.id >> 8), byte(s.id)}), func(t *testing.T) {
			offered = []suite{s}
			key := testKey(t)
			cfg := Config{Identity: "senhub", Key: key, Deadline: 5 * time.Second}
			c, srv, cErr, sErr := pair(t, cfg, cfg)
			if cErr != nil || sErr != nil {
				t.Fatalf("handshake failed: client=%v server=%v", cErr, sErr)
			}
			defer c.Close()
			defer srv.Close()

			if _, err := c.Write([]byte("ping")); err != nil {
				t.Fatalf("write: %v", err)
			}
			got := make([]byte, 4)
			if _, err := io.ReadFull(srv, got); err != nil {
				t.Fatalf("read: %v", err)
			}
			if string(got) != "ping" {
				t.Errorf("read %q", got)
			}
		})
	}
}

// The pre-master secret is the one place a transcription slip would
// still interoperate with ourselves while failing against every real
// server, so it is pinned to the shape RFC 4279 states.
func TestPreMasterSecretFollowsRFC4279(t *testing.T) {
	got := preMasterSecret([]byte{0xAA, 0xBB, 0xCC})
	want := []byte{0, 3, 0, 0, 0, 0, 3, 0xAA, 0xBB, 0xCC}
	if !bytes.Equal(got, want) {
		t.Errorf("pre-master secret = % x; want % x", got, want)
	}
}

func TestTamperedRecordDoesNotOpen(t *testing.T) {
	key := testKey(t)
	cfg := Config{Identity: "senhub", Key: key, Deadline: 5 * time.Second}
	c, s, cErr, sErr := pair(t, cfg, cfg)
	if cErr != nil || sErr != nil {
		t.Fatalf("handshake failed: client=%v server=%v", cErr, sErr)
	}
	defer c.Close()
	defer s.Close()

	// Write a record by hand with one byte of ciphertext flipped.
	inner := c.(*conn)
	sealed, err := inner.seal(recordApplicationData, []byte("senhub"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	sealed[len(sealed)-1] ^= 0xFF
	header := []byte{recordApplicationData, 3, 3, byte(len(sealed) >> 8), byte(len(sealed))}
	if _, err := inner.Conn.Write(append(header, sealed...)); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, 16)
	if _, err := s.Read(buf); err == nil {
		t.Fatal("a tampered record was accepted")
	} else if !strings.Contains(err.Error(), "authenticate") {
		t.Errorf("error does not say the record failed to authenticate: %v", err)
	}
}

func TestCloseIsOrderly(t *testing.T) {
	key := testKey(t)
	cfg := Config{Identity: "senhub", Key: key, Deadline: 5 * time.Second}
	c, s, cErr, sErr := pair(t, cfg, cfg)
	if cErr != nil || sErr != nil {
		t.Fatalf("handshake failed: client=%v server=%v", cErr, sErr)
	}
	defer s.Close()

	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := s.Read(buf); !errors.Is(err, io.EOF) {
		t.Errorf("peer saw %v rather than an orderly end of stream", err)
	}
}
