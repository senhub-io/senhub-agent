package dbcommon

import (
	"fmt"

	"senhub-agent.go/internal/agent/services/entity"
)

// FallbackInstanceID builds the degraded db.instance.id used when a datastore
// exposes no stable server-side identifier — the documented "address:port"
// fallback — and host-scopes it when the address is loopback.
//
// A loopback address is the same string on every machine, so "127.0.0.1:3306"
// names a different database on each host while reading as one identity. In the
// field that is not a theoretical collision: two MariaDB instances on separate
// hosts collapsed into a single entity whose version flipped between them every
// few minutes, and the entity's telemetry join keys pointed at the wrong
// machine — attributes describing one host, keys resolving to another (#740).
//
// Scoping replaces the loopback address with the agent host's own id, giving
// "<host.id>:3306". That is the same shape service.listener already mints
// ("<host.id>:9467/tcp"), so the two spellings of "something local on a port"
// agree instead of each inventing their own.
//
// A routable address is left alone: it already distinguishes the target, and
// rewriting it would re-key every remote database for no gain. When hostID is
// unreadable the raw form is returned unchanged — a wrong-but-stable id beats
// an id that changes shape depending on whether a lookup succeeded.
//
// Host-scoping also lifts the collapse guard in entity.LocalRunsOn, which
// refuses a runs_on edge from an identity embedding the loopback literal. So a
// local database gains the host anchor it could not have before.
func FallbackInstanceID(address string, port int, hostID string) string {
	if hostID != "" && entity.IsLoopbackHost(address) {
		return fmt.Sprintf("%s:%d", hostID, port)
	}
	return fmt.Sprintf("%s:%d", address, port)
}
