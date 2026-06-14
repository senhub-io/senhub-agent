package app

// Blank-import every probe package the open-source agent binary ships.
// Each package's init() calls probes.RegisterProbe(...) and populates
// the runtime catalogue. Without these imports the registry is empty
// and `config check` reports `unknown type "<probe>"` for everything.
//
// This file is the canonical catalogue for the OSS build: the
// host-local probes whose source lives in this public repository.
//
//   - Add a line here when you create a new public probe package.
//   - Remove a line here to ship a build without a given probe.
//
// The paid probes (citrix, veeam, netscaler, redfish, ibmi, mysql,
// postgresql, webapp, gateway) are NOT imported here — they live in the
// senhub-agent-enterprise module and are blank-imported by that repo's
// own cmd/agent entrypoint, which reuses app.Main(). Keeping them out of
// this file is what makes the default build the OSS edition; it also
// prevents a duplicate-registration panic when the enterprise entrypoint
// adds them on top of this set.
import (
	_ "senhub-agent.go/internal/agent/probes/consul"
	_ "senhub-agent.go/internal/agent/probes/cpu"
	_ "senhub-agent.go/internal/agent/probes/dnslatency"
	_ "senhub-agent.go/internal/agent/probes/event"
	_ "senhub-agent.go/internal/agent/probes/execprobe"
	_ "senhub-agent.go/internal/agent/probes/filetail"
	_ "senhub-agent.go/internal/agent/probes/host"
	_ "senhub-agent.go/internal/agent/probes/httpcheck"
	_ "senhub-agent.go/internal/agent/probes/icmpcheck"
	_ "senhub-agent.go/internal/agent/probes/linuxlogs"
	_ "senhub-agent.go/internal/agent/probes/logicaldisk"
	_ "senhub-agent.go/internal/agent/probes/memory"
	_ "senhub-agent.go/internal/agent/probes/network"
	_ "senhub-agent.go/internal/agent/probes/otlpreceiver"
	_ "senhub-agent.go/internal/agent/probes/promscrape"
	_ "senhub-agent.go/internal/agent/probes/snmppoll"
	_ "senhub-agent.go/internal/agent/probes/snmptrap"
	_ "senhub-agent.go/internal/agent/probes/syslog"
	_ "senhub-agent.go/internal/agent/probes/tcpdial"
	_ "senhub-agent.go/internal/agent/probes/windowseventlog"
)
