package gateway

import "senhub-agent.go/probesdk/probes"

func init() { probes.RegisterProbe("ping_gateway", NewPingGatewayProbe) }
