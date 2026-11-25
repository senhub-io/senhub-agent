package agentmetrics

// boolGauge turns a boolean into a 1/0 gauge value.
func boolGauge(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
