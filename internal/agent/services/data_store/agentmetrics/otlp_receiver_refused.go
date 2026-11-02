package agentmetrics

import (
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
)

// appendOTLPReceiverRefused emits the OTLP/HTTP receiver refusal counters,
// one series per (signal, reason). Called right after the dropped-counter
// loop in BuildAgentRecords.
func appendOTLPReceiverRefused(records []otelmapper.OtelRecord) []otelmapper.OtelRecord {
	for key, n := range agentstate.GetOTLPReceiverRefusedBySignal() {
		records = append(records, otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp_receiver.refused",
			Unit:        "{request}",
			Type:        "counter",
			Attributes:  map[string]string{"signal": key.Signal, "reason": key.Reason},
			Value:       float64(n),
			Description: "Cumulative count of OTLP/HTTP requests the receiver refused, by signal and reason (bad_body, too_large, media_type, encoding, rate_limited, method, unauthorized, forbidden).",
		})
	}
	return records
}
