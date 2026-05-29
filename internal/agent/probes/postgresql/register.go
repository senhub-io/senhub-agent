package postgresql

import "senhub-agent.go/probesdk/probes"

func init() { probes.RegisterProbe("postgresql", NewPostgreSQLProbe) }
