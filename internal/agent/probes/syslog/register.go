package syslog

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("syslog", NewSyslogProbe) }
