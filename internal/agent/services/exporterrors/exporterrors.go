// Package exporterrors carries the error classes an export path has to
// tell apart to decide what to do with a batch it could not deliver.
//
// There are exactly three, because there are exactly three answers:
//
//   - ErrTransport — the far end could not be reached, or answered in a
//     way a later attempt may not repeat. Keep the batch, retry.
//   - ErrConfiguration — the request could not even be built from the
//     configured endpoint. No number of retries produces a different
//     outcome until an operator edits the config. Drop the batch.
//   - ErrValidation — the request reached the far end and was rejected
//     for what it contained. Resending the same bytes gets the same
//     rejection. Drop the batch.
//
// The distinction that matters operationally is retry vs drop: a batch
// re-queued for an error that will never clear pins the head of a
// bounded push buffer forever, so the buffer stops draining and every
// scheduler tick burns a round-trip. Splitting drop into configuration
// and validation costs nothing and keeps the operator-facing log honest
// about whose fault it is.
//
// Nothing else belongs here. A class earns its place when an export
// path branches on it, not when it sounds like a category.
package exporterrors

import (
	"errors"
	"fmt"
)

var (
	// ErrTransport marks a delivery failure: connection refused,
	// timeout, TLS handshake, a 5xx, a retryable 4xx.
	ErrTransport = errors.New("transport failure")

	// ErrConfiguration marks a request that could not be constructed
	// from the configured parameters — a malformed endpoint URL, an
	// unusable method. Operator input is required to clear it.
	ErrConfiguration = errors.New("configuration failure")

	// ErrValidation marks a payload the far end refuses on its merits,
	// or one this agent cannot even serialise. Resending is pointless.
	ErrValidation = errors.New("payload rejected")
)

// Transport, Configuration and Validation wrap cause in the matching
// class. Both the class and the cause stay reachable through errors.Is
// and errors.As, so a caller can branch on the class while the log line
// still names the underlying failure.
func Transport(msg string, cause error) error {
	return classify(ErrTransport, msg, cause)
}

func Configuration(msg string, cause error) error {
	return classify(ErrConfiguration, msg, cause)
}

func Validation(msg string, cause error) error {
	return classify(ErrValidation, msg, cause)
}

func classify(class error, msg string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%s: %w", msg, class)
	}
	return fmt.Errorf("%s: %w (%w)", msg, cause, class)
}

// IsRetryable reports whether a later attempt with the same payload
// could plausibly succeed. Anything unclassified is retryable: the
// export paths predate this taxonomy and an unknown failure must keep
// the batch rather than silently discard it.
func IsRetryable(err error) bool {
	return !errors.Is(err, ErrConfiguration) && !errors.Is(err, ErrValidation)
}
