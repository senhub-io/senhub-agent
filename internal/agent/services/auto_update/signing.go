package auto_update

import (
	"fmt"
	"os"

	"aead.dev/minisign"
)

// signingPublicKey is the minisign public key release artifacts must be
// signed with, injected at build time by the release pipeline:
//
//	-ldflags "-X senhub-agent.go/internal/agent/services/auto_update.signingPublicKey=RWQ..."
//
// Fail-closed (#266): a build that embeds no key refuses to self-update
// — integrity of a root-fleet updater cannot rest on TLS to a
// config-settable registry URL alone, so the key is deliberately NOT
// configurable at runtime.
var signingPublicKey string

// verifyArchiveSignature checks the detached minisign signature of a
// downloaded release archive against the embedded public key. The
// signature covers the archive bytes exactly as served, so verification
// happens before the archive is even parsed as a ZIP.
func verifyArchiveSignature(archive, signature []byte) error {
	pub, err := embeddedSigningKey()
	if err != nil {
		return err
	}
	if !minisign.Verify(pub, archive, signature) {
		return fmt.Errorf("minisign signature verification failed")
	}
	return nil
}

// verifyArchiveFile is verifyArchiveSignature for an archive that was
// streamed to path through digest instead of held in memory. A
// prehashed signature (HashEdDSA, what the release tool produces from
// 0.6.0 on) is checked against the digest the reader accumulated. A
// legacy one (EdDSA) signs the bytes themselves, so the archive is read
// back for it: that costs the archive's size in memory, never the
// binary's (#891).
func verifyArchiveFile(digest *minisign.Reader, path string, signature []byte) error {
	pub, err := embeddedSigningKey()
	if err != nil {
		return err
	}
	var sig minisign.Signature
	if err := sig.UnmarshalText(signature); err != nil {
		return fmt.Errorf("minisign signature is unreadable: %w", err)
	}
	if sig.Algorithm == minisign.HashEdDSA {
		if !digest.Verify(pub, signature) {
			return fmt.Errorf("minisign signature verification failed")
		}
		return nil
	}
	archive, err := os.ReadFile(path) // #nosec G304 - the archive this process just wrote
	if err != nil {
		return fmt.Errorf("reading the archive back for a legacy signature: %w", err)
	}
	if !minisign.Verify(pub, archive, signature) {
		return fmt.Errorf("minisign signature verification failed")
	}
	return nil
}

func embeddedSigningKey() (minisign.PublicKey, error) {
	var pub minisign.PublicKey
	if signingPublicKey == "" {
		return pub, fmt.Errorf("this build embeds no update signing key — refusing to self-update")
	}
	if err := pub.UnmarshalText([]byte(signingPublicKey)); err != nil {
		return pub, fmt.Errorf("embedded signing public key is invalid: %w", err)
	}
	return pub, nil
}
