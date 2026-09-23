package psk

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"net"
	"time"
)

const (
	msgClientHello       = 1
	msgServerHello       = 2
	msgServerKeyExchange = 12
	msgServerHelloDone   = 14
	msgClientKeyExchange = 16
	msgFinished          = 20
)

// renegotiationInfo is RFC 5746's empty "secure renegotiation" marker.
// It is sent even though this profile never renegotiates: OpenSSL peers
// refuse a hello without it — measured, zabbix_get answers
// "unsafe legacy renegotiation disabled" and drops the handshake — so an
// extension the code otherwise has no use for is what makes the polled
// port answer a real Zabbix client at all.
var renegotiationInfo = []byte{0xFF, 0x01, 0x00, 0x01, 0x00}

func withExtensions(hello []byte, ext []byte) []byte {
	hello = append(hello, byte(len(ext)>>8), byte(len(ext)))
	return append(hello, ext...)
}

type role int

const (
	roleClient role = iota
	roleServer
)

// handshakeState carries what both roles need while the exchange runs.
type handshakeState struct {
	c          *conn
	cfg        Config
	suite      suite
	transcript hash.Hash
	clientRand []byte
	serverRand []byte
}

func handshake(raw net.Conn, cfg Config, r role) (net.Conn, error) {
	c := &conn{Conn: raw}

	if cfg.Deadline > 0 {
		if err := raw.SetDeadline(time.Now().Add(cfg.Deadline)); err != nil {
			return nil, fmt.Errorf("psk: setting the handshake deadline: %w", err)
		}
		defer func() { _ = raw.SetDeadline(time.Time{}) }()
	}

	hs := &handshakeState{c: c, cfg: cfg}
	var err error
	if r == roleClient {
		err = hs.runClient()
	} else {
		err = hs.runServer()
	}
	if err != nil {
		// A failed handshake tells the peer why before hanging up, which
		// is what turns "it does not work" into a diagnosable message on
		// the other side.
		_ = c.writeRecord(recordAlert, []byte{2, 40})
		_ = raw.Close()
		return nil, err
	}
	return c, nil
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("psk: reading random bytes: %w", err)
	}
	return b, nil
}

func handshakeMessage(typ byte, body []byte) []byte {
	out := make([]byte, 0, 4+len(body))
	out = append(out, typ, byte(len(body)>>16), byte(len(body)>>8), byte(len(body)))
	return append(out, body...)
}

// writeHandshake records the message in the transcript before sending
// it: both sides must hash exactly what travelled, in order.
func (hs *handshakeState) writeHandshake(typ byte, body []byte) error {
	msg := handshakeMessage(typ, body)
	hs.transcript.Write(msg)
	return hs.c.writeRecord(recordHandshake, msg)
}

// nextHandshake returns one handshake message, reassembling across
// records: a peer is free to split a flight however it likes.
//
// ChangeCipherSpec is reported through its own return value rather than
// as a message type. The two numbering spaces overlap — record type 20
// is ChangeCipherSpec, handshake type 20 is Finished — and folding them
// into one byte makes the peer's Finished read as a second
// ChangeCipherSpec, which is a handshake that fails against every real
// server.
func (hs *handshakeState) nextHandshake(buf *[]byte) (isCCS bool, typ byte, body []byte, err error) {
	for {
		if len(*buf) >= 4 {
			length := int((*buf)[1])<<16 | int((*buf)[2])<<8 | int((*buf)[3])
			if length > maxPlaintext {
				return false, 0, nil, fmt.Errorf("psk: handshake message announces %d bytes", length)
			}
			if len(*buf) >= 4+length {
				whole := (*buf)[:4+length]
				*buf = (*buf)[4+length:]
				hs.transcript.Write(whole)
				return false, whole[0], whole[4:], nil
			}
		}
		recType, recBody, err := hs.c.readRecord()
		if err != nil {
			return false, 0, nil, err
		}
		if recType == recordChangeCipherSpec {
			return true, 0, recBody, nil
		}
		if recType != recordHandshake {
			return false, 0, nil, fmt.Errorf("psk: expected a handshake record, got type %d", recType)
		}
		*buf = append(*buf, recBody...)
	}
}

func (hs *handshakeState) activateWrite(key, iv []byte) error {
	aead, err := newAEAD(key)
	if err != nil {
		return err
	}
	if err := hs.c.writeRecord(recordChangeCipherSpec, []byte{1}); err != nil {
		return err
	}
	hs.c.writeAEAD, hs.c.writeIV, hs.c.writeSeq = aead, iv, 0
	return nil
}

func (hs *handshakeState) activateRead(key, iv []byte) error {
	aead, err := newAEAD(key)
	if err != nil {
		return err
	}
	hs.c.readAEAD, hs.c.readIV, hs.c.readSeq = aead, iv, 0
	return nil
}

