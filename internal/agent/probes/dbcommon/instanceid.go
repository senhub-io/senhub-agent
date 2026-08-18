package dbcommon

import (
	"fmt"
	"strings"

	"senhub-agent.go/internal/agent/services/entity"
)

// FallbackInstanceID builds the degraded db.instance.id used when a datastore
// exposes no stable server-side identifier, host-scoping it when the address
// is loopback.
//
// A loopback address is the same string on every machine, so "127.0.0.1:3306"
// names a different database on each host while reading as one identity. In the
// field that was not a theoretical collision: two MariaDB instances on separate
// hosts collapsed into a single entity whose version flipped between them every
// few minutes, and the entity's telemetry join keys pointed at the wrong
// machine — attributes describing one host, keys resolving to another (#740).
//
// The local form is <db.system.name>:<port>@<host.id>, agreed with the topology
// consumer. Three properties are wanted, and the shape is the smallest one that
// has all three:
//
//   - @<host.id> is the form their contract already prescribes for a local
//     thing with no stable id (service.instance uses <service.name>@<host.id>).
//     Reusing it means the answer to "how do we name this" is written once.
//   - The port stays, because two Redis on one machine must differ without
//     asking the operator to configure anything — but it is a discriminant
//     INSIDE a host, no longer the base of the identity.
//   - The system name keeps it distinct from the service.listener minted for
//     the same socket. Those are near-neighbours, not twins (a listener carries
//     a /<transport> suffix, so the strings were never equal), but two
//     identities that differ only by a suffix invite a reader to conclude they
//     name the same thing. Vocabularies that resemble each other too closely
//     are more treacherous than disjoint ones.
//
// A routable address is left alone: it already distinguishes the target, and
// rewriting it would re-key every remote database for no defect. When hostID or
// the system name is unavailable the raw address:port form is returned — a
// wrong-but-stable id beats one whose shape depends on whether a lookup
// succeeded.
//
// Host-scoping also lifts the collapse guard in entity.LocalRunsOn, which
// refuses a runs_on edge from an identity embedding the loopback literal. So a
// local database gains the host anchor it could not have before, which puts it
// inside the impact radius of its host for the first time.
func FallbackInstanceID(system, address string, port int, hostID string) string {
	system = strings.TrimSpace(system)
	if hostID != "" && system != "" && entity.IsLoopbackHost(address) {
		return fmt.Sprintf("%s:%d@%s", system, port, hostID)
	}
	return fmt.Sprintf("%s:%d", address, port)
}

// legacyFallbackInstanceID is the pre-0.5.4 form: the bare address:port that
// collapsed two local databases into one entity. Kept only so the re-key
// announcement can name the node it retires — nothing emits it any more.
func legacyFallbackInstanceID(address string, port int) string {
	return fmt.Sprintf("%s:%d", address, port)
}
