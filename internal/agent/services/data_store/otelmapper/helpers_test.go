package otelmapper

import "math"

func floatApprox(a, b float64) bool {
	if a == b {
		return true
	}
	return math.Abs(a-b) < 1e-9
}
