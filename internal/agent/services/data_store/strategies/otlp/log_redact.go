package otlp

import "regexp"

// bearerTokenPattern catches "Bearer <token>" anywhere in a string —
// the OTel collector's bearertokenauth extension echoes the rejected
// token in its gRPC error response, and that string ends up in our
// "OTLP metrics export failed" log lines if we pass err.Error()
// through unmodified.
//
// The regex is intentionally permissive on the token shape (anything
// non-whitespace) because the collector format varies: hex64,
// UUID-with-suffix, plain opaque strings. The capture is the literal
// "Bearer" word + whitespace + one non-whitespace token.
var bearerTokenPattern = regexp.MustCompile(`(?i)Bearer\s+\S+`)

// redactSensitive returns s with any "Bearer <token>" occurrence
// replaced by "Bearer ***". Use it on error messages just before they
// land in a log entry; do not use on the structured `err` field
// (zerolog's Err() method calls err.Error() internally — by then
// the bytes are already buffered).
//
// Called only on the cold path (export failure). The regex cost is
// negligible compared to the gRPC roundtrip that just failed.
func redactSensitive(s string) string {
	return bearerTokenPattern.ReplaceAllString(s, "Bearer ***")
}
