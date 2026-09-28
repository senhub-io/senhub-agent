package psk

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

const (
	recordChangeCipherSpec = 20
	recordAlert            = 21
	recordHandshake        = 22
	recordApplicationData  = 23

	fixedIVLen    = 4
	explicitIVLen = 8
	gcmTagLen     = 16

	// maxPlaintext is the TLS record limit. The ceiling on what is read
	// is stated rather than trusted from the header: a length field is
	// attacker-controlled, and allocating from it is how a peer turns a
	// 5-byte header into a memory exhaustion.
	maxPlaintext  = 1 << 14
	maxCiphertext = maxPlaintext + explicitIVLen + gcmTagLen + 256
)

type alertError struct {
	level, description uint8
}

func (a alertError) Error() string {
	if a.description == 0 {
		return "psk: peer closed the connection"
	}
	return fmt.Sprintf("psk: peer sent alert level %d description %d (%s)", a.level, a.description, alertName(a.description))
}

func alertName(d uint8) string {
	switch d {
	case 40:
		return "handshake failure: the server has no key for this identity, or refuses the suite"
	case 47:
		return "illegal parameter"
	case 20:
		return "bad record MAC: the key does not match"
	case 51:
		return "decrypt error"
	case 71:
		return "insufficient security"
	case 112:
		return "unrecognized name"
	}
	return "see RFC 5246 appendix A.3"
}

// conn is the record layer. Everything before the ChangeCipherSpec of a
// direction travels in clear; everything after is sealed.
type conn struct {
	net.Conn

	writeMu  sync.Mutex
	readMu   sync.Mutex
	writeSeq uint64
	readSeq  uint64

	writeAEAD cipher.AEAD
	readAEAD  cipher.AEAD
	writeIV   []byte
	readIV    []byte

	pending []byte
	readErr error
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("psk: aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("psk: gcm: %w", err)
	}
	return aead, nil
}

func additionalData(seq uint64, typ byte, length int) []byte {
	aad := make([]byte, 0, 13)
	aad = binary.BigEndian.AppendUint64(aad, seq)
	aad = append(aad, typ, 3, 3, byte(length>>8), byte(length))
	return aad
}

func (c *conn) writeRecord(typ byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeRecordLocked(typ, payload)
}

func (c *conn) writeRecordLocked(typ byte, payload []byte) error {
	for {
		chunk := payload
		if len(chunk) > maxPlaintext {
			chunk = chunk[:maxPlaintext]
		}
		body := chunk
		if c.writeAEAD != nil {
			var err error
			if body, err = c.seal(typ, chunk); err != nil {
				return err
			}
		}
		header := []byte{typ, 3, 3, byte(len(body) >> 8), byte(len(body))}
		if _, err := c.Conn.Write(append(header, body...)); err != nil {
			return fmt.Errorf("psk: writing record: %w", err)
		}
		payload = payload[len(chunk):]
		if len(payload) == 0 {
			return nil
		}
	}
}

func (c *conn) seal(typ byte, plain []byte) ([]byte, error) {
	// The sequence number is also the explicit nonce, which is what
	// RFC 5288 allows and what every TLS 1.2 GCM implementation does. It
	// must never repeat under one key; refusing to wrap is the only
	// honest answer, and no Zabbix session comes near 2^64 records.
	if c.writeSeq == ^uint64(0) {
		return nil, errors.New("psk: record sequence number exhausted")
	}
	explicit := make([]byte, explicitIVLen)
	binary.BigEndian.PutUint64(explicit, c.writeSeq)

	nonce := make([]byte, 0, fixedIVLen+explicitIVLen)
	nonce = append(nonce, c.writeIV...)
	nonce = append(nonce, explicit...)

	sealed := c.writeAEAD.Seal(nil, nonce, plain, additionalData(c.writeSeq, typ, len(plain)))
	c.writeSeq++
	return append(explicit, sealed...), nil
}

// readRecord returns one record's payload, decrypted when the read side
// is protected.
func (c *conn) readRecord() (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(c.Conn, header); err != nil {
		return 0, nil, err
	}
	typ := header[0]
	length := int(header[3])<<8 | int(header[4])
	if length == 0 || length > maxCiphertext {
		return 0, nil, fmt.Errorf("psk: record announces %d bytes, outside what this profile carries", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(c.Conn, body); err != nil {
		return 0, nil, fmt.Errorf("psk: short record: %w", err)
	}

	// ChangeCipherSpec is the message that turns protection on, so it is
	// never itself protected.
	if c.readAEAD != nil && typ != recordChangeCipherSpec {
		plain, err := c.open(typ, body)
		if err != nil {
			return typ, nil, err
		}
		body = plain
	}
	if typ == recordAlert {
		if len(body) < 2 {
			return typ, nil, errors.New("psk: truncated alert")
		}
		return typ, body, alertError{level: body[0], description: body[1]}
	}
	return typ, body, nil
}

func (c *conn) open(typ byte, body []byte) ([]byte, error) {
	if len(body) < explicitIVLen+gcmTagLen {
		return nil, errors.New("psk: sealed record too short to hold a nonce and a tag")
	}
	explicit, sealed := body[:explicitIVLen], body[explicitIVLen:]

	nonce := make([]byte, 0, fixedIVLen+explicitIVLen)
	nonce = append(nonce, c.readIV...)
	nonce = append(nonce, explicit...)

	plainLen := len(sealed) - gcmTagLen
	if plainLen > maxPlaintext {
		return nil, fmt.Errorf("psk: record would open to %d bytes, over the %d limit", plainLen, maxPlaintext)
	}
	plain, err := c.readAEAD.Open(nil, nonce, sealed, additionalData(c.readSeq, typ, plainLen))
	if err != nil {
		return nil, errors.New("psk: record failed to authenticate; the pre-shared keys differ or the stream was tampered with")
	}
	c.readSeq++
	return plain, nil
}

// Read serves application data, hiding the record framing.
func (c *conn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for len(c.pending) == 0 {
		if c.readErr != nil {
			return 0, c.readErr
		}
		typ, body, err := c.readRecord()
		if err != nil {
			var alert alertError
			if errors.As(err, &alert) && alert.description == 0 {
				c.readErr = io.EOF
				continue
			}
			c.readErr = err
			return 0, err
		}
		switch typ {
		case recordApplicationData:
			c.pending = body
		case recordHandshake:
			// A peer asking to renegotiate mid-stream is refused rather
			// than served: renegotiation is not in this profile, and
			// silently ignoring it would leave the peer waiting.
			c.readErr = errors.New("psk: peer tried to renegotiate, which this profile does not support")
			return 0, c.readErr
		default:
			// ChangeCipherSpec after the handshake is noise; skip it.
		}
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

func (c *conn) Write(p []byte) (int, error) {
	if err := c.writeRecord(recordApplicationData, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close sends a close_notify so the peer sees an orderly end rather than
// a truncated stream, then closes the socket whatever that produced.
func (c *conn) Close() error {
	if c.writeAEAD != nil {
		_ = c.writeRecord(recordAlert, []byte{1, 0})
	}
	return c.Conn.Close()
}
