package dbcommon

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
)

func TestClassifyError(t *testing.T) {
	wrap := func(err error) error {
		return fmt.Errorf("wrapped: %w", err)
	}

	tests := []struct {
		name string
		err  error
		want CheckResult
	}{
		{"nil", nil, CheckOK},
		{"net.OpError ECONNREFUSED", wrap(&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}), CheckTargetDown},
		{"net.OpError EHOSTUNREACH", wrap(&net.OpError{Op: "dial", Net: "tcp", Err: syscall.EHOSTUNREACH}), CheckTargetDown},
		{"net.OpError ENETUNREACH", wrap(&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ENETUNREACH}), CheckTargetDown},
		{"net.OpError ECONNRESET", wrap(&net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}), CheckTargetDown},
		{"net.DNSError IsNotFound", wrap(&net.DNSError{Err: "no such host", Name: "example.invalid", IsNotFound: true}), CheckTargetDown},
		{"os.ErrDeadlineExceeded", wrap(os.ErrDeadlineExceeded), CheckTimeout},
		{"context.DeadlineExceeded", wrap(context.DeadlineExceeded), CheckTimeout},
		{"net.Error Timeout", wrap(&net.DNSError{Err: "i/o timeout", IsTimeout: true}), CheckTimeout},
		{"x509.UnknownAuthorityError", wrap(x509.UnknownAuthorityError{}), CheckTLSError},
		{"x509.CertificateInvalidError", wrap(x509.CertificateInvalidError{Reason: x509.Expired}), CheckTLSError},
		{"x509.HostnameError", wrap(x509.HostnameError{Host: "example.com"}), CheckTLSError},
		{"tls.RecordHeaderError", wrap(tls.RecordHeaderError{Msg: "bad record MAC"}), CheckTLSError},
		{"tls.CertificateVerificationError", wrap(&tls.CertificateVerificationError{Err: errors.New("verification failed")}), CheckTLSError},
		{"mysql Error 1045", errors.New("Error 1045 (28000): Access denied for user 'root'@'localhost'"), CheckAuthDenied},
		{"mysql access denied for user", errors.New("Access denied for user 'root'@'localhost'"), CheckAuthDenied},
		{"mysql Error 1044", errors.New("Error 1044: Access denied for user 'root'@'localhost' to database 'db'"), CheckPermissionDenied},
		{"mysql Error 1142", errors.New("Error 1142: UPDATE command denied to user"), CheckPermissionDenied},
		{"mysql Error 1227", errors.New("Error 1227: Access denied; you need (at least one of) the SUPER privilege(s)"), CheckPermissionDenied},
		{"mysql Error 1049", errors.New("Error 1049: Unknown database 'db'"), CheckConfigError},
		{"postgres 28P01", errors.New("pq: 28P01"), CheckAuthDenied},
		{"postgres 28000", errors.New("pq: 28000"), CheckAuthDenied},
		{"postgres password authentication failed", errors.New("pq: password authentication failed for user \"postgres\""), CheckAuthDenied},
		{"postgres 42501", errors.New("pq: 42501"), CheckPermissionDenied},
		{"postgres permission denied", errors.New("pq: permission denied for relation pg_stat_replication"), CheckPermissionDenied},
		{"postgres 3D000", errors.New("pq: 3D000: database \"foo\" does not exist"), CheckConfigError},
		{"sql server login failed", errors.New("Login failed for user 'sa'"), CheckAuthDenied},
		{"oracle ORA-01017", errors.New("ORA-01017: invalid username/password; logon denied"), CheckAuthDenied},
		{"oracle ORA-01031", errors.New("ORA-01031: insufficient privileges"), CheckPermissionDenied},
		{"oracle ORA-12541", errors.New("ORA-12541: TNS:no listener"), CheckTargetDown},
		{"oracle ORA-12170", errors.New("ORA-12170: TNS:Connect timeout occurred"), CheckTimeout},
		{"oracle ORA-12535", errors.New("ORA-12535: TNS:operation timed out"), CheckTimeout},
		{"redis NOAUTH", errors.New("NOAUTH Authentication required"), CheckAuthDenied},
		{"redis WRONGPASS", errors.New("WRONGPASS invalid username-password pair"), CheckAuthDenied},
		{"redis invalid password", errors.New("ERR invalid password"), CheckAuthDenied},
		{"redis NOPERM", errors.New("NOPERM this user has no permissions"), CheckPermissionDenied},
		{"mongo authentication failed", errors.New("Authentication failed"), CheckAuthDenied},
		{"mongo auth error", errors.New("auth error"), CheckAuthDenied},
		{"mongo not authorized", errors.New("not authorized on admin to execute command"), CheckPermissionDenied},
		{"mongo unauthorized", errors.New("Unauthorized"), CheckPermissionDenied},
		{"unrecognized net.Error", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("network problem")}, CheckTargetDown},
		{"unrecognized plain error", errors.New("unexpected reply from server"), CheckProtocolError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyError(tt.err); got != tt.want {
				t.Fatalf("ClassifyError(%v) = %s, want %s", tt.err, got, tt.want)
			}
		})
	}
}

func TestClassifyHTTPStatus(t *testing.T) {
	tests := []struct {
		name string
		code int
		want CheckResult
	}{
		{"2xx", 200, CheckOK},
		{"3xx", 301, CheckOK},
		{"401", 401, CheckAuthDenied},
		{"403", 403, CheckPermissionDenied},
		{"407", 407, CheckAuthDenied},
		{"408", 408, CheckTimeout},
		{"504", 504, CheckTimeout},
		{"other 4xx 400", 400, CheckConfigError},
		{"other 4xx 404", 404, CheckConfigError},
		{"5xx 500", 500, CheckTargetDown},
		{"5xx 503", 503, CheckTargetDown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyHTTPStatus(tt.code); got != tt.want {
				t.Fatalf("ClassifyHTTPStatus(%d) = %s, want %s", tt.code, got, tt.want)
			}
		})
	}
}

func TestCheckResultString(t *testing.T) {
	tests := []struct {
		c    CheckResult
		want string
	}{
		{CheckOK, "ok"},
		{CheckTargetDown, "target_down"},
		{CheckAuthDenied, "auth_denied"},
		{CheckTimeout, "timeout"},
		{CheckTLSError, "tls_error"},
		{CheckConfigError, "config_error"},
		{CheckProtocolError, "protocol_error"},
		{CheckPermissionDenied, "permission_denied"},
	}

	for _, tt := range tests {
		if got := tt.c.String(); got != tt.want {
			t.Fatalf("String() = %q, want %q", got, tt.want)
		}
	}
}
