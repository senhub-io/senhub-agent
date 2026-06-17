package snmppoll

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("snmp_poll", NewSnmpPollProbe) }
