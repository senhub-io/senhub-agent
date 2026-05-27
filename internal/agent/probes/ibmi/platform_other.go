//go:build !linux

package ibmi

import (
	"fmt"
	"runtime"
)

// platformSupported reports whether the JT400 bridge has been validated
// on the current GOOS. Set to false on every non-linux build so the
// probe registry can still parse `type: ibmi` configurations but the
// constructor refuses at instantiation time with a clear error.
//
// Why Linux only: the JT400 native runner (GraalVM) and the legacy
// Java fallback have been hardened only against the Linux runtime. On
// Windows the bridge subprocess can wedge the agent's logger after a
// respawn (see incident audit captured during 0.1.96-beta validation
// on the Windows test host — 37-minute log silences correlated with ibmi doCall
// durations of 2200s+). Re-enable per platform once each is shipped
// with its own stall-resilience test pass.
const platformSupported = false

// platformGate returns a descriptive error on unsupported platforms.
// Callers should surface this verbatim to the operator so they know to
// remove the ibmi probe from their config (or move the workload to a
// Linux host).
func platformGate() error {
	return fmt.Errorf(
		"ibmi probe is only supported on linux (current platform: %s); "+
			"remove `type: ibmi` from your probes config or run the agent on a Linux host",
		runtime.GOOS,
	)
}
