package agentstate

import "sync/atomic"

// otlpKeepalivePoints counts OTLP keepalive records successfully
// exported by the metrics strategy.
var otlpKeepalivePoints atomic.Uint64

// AddOTLPKeepalivePoints records `n` keepalive records exported after
// the gauge filter has been applied.
func AddOTLPKeepalivePoints(n int) {
	if n <= 0 {
		return
	}
	otlpKeepalivePoints.Add(uint64(n))
}

// GetOTLPKeepalivePointsTotal returns the cumulative number of exported
// OTLP keepalive records.
func GetOTLPKeepalivePointsTotal() uint64 {
	return otlpKeepalivePoints.Load()
}
