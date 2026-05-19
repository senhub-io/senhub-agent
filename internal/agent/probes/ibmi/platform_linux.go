//go:build linux

package ibmi

// platformSupported reports whether the JT400 bridge has been validated
// on the current GOOS. The probe registry still wires the constructor
// on every platform so a config that mentions `type: ibmi` parses, but
// instantiation calls platformGate() first.
const platformSupported = true

// platformGate returns nil on supported platforms.
func platformGate() error { return nil }