func (hs *handshakeState) runClient() error {
	var err error
	if hs.clientRand, err = randomBytes(32); err != nil {
		return err
	}

	// The transcript hash is fixed by the suite, which the server has
	// not chosen yet. Both offered suites use a SHA-2 PRF but a
	// different one, so the transcript is buffered until the choice is
	// known rather than hashed with a guess.
	var raw []byte
	record := func(b []byte) { raw = append(raw, b...) }

	hello := make([]byte, 0, 64)
	hello = append(hello, 3, 3)
	hello = append(hello, hs.clientRand...)
	hello = append(hello, 0)
	hello = append(hello, byte(len(offered)*2>>8), byte(len(offered)*2))
	for _, s := range offered {
		hello = binary.BigEndian.AppendUint16(hello, s.id)
	}
	hello = append(hello, 1, 0)
	hello = withExtensions(hello, renegotiationInfo)

	msg := handshakeMessage(msgClientHello, hello)
	record(msg)
	if err := hs.c.writeRecord(recordHandshake, msg); err != nil {
		return fmt.Errorf("psk: sending client hello: %w", err)
	}

	var buf []byte
	var pending [][]byte
	readOne := func() (byte, []byte, error) {
		for {
			if len(buf) >= 4 {
				length := int(buf[1])<<16 | int(buf[2])<<8 | int(buf[3])
				if length > maxPlaintext {
					return 0, nil, fmt.Errorf("psk: handshake message announces %d bytes", length)
				}
				if len(buf) >= 4+length {
					whole := buf[:4+length]
					buf = buf[4+length:]
					pending = append(pending, whole)
					return whole[0], whole[4:], nil
				}
			}
			typ, body, err := hs.c.readRecord()
			if err != nil {
				return 0, nil, err
			}
			if typ != recordHandshake {
				return 0, nil, fmt.Errorf("psk: expected a handshake record, got type %d", typ)
			}
			buf = append(buf, body...)
		}
	}

	for done := false; !done; {
		typ, body, err := readOne()
		if err != nil {
			return fmt.Errorf("psk: reading the server flight: %w", err)
		}
		switch typ {
		case msgServerHello:
			if len(body) < 38 {
				return errors.New("psk: truncated server hello")
			}
			hs.serverRand = append([]byte{}, body[2:34]...)
			sessionLen := int(body[34])
			if len(body) < 35+sessionLen+2 {
				return errors.New("psk: truncated server hello")
			}
			id := binary.BigEndian.Uint16(body[35+sessionLen : 37+sessionLen])
			s, ok := suiteByID(id)
			if !ok {
				return fmt.Errorf("psk: server chose cipher suite 0x%04X, which was not offered", id)
			}
			hs.suite = s
		case msgServerKeyExchange:
			// Carries a psk_identity_hint. Ignored on purpose: the
			// identity comes from configuration, so a peer cannot steer
			// which key we prove possession of.
		case msgServerHelloDone:
			done = true
		default:
			return fmt.Errorf("psk: unexpected handshake message %d from the server", typ)
		}
	}
	if hs.suite.id == 0 {
		return errors.New("psk: the server never sent a server hello")
	}

	// The transcript is replayed in order now that the hash is known:
	// the client hello we sent, then every message the server sent.
	hs.transcript = hs.suite.newPRF()
	hs.transcript.Write(raw)
	for _, m := range pending {
		hs.transcript.Write(m)
	}

	identity := make([]byte, 0, 2+len(hs.cfg.Identity))
	identity = binary.BigEndian.AppendUint16(identity, uint16(len(hs.cfg.Identity)))
	identity = append(identity, hs.cfg.Identity...)
	if err := hs.writeHandshake(msgClientKeyExchange, identity); err != nil {
		return fmt.Errorf("psk: sending client key exchange: %w", err)
	}

	k := deriveKeys(hs.suite, hs.cfg.Key, hs.clientRand, hs.serverRand)
	if err := hs.activateWrite(k.clientKey, k.clientIV); err != nil {
		return err
	}
	verify := finishedVerify(hs.suite, k.master, labelClientFinished, hs.transcript.Sum(nil))
	if err := hs.writeHandshake(msgFinished, verify); err != nil {
		return fmt.Errorf("psk: sending finished: %w", err)
	}

	expected := finishedVerify(hs.suite, k.master, labelServerFinished, hs.transcript.Sum(nil))
	return hs.readPeerFinished(k.serverKey, k.serverIV, expected)
}

// readPeerFinished waits for the peer's ChangeCipherSpec then its
// Finished, and checks the verify data in constant time.
func (hs *handshakeState) readPeerFinished(key, iv, expected []byte) error {
	var buf []byte
	activated := false
	for {
		isCCS, typ, body, err := hs.nextHandshake(&buf)
		if err != nil {
			return fmt.Errorf("psk: reading the peer's finished: %w", err)
		}
		if isCCS {
			if activated {
				return errors.New("psk: peer sent change cipher spec twice")
			}
			if err := hs.activateRead(key, iv); err != nil {
				return err
			}
			activated = true
			continue
		}
		if typ != msgFinished {
			return fmt.Errorf("psk: expected finished, got handshake message %d", typ)
		}
		if !activated {
			return errors.New("psk: peer sent finished before enabling encryption")
		}
		if !hmac.Equal(body, expected) {
			return errors.New("psk: the peer's finished does not verify; the pre-shared keys differ")
		}
		return nil
	}
}

