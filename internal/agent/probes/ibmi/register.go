package ibmi

import "senhub-agent.go/probesdk/probes"

func init() { probes.RegisterProbe("ibmi", NewIBMiProbe) }
