package types

import (
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestExplainBindErrorNamesTheWayOut(t *testing.T) {
	refused := &net.OpError{Op: "listen", Net: "udp", Err: os.NewSyscallError("bind", syscall.EACCES)}
	got := ExplainBindError(refused, 162)
	if !strings.Contains(got.Error(), "CAP_NET_BIND_SERVICE") || !errors.Is(got, os.ErrPermission) {
		t.Errorf("a refused privileged port is not explained: %v", got)
	}
	if ExplainBindError(refused, 1162) != error(refused) {
		t.Error("a high port refusal must be left as it is")
	}
	inUse := &net.OpError{Op: "listen", Net: "udp", Err: os.NewSyscallError("bind", syscall.EADDRINUSE)}
	if ExplainBindError(inUse, 162) != error(inUse) {
		t.Error("a port in use must not be explained as a privilege problem")
	}
}