func (hs *handshakeState) runServer() error {
	var buf []byte
	// Same reason as the client: the transcript hash depends on the
	// suite, which is not known until the hello has been read.
	typ, body, err := hs.readFirstClientHello(&buf)
	if err != nil {
		return err
	}
	if typ != msgClientHello {
		return fmt.Errorf("psk: expected a client hello, got handshake message %d", typ)
	}
	helloRaw := handshakeMessage(msgClientHello, body)

	if len(body) < 35 {
		return errors.New("psk: truncated client hello")
	}
	hs.clientRand = append([]byte{}, body[2:34]...)
	sessionLen := int(body[34])
	rest := body[35+sessionLen:]
	if len(rest) < 2 {
		return errors.New("psk: truncated client hello")
	}
	suitesLen := int(rest[0])<<8 | int(rest[1])
	if len(rest) < 2+suitesLen {
		return errors.New("psk: truncated cipher suite list")
	}
	var chosen suite
	for i := 0; i+1 < suitesLen; i += 2 {
		if s, ok := suiteByID(binary.BigEndian.Uint16(rest[2+i : 4+i])); ok {
			chosen = s
			break
		}
	}
	if chosen.id == 0 {
		return errors.New("psk: the client offered no pre-shared-key cipher suite this profile speaks")
	}
	hs.suite = chosen
	hs.transcript = chosen.newPRF()
	hs.transcript.Write(helloRaw)

	if hs.serverRand, err = randomBytes(32); err != nil {
		return err
	}
	hello := make([]byte, 0, 64)
	hello = append(hello, 3, 3)
	hello = append(hello, hs.serverRand...)
	hello = append(hello, 0)
	hello = binary.BigEndian.AppendUint16(hello, chosen.id)
	hello = append(hello, 0)
	hello = withExtensions(hello, renegotiationInfo)
	if err := hs.writeHandshake(msgServerHello, hello); err != nil {
		return fmt.Errorf("psk: sending server hello: %w", err)
	}
	if err := hs.writeHandshake(msgServerHelloDone, nil); err != nil {
		return fmt.Errorf("psk: sending server hello done: %w", err)
	}

	var isCCS bool
	isCCS, typ, body, err = hs.nextHandshake(&buf)
	if err != nil {
		return fmt.Errorf("psk: reading client key exchange: %w", err)
	}
	if isCCS {
		return errors.New("psk: client enabled encryption before sending its key exchange")
	}
	if typ != msgClientKeyExchange {
		return fmt.Errorf("psk: expected client key exchange, got handshake message %d", typ)
	}
	if len(body) < 2 {
		return errors.New("psk: truncated client key exchange")
	}
	identityLen := int(binary.BigEndian.Uint16(body[:2]))
	if len(body) < 2+identityLen {
		return errors.New("psk: client key exchange shorter than the identity it announces")
	}
	// The identity is compared in constant time even though it is not a
	// secret: the reply timing should not distinguish a near-miss.
	if !hmac.Equal(body[2:2+identityLen], []byte(hs.cfg.Identity)) {
		return errors.New("psk: the client presented an identity this listener has no key for")
	}

	k := deriveKeys(hs.suite, hs.cfg.Key, hs.clientRand, hs.serverRand)
	expected := finishedVerify(hs.suite, k.master, labelClientFinished, hs.transcript.Sum(nil))
	if err := hs.readPeerFinished(k.clientKey, k.clientIV, expected); err != nil {
		return err
	}
	if err := hs.activateWrite(k.serverKey, k.serverIV); err != nil {
		return err
	}
	verify := finishedVerify(hs.suite, k.master, labelServerFinished, hs.transcript.Sum(nil))
	if err := hs.writeHandshake(msgFinished, verify); err != nil {
		return fmt.Errorf("psk: sending finished: %w", err)
	}
	return nil
}

// readFirstClientHello reads before any transcript exists, so it cannot
// go through nextHandshake.
func (hs *handshakeState) readFirstClientHello(buf *[]byte) (byte, []byte, error) {
	for {
		if len(*buf) >= 4 {
			length := int((*buf)[1])<<16 | int((*buf)[2])<<8 | int((*buf)[3])
			if length > maxPlaintext {
				return 0, nil, fmt.Errorf("psk: client hello announces %d bytes", length)
			}
			if len(*buf) >= 4+length {
				whole := (*buf)[:4+length]
				*buf = (*buf)[4+length:]
				return whole[0], whole[4:], nil
			}
		}
		typ, body, err := hs.c.readRecord()
		if err != nil {
			return 0, nil, err
		}
		if typ != recordHandshake {
			return 0, nil, fmt.Errorf("psk: expected a handshake record, got type %d", typ)
		}
		*buf = append(*buf, body...)
	}
}
