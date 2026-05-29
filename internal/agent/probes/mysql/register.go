package mysql

import "senhub-agent.go/probesdk/probes"

func init() { probes.RegisterProbe("mysql", NewMySQLProbe) }
