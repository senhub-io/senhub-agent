package dbcommon

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
)

const CheckResultMetric = "senhub.probe.check.result"

// CheckResult classifies the outcome of a single probe check.
type CheckResult int

const (
	CheckOK CheckResult = iota
	CheckTargetDown
	CheckAuthDenied
	CheckTimeout
	CheckTLSError
	CheckConfigError
	CheckProtocolError
	CheckPermissionDenied
)

// ClassifyError maps an error returned by a probe attempt to a
// CheckResult. It first checks standard error types, then falls back
// to driver-specific message matching.
func ClassifyError(err error) CheckResult {
	if err == nil {
		return CheckOK
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if errors.Is(opErr.Err, syscall.ECONNREFUSED) ||
			errors.Is(opErr.Err, syscall.EHOSTUNREACH) ||
			errors.Is(opErr.Err, syscall.ENETUNREACH) ||
			errors.Is(opErr.Err, syscall.ECONNRESET) {
			return CheckTargetDown
		}
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return CheckTargetDown
	}

	// Some drivers (the MongoDB one among them) report a selection timeout
	// whose only trace of the real cause is the last dial error, flattened
	// into the message. A refusal there means the target is down, even though
	// the error chain itself ends on a deadline.
	msg := strings.ToLower(err.Error())
	if containsAny(msg, "connection refused", "no route to host", "network is unreachable",
		"connection reset by peer", "no such host") {
		return CheckTargetDown
	}

	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return CheckTimeout
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return CheckTimeout
	}

	var unknownAuthorityErr x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthorityErr) {
		return CheckTLSError
	}
	var invalidCertErr x509.CertificateInvalidError
	if errors.As(err, &invalidCertErr) {
		return CheckTLSError
	}
	var hostnameErr x509.HostnameError
	if errors.As(err, &hostnameErr) {
		return CheckTLSError
	}
	var recordHeaderErr tls.RecordHeaderError
	if errors.As(err, &recordHeaderErr) {
		return CheckTLSError
	}
	var certVerificationErr *tls.CertificateVerificationError
	if errors.As(err, &certVerificationErr) {
		return CheckTLSError
	}

	switch {
	case strings.Contains(msg, "error 1044"),
		strings.Contains(msg, "error 1142"),
		strings.Contains(msg, "error 1227"):
		return CheckPermissionDenied
	case strings.Contains(msg, "error 1049"):
		return CheckConfigError
	case strings.Contains(msg, "error 1045"),
		strings.Contains(msg, "access denied for user"):
		return CheckAuthDenied
	case strings.Contains(msg, "28p01"),
		strings.Contains(msg, "28000"),
		strings.Contains(msg, "password authentication failed"):
		return CheckAuthDenied
	case strings.Contains(msg, "42501"),
		strings.Contains(msg, "permission denied"):
		return CheckPermissionDenied
	case strings.Contains(msg, "3d000"):
		return CheckConfigError
	case strings.Contains(msg, "login failed for user"):
		return CheckAuthDenied
	case strings.Contains(msg, "ora-01017"):
		return CheckAuthDenied
	case strings.Contains(msg, "ora-01031"):
		return CheckPermissionDenied
	case strings.Contains(msg, "ora-12541"):
		return CheckTargetDown
	case strings.Contains(msg, "ora-12170"),
		strings.Contains(msg, "ora-12535"):
		return CheckTimeout
	case strings.Contains(msg, "noauth"),
		strings.Contains(msg, "wrongpass"),
		strings.Contains(msg, "invalid password"):
		return CheckAuthDenied
	case strings.Contains(msg, "noperm"):
		return CheckPermissionDenied
	case strings.Contains(msg, "authentication failed"),
		strings.Contains(msg, "auth error"):
		return CheckAuthDenied
	case strings.Contains(msg, "not authorized"),
		strings.Contains(msg, "unauthorized"):
		return CheckPermissionDenied
	case containsAny(msg, "i/o timeout", "server selection timeout", "deadline exceeded"):
		return CheckTimeout
	}

	if errors.As(err, &netErr) {
		return CheckTargetDown
	}
	return CheckProtocolError
}

// ClassifyHTTPStatus maps an HTTP status code to a CheckResult.
func ClassifyHTTPStatus(code int) CheckResult {
	switch {
	case code >= 200 && code < 400:
		return CheckOK
	case code == 401 || code == 407:
		return CheckAuthDenied
	case code == 403:
		return CheckPermissionDenied
	case code == 408 || code == 504:
		return CheckTimeout
	case code >= 400 && code < 500:
		return CheckConfigError
	case code >= 500 && code < 600:
		return CheckTargetDown
	default:
		return CheckProtocolError
	}
}

// String returns the lowercase human-readable name for a CheckResult.
func (c CheckResult) String() string {
	switch c {
	case CheckOK:
		return "ok"
	case CheckTargetDown:
		return "target_down"
	case CheckAuthDenied:
		return "auth_denied"
	case CheckTimeout:
		return "timeout"
	case CheckTLSError:
		return "tls_error"
	case CheckConfigError:
		return "config_error"
	case CheckProtocolError:
		return "protocol_error"
	case CheckPermissionDenied:
		return "permission_denied"
	default:
		return "unknown"
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
