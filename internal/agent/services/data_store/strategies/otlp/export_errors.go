package otlp

import (
	"regexp"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"senhub-agent.go/internal/agent/services/exporterrors"
)

// What a failure class means for a signal that has a disk queue behind
// it (#833).
//
// The other push sinks answer "retry or drop" with a memory backlog:
// dropping a batch the far end will never accept costs one batch. The
// logs signal answers it with a 128 MiB on-disk dead-letter queue, and
// that changes the stakes in both directions.
//
// Persisting an unclearable rejection is worse here than anywhere else.
// The batch is written to disk, replayed at boot and on every recovery,
// rejected again, and written back — forever. It occupies space that
// the drop-oldest eviction then takes from batches that COULD still be
// delivered. An outage's worth of real event logs is evicted to keep
// re-sending a payload the receiver has refused every time it saw it.
//
// So: the queue is for outages. A payload the far end rejects on its
// merits does not belong in it, and is dropped once, counted, and
// logged. Everything else is persisted, which is what the queue is for.
//
// The metrics signal needs no equivalent branch: its durability is the
// LWW checkpoint, a last-value-wins STATE store rather than a backlog of
// batches. A rejected push is superseded by the next cycle's values on
// its own, so "drop" is already what happens and adding a class check
// there would change nothing.

// classifyExportError maps an OTel SDK export error onto the shared
// taxonomy.
//
// gRPC is exact: the SDK returns a status error, and the code says
// whether the server refused the payload or could not be reached.
//
// OTLP/HTTP is not, because the SDK reports a non-retryable response as
// a formatted string with no structured status. The code is parsed out
// of it, and the parse FAILS SAFE: anything that does not yield a
// recognisable status stays transport, i.e. retryable and persisted.
// An upstream wording change therefore degrades to today's behaviour —
// a batch kept that could have been dropped — never to data loss.
func classifyExportError(err error) error {
	if err == nil {
		return nil
	}

	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return classifyGRPCCode(st.Code(), err)
	}

	if code, ok := httpStatusFromError(err); ok && exporterrors.IsPermanentHTTPStatus(code) {
		return exporterrors.Validation("receiver rejected the batch", err)
	}

	return exporterrors.Transport("export failed", err)
}

// classifyGRPCCode splits the gRPC status codes the OTLP spec calls
// retryable from the ones that describe a request the server will refuse
// again.
//
// The retryable set is the one the OTLP specification names:
// CANCELLED, DEADLINE_EXCEEDED, ABORTED, OUT_OF_RANGE, UNAVAILABLE,
// DATA_LOSS, RESOURCE_EXHAUSTED (the last only with a throttle hint,
// which we cannot see here — treated as retryable, the safe side).
func classifyGRPCCode(code codes.Code, cause error) error {
	switch code {
	case codes.Canceled,
		codes.DeadlineExceeded,
		codes.Aborted,
		codes.OutOfRange,
		codes.Unavailable,
		codes.DataLoss,
		codes.ResourceExhausted,
		codes.Internal,
		codes.Unknown:
		return exporterrors.Transport("export failed", cause)

	case codes.InvalidArgument, codes.FailedPrecondition:
		return exporterrors.Validation("receiver rejected the batch", cause)

	case codes.Unauthenticated, codes.PermissionDenied:
		// Deliberately retryable, matching the HTTP 401/403 decision the
		// cloud sink already makes: an auth blip or a slow key rotation
		// clears on its own, and the bounded queue caps the cost if the
		// credential is genuinely wrong.
		return exporterrors.Transport("export failed", cause)

	case codes.Unimplemented, codes.NotFound:
		// The endpoint does not speak this signal, or does not exist.
		// No amount of retrying changes that; an operator has to.
		return exporterrors.Configuration("endpoint does not accept this signal", cause)

	default:
		return exporterrors.Transport("export failed", cause)
	}
}

// httpStatusRe matches the status token the OTLP/HTTP exporter formats
// into its non-retryable error ("...: 400 Bad Request (body: ...)").
// Anchored on a space-delimited 3-digit code so a port number or a byte
// count elsewhere in the message cannot be mistaken for one.
var httpStatusRe = regexp.MustCompile(`(^|[^\d])([1-5]\d{2}) [A-Z][A-Za-z]`)

// httpStatusFromError extracts the HTTP status code from an OTLP/HTTP
// export error, reporting false when the message does not carry one.
func httpStatusFromError(err error) (int, bool) {
	m := httpStatusRe.FindStringSubmatch(err.Error())
	if m == nil {
		return 0, false
	}
	code, convErr := strconv.Atoi(m[2])
	if convErr != nil {
		return 0, false
	}
	return code, true
}
