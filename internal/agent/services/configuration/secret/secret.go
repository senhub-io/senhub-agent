// Package secret provides the runtime resolution behind the ${secret:<name>}
// configuration scheme and the non-printing Secret type that keeps a resolved
// value out of logs, config dumps and error strings.
//
// Design invariants (security-critical — see docs/audit/SECRETS-*.md):
//   - A secret value is never rendered by any stringification path: fmt verbs,
//     JSON, YAML and zerolog all emit a fixed redaction marker.
//   - The plaintext is reachable only through Secret.Expose(), whose name marks
//     every call site as a deliberate, audited reveal boundary.
//   - Errors carry only the secret NAME, never the value.
package secret

import (
	"fmt"

	"github.com/rs/zerolog"
)

// redacted is the single marker emitted on every render path.
const redacted = "****"

// Secret holds a sensitive value in memory while preventing it from leaking
// through the usual stringification paths.
//
// fmt.Formatter covers every fmt verb (%v, %s, %q, %+v, %#v); the Marshal*
// methods cover the JSON, YAML and zerolog encoders. The value is stored in an
// unexported field and is reachable only through Expose().
//
// Caveat: a reflection-based dumper that reads unexported fields directly
// (encoding/gob, davecgh/spew) can still observe the value. No secret-bearing
// code path uses such a dumper; the config and log paths all go through the
// methods below.
type Secret struct {
	v string
}

// New wraps a plaintext value.
func New(plaintext string) Secret { return Secret{v: plaintext} }

// Expose returns the plaintext. Every call site is an audited boundary where a
// secret is deliberately handed to code that must see it — a probe client, the
// `agent secret get` reveal command, or a backend write. Grep for `.Expose(` to
// review every place a secret leaves the type.
func (s Secret) Expose() string { return s.v }

// IsZero reports whether the wrapped value is empty.
func (s Secret) IsZero() bool { return s.v == "" }

// Format implements fmt.Formatter so EVERY fmt verb renders the marker — this is
// the catch-all that also neutralises %#v and %+v (which would otherwise expose
// the unexported field through the default struct formatter).
func (s Secret) Format(f fmt.State, verb rune) {
	if verb == 'q' {
		_, _ = fmt.Fprintf(f, "%q", redacted)
		return
	}
	_, _ = f.Write([]byte(redacted))
}

// String renders the marker for callers that bypass fmt (explicit .String()).
func (s Secret) String() string { return redacted }

// MarshalText covers encoders that prefer encoding.TextMarshaler.
func (s Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// MarshalJSON keeps the value out of JSON output.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// MarshalYAML keeps the value out of YAML output (e.g. `config show`).
func (s Secret) MarshalYAML() (interface{}, error) { return redacted, nil }

// MarshalZerologObject keeps the value out of structured logs.
func (s Secret) MarshalZerologObject(e *zerolog.Event) { e.Str("value", redacted) }
