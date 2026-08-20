package exporterrors

import (
	"errors"
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
