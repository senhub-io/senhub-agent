package veeam

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("veeam", NewVeeamProbe) }
