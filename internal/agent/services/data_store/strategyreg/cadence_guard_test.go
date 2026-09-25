package strategyreg

import (
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/data_store/strategies/otlp"
	"senhub-agent.go/internal/agent/services/data_store/strategies/zabbix"
)

// The push outputs keep a probe's last value until its next run is due,
// which needs the data store to tell them each probe's cadence. It does
// so only for a strategy satisfying the interface, and a method that no
// longer matches it is never called and fails nowhere.
var (
	_ data_store.ProbeCadenceSink = (*otlp.OTLPSyncStrategy)(nil)
	_ data_store.ProbeCadenceSink = (*zabbix.Strategy)(nil)
)
