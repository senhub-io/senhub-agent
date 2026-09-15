package exporterrors

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// Both the class and the underlying cause have to stay reachable: the
// class drives the retry decision, the cause is what the operator reads
// in the log line.
func TestClassifyKeepsBothClassAndCause(t *testing.T) {
	cause := errors.New("connection refused")

	cases := []struct {
		name  string
		err   error
		class error
	}{
		{"transport", Transport("POST request failed", cause), ErrTransport},
		{"configuration", Configuration("failed to join URL path", cause), ErrConfiguration},
		{"validation", Validation("failed to marshal JSON", cause), ErrValidation},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !errors.Is(c.err, c.class) {
				t.Errorf("errors.Is(%v, %v) = false, want true", c.err, c.class)
			}
			if !errors.Is(c.err, cause) {
				t.Errorf("cause is no longer reachable through %v", c.err)
			}
			if !strings.Contains(c.err.Error(), "connection refused") {
				t.Errorf("message drops the cause: %q", c.err)
			}
		})
	}
}

// The three classes are distinct: classifying as one must not make the
// error read as another.
func TestClassesDoNotOverlap(t *testing.T) {
	err := Configuration("bad endpoint", errors.New("parse error"))
	if errors.Is(err, ErrTransport) || errors.Is(err, ErrValidation) {
		t.Errorf("configuration failure leaked into another class: %v", err)
	}
}

func TestClassifyWithoutCause(t *testing.T) {
	err := Transport("no endpoint answered", nil)
	if !errors.Is(err, ErrTransport) {
		t.Errorf("errors.Is(%v, ErrTransport) = false, want true", err)
	}
}

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"transport retries", Transport("timeout", errors.New("i/o timeout")), true},
		{"configuration drops", Configuration("bad url", errors.New("parse")), false},
		{"validation drops", Validation("bad payload", errors.New("marshal")), false},
		// Everything the export paths raised before this taxonomy
		// existed is unclassified and must keep its batch — an unknown
		// failure is not a licence to discard data.
		{"unclassified retries", errors.New("something else"), true},
		{"nil retries", nil, true},
	}
	for _, c := range cases {
		if got := IsRetryable(c.err); got != c.want {
			t.Errorf("%s: IsRetryable(%v) = %v, want %v", c.name, c.err, got, c.want)
		}
	}
}

// TestIsPermanentHTTPStatus pins the retry-vs-drop split for HTTP push
// sinks. It moved here from the cloud strategy when the event sink
// needed the same classification and would otherwise have forked it
// (#287) — the table is the one the cloud sink shipped.
func TestIsPermanentHTTPStatus(t *testing.T) {
	cases := []struct {
		status    int
		permanent bool
	}{
		{http.StatusBadRequest, true},           // 400
		{http.StatusUnauthorized, false},        // 401 retryable (auth blip / key rotation)
		{http.StatusForbidden, false},           // 403 retryable (auth blip / key rotation)
		{http.StatusNotFound, true},             // 404
		{http.StatusUnprocessableEntity, true},  // 422
		{http.StatusRequestTimeout, false},      // 408 retryable
		{http.StatusTooManyRequests, false},     // 429 retryable
		{http.StatusInternalServerError, false}, // 500 retryable
		{http.StatusServiceUnavailable, false},  // 503 retryable
		{http.StatusOK, false},                  // 200 not an error
	}
	for _, c := range cases {
		if got := IsPermanentHTTPStatus(c.status); got != c.permanent {
			t.Errorf("IsPermanentHTTPStatus(%d) = %v, want %v", c.status, got, c.permanent)
		}
	}
}

// TestReason pins the bounded label set the send-failure counter uses.
// An unclassified error must report "transport": IsRetryable keeps its
// batch, so a counter saying otherwise would tell an operator data was
// dropped when it was not.
func TestReason(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"transport", Transport("posting", errors.New("connection refused")), "transport"},
		{"validation", Validation("rejected", errors.New("400")), "validation"},
		{"configuration", Configuration("bad url", errors.New("parse")), "configuration"},
		{"unclassified", errors.New("something else"), "transport"},
		{"nil", nil, "transport"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Reason(c.err); got != c.want {
				t.Errorf("Reason(%v) = %q, want %q", c.err, got, c.want)
			}
		})
	}
}
