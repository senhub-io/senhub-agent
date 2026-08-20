package otlp

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"senhub-agent.go/internal/agent/services/exporterrors"
)

func TestClassifyExportError_GRPC(t *testing.T) {
	cases := []struct {
		code      codes.Code
		retryable bool
		reason    string
	}{
		{codes.Unavailable, true, "transport"},
		{codes.DeadlineExceeded, true, "transport"},
		{codes.ResourceExhausted, true, "transport"},
		{codes.Aborted, true, "transport"},
		{codes.Internal, true, "transport"},
		// Auth is deliberately retryable, matching the cloud sink's 401/403
		// decision: a blip or a slow key rotation clears on its own.
		{codes.Unauthenticated, true, "transport"},
		{codes.PermissionDenied, true, "transport"},
		// The server looked at the payload and refused it.
		{codes.InvalidArgument, false, "validation"},
		{codes.FailedPrecondition, false, "validation"},
		// The endpoint does not speak this signal; only an operator fixes that.
		{codes.Unimplemented, false, "configuration"},
		{codes.NotFound, false, "configuration"},
	}

	for _, c := range cases {
		t.Run(c.code.String(), func(t *testing.T) {
			got := classifyExportError(status.Error(c.code, "boom"))
			if exporterrors.IsRetryable(got) != c.retryable {
				t.Errorf("IsRetryable = %v, want %v (err: %v)", !c.retryable, c.retryable, got)
			}
			if r := exporterrors.Reason(got); r != c.reason {
				t.Errorf("Reason = %q, want %q", r, c.reason)
			}
		})
	}
}

// TestClassifyExportError_HTTP covers the OTLP/HTTP path, where the SDK
// reports a non-retryable response as a formatted string. The exact
// wording is upstream's, reproduced here from otlploghttp's client.
func TestClassifyExportError_HTTP(t *testing.T) {
	sdkError := func(status string) error {
		return fmt.Errorf("failed to send logs to https://intake.example/v1/logs: %s (body: %s)",
			status, "unsupported attribute type")
	}

	cases := []struct {
		status    string
		retryable bool
	}{
		{"400 Bad Request", false},
		{"404 Not Found", false},
		{"422 Unprocessable Entity", false},
		// The SDK retries these itself; if one still surfaces, it stays
		// on the retry path rather than being discarded.
		{"429 Too Many Requests", true},
		{"408 Request Timeout", true},
		{"503 Service Unavailable", true},
		{"401 Unauthorized", true},
		{"403 Forbidden", true},
	}

	for _, c := range cases {
		t.Run(c.status, func(t *testing.T) {
			got := classifyExportError(sdkError(c.status))
			if exporterrors.IsRetryable(got) != c.retryable {
				t.Errorf("%q: IsRetryable = %v, want %v", c.status, !c.retryable, c.retryable)
			}
		})
	}
}

// TestClassifyExportError_FailsSafe is the property that makes the
// string parse acceptable: anything the classifier cannot read stays
// retryable, so an upstream wording change degrades to a batch kept
// that could have been dropped — never to a batch discarded that could
// have been delivered.
func TestClassifyExportError_FailsSafe(t *testing.T) {
	cases := []error{
		errors.New("dial tcp 10.0.0.1:4318: connect: connection refused"),
		errors.New("context deadline exceeded"),
		errors.New("failed to send logs: EOF"),
		// A port and a byte count must not be mistaken for a status.
		errors.New("dial tcp intake.example:4318: i/o timeout"),
		errors.New("response body too large: exceeded 400 bytes"),
	}

	for _, err := range cases {
		t.Run(err.Error(), func(t *testing.T) {
			if got := classifyExportError(err); !exporterrors.IsRetryable(got) {
				t.Errorf("an unrecognisable error was classified as permanent: %v", got)
			}
		})
	}

	if got := classifyExportError(nil); got != nil {
		t.Errorf("classifyExportError(nil) = %v, want nil", got)
	}
}
