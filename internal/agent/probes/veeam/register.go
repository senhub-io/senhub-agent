package veeam

import "senhub-agent.go/probesdk/probes"

func init() { probes.RegisterProbe("veeam", NewVeeamProbe) }
