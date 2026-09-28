package types

import (
	"errors"
	"fmt"
	"os"
)

// ExplainBindError adds what to do to a refused bind on a privileged port.
// The Linux service runs as an unprivileged account with every capability
// dropped, so a listener left on its standard port (514 for syslog, 162
// for traps) fails with a bare "permission denied" that names neither the
// cause nor the two ways out.
func ExplainBindError(err error, port int) error {
	if err == nil || port <= 0 || port >= 1024 || !errors.Is(err, os.ErrPermission) {
		return err
	}
	return fmt.Errorf("%w: port %d is below 1024; grant the service CAP_NET_BIND_SERVICE in a unit drop-in (see the least-privilege guide) or listen on a port above 1023", err, port)
}
