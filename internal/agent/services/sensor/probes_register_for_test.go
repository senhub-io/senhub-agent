package sensor

// Blank-imports for the test binary only — pull in every probe
// package's init() so probes.RegisterProbe(...) populates the runtime
// catalogue. Without this, sensor tests that synthesise a config like
// `type: cpu` get "unknown probe type" because the package they're
// testing knows about the registry but cannot trigger its
// population from production code (that's the entrypoint's job).
//
// Only the public (OSS) probe packages are imported here — the same
// set the open-source binary ships. The paid probes live in the
// senhub-agent-enterprise module; their sensor-level coverage runs in
// that repository's own test suite.
//
// File name uses the `_test.go` suffix so it never lands in a
// production binary.

import (
	_ "senhub-agent.go/internal/agent/probes/cpu"
	_ "senhub-agent.go/internal/agent/probes/event"
	_ "senhub-agent.go/internal/agent/probes/host"
	_ "senhub-agent.go/internal/agent/probes/linuxlogs"
	_ "senhub-agent.go/internal/agent/probes/logicaldisk"
	_ "senhub-agent.go/internal/agent/probes/memory"
	_ "senhub-agent.go/internal/agent/probes/network"
	_ "senhub-agent.go/internal/agent/probes/syslog"
)
