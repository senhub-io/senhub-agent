package sensor

// Blank-imports for the test binary only — pull in every probe
// package's init() so probes.RegisterProbe(...) populates the runtime
// catalogue. Without this, sensor tests that synthesise a config like
// `type: cpu` get "unknown probe type" because the package they're
// testing knows about the registry but cannot trigger its
// population from production code (that's cmd/agent's job).
//
// File name uses the `_test.go` suffix so it never lands in a
// production binary; if `sensor` is ever consumed by a non-test
// caller other than cmd/agent, the production wiring stays
// cmd/agent's responsibility.

import (
	_ "senhub-agent.go/internal/agent/probes/citrix"
	_ "senhub-agent.go/internal/agent/probes/cpu"
	_ "senhub-agent.go/internal/agent/probes/event"
	_ "senhub-agent.go/internal/agent/probes/gateway"
	_ "senhub-agent.go/internal/agent/probes/host"
	_ "senhub-agent.go/internal/agent/probes/ibmi"
	_ "senhub-agent.go/internal/agent/probes/linuxlogs"
	_ "senhub-agent.go/internal/agent/probes/logicaldisk"
	_ "senhub-agent.go/internal/agent/probes/memory"
	_ "senhub-agent.go/internal/agent/probes/mysql"
	_ "senhub-agent.go/internal/agent/probes/netscaler"
	_ "senhub-agent.go/internal/agent/probes/network"
	_ "senhub-agent.go/internal/agent/probes/postgresql"
	_ "senhub-agent.go/internal/agent/probes/redfish"
	_ "senhub-agent.go/internal/agent/probes/syslog"
	_ "senhub-agent.go/internal/agent/probes/veeam"
	_ "senhub-agent.go/internal/agent/probes/webapp"
)
