package app

// Blank-import every probe package the agent binary should know about.
// Each package's init() calls probes.RegisterProbe(...) and populates
// the runtime catalogue. Without these imports the registry is empty
// and `config check` reports `unknown type "<probe>"` for everything.
//
// This file is the canonical place to add (or remove) a probe from
// the binary's catalogue:
//
//   - Add a line here when you create a new probe package.
//   - Remove a line here when you want a build to ship without a
//     given probe.
//
// In a future core/enterprise split, the paid probes will move out of
// this file (likely into a tag-gated companion file in the enterprise
// repo). The free-tier probes stay here unconditionally.
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
