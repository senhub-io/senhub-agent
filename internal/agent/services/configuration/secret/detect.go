package secret

import "regexp"

// sensitiveKeyPattern matches config field NAMES that carry a secret value and
// therefore must be sealed. It is deliberately narrower than the log-redaction
// pattern: identifier-style fields (`user`, `login`, `email`) are NOT secrets
// and must not be moved into the store. Connection strings (`dsn`, `uri`) are
// included because they embed a password.
var sensitiveKeyPattern = regexp.MustCompile(`(?i)(password|passphrase|secret|token|api[_-]?key|community|credential|dsn|uri|private[_-]?key)`)

// IsSensitiveKey reports whether a field name denotes a secret to seal. It is
// the single definition of "what field name is a secret"; the seal walker in
// config_seal.go consumes it directly so the two never drift.
func IsSensitiveKey(name string) bool { return sensitiveKeyPattern.MatchString(name) }
