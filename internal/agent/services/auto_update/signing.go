package auto_update

import (
	"fmt"

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
	if signingPublicKey == "" {
		return fmt.Errorf("this build embeds no update signing key — refusing to self-update")
	}
	var pub minisign.PublicKey
	if err := pub.UnmarshalText([]byte(signingPublicKey)); err != nil {
		return fmt.Errorf("embedded signing public key is invalid: %w", err)
	}
	if !minisign.Verify(pub, archive, signature) {
		return fmt.Errorf("minisign signature verification failed")
	}
	return nil
}
