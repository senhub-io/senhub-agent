package psk

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"hash"
)

// suite is one of the two AEAD cipher suites this profile carries. They
// differ in three linked ways — the PRF hash, the key size and the hash
// the transcript is taken with — so they travel together rather than as
// separate constants that could drift apart.
type suite struct {
	id     uint16
	keyLen int
	newPRF func() hash.Hash
}

var (
	suiteAES128 = suite{id: 0x00A8, keyLen: 16, newPRF: sha256.New}
	suiteAES256 = suite{id: 0x00A9, keyLen: 32, newPRF: sha512.New384}

	// Offered in this order: the server picks, and AES-128-GCM is what
	// both Zabbix lines choose from their default list.
	offered = []suite{suiteAES128, suiteAES256}
)

func suiteByID(id uint16) (suite, bool) {
	for _, s := range offered {
		if s.id == id {
			return s, true
		}
	}
	return suite{}, false
}

// prf is the TLS 1.2 pseudo-random function (RFC 5246 section 5), whose
// hash is the one the cipher suite names.
func prf(newHash func() hash.Hash, secret []byte, label string, seed []byte, n int) []byte {
	s := make([]byte, 0, len(label)+len(seed))
	s = append(s, label...)
	s = append(s, seed...)

	out := make([]byte, 0, n+newHash().Size())
	a := s
	for len(out) < n {
		h := hmac.New(newHash, secret)
		h.Write(a)
		a = h.Sum(nil)

		h = hmac.New(newHash, secret)
		h.Write(a)
		h.Write(s)
		out = h.Sum(out)
	}
	return out[:n]
}

// preMasterSecret is RFC 4279 section 2: for a pure PSK suite the
// "other secret" is a run of zeroes as long as the key, and each half
// carries its own 16-bit length.
//
// The length is checked here rather than trusted from Config.validate.
// A key past 16 bits would wrap silently into a length that describes
// something else, and the session keys would then be derived from a
// structure neither side agrees on — a failure that looks like a
// mismatched key rather than a bug.
func preMasterSecret(key []byte) ([]byte, error) {
	if len(key) > maxKeyLen {
		return nil, fmt.Errorf("psk: key is %d bytes, more than the %d this profile carries", len(key), maxKeyLen)
	}
	n := uint16(len(key)) // #nosec G115 - bounded by maxKeyLen just above
	out := make([]byte, 0, 4+2*len(key))
	out = binary.BigEndian.AppendUint16(out, n)
	out = append(out, make([]byte, len(key))...)
	out = binary.BigEndian.AppendUint16(out, n)
	out = append(out, key...)
	return out, nil
}

// keys holds what the record layer needs, per direction.
type keys struct {
	clientKey, serverKey []byte
	clientIV, serverIV   []byte
	master               []byte
}

// deriveKeys runs the master secret and the key block. AEAD suites carry
// no MAC key, so the block is two keys and two 4-byte implicit nonces.
func deriveKeys(s suite, key, clientRandom, serverRandom []byte) (keys, error) {
	pms, err := preMasterSecret(key)
	if err != nil {
		return keys{}, err
	}
	seed := make([]byte, 0, 64)
	seed = append(seed, clientRandom...)
	seed = append(seed, serverRandom...)
	master := prf(s.newPRF, pms, "master secret", seed, 48)

	seed = seed[:0]
	seed = append(seed, serverRandom...)
	seed = append(seed, clientRandom...)
	block := prf(s.newPRF, master, "key expansion", seed, 2*s.keyLen+2*fixedIVLen)

	k := s.keyLen
	return keys{
		clientKey: block[0:k],
		serverKey: block[k : 2*k],
		clientIV:  block[2*k : 2*k+fixedIVLen],
		serverIV:  block[2*k+fixedIVLen : 2*k+2*fixedIVLen],
		master:    master,
	}, nil
}

// finishedVerify is the 12 bytes each side proves the transcript with.
func finishedVerify(s suite, master []byte, label string, transcript []byte) []byte {
	return prf(s.newPRF, master, label, transcript, 12)
}

const (
	labelClientFinished = "client finished"
	labelServerFinished = "server finished"
)
