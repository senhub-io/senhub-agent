package clickhouse

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe(ProbeType, NewClickHouseProbe) }
