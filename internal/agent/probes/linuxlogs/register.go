package linuxlogs

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("linux_logs", NewLinuxLogsProbe) }
