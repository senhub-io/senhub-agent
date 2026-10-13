package agentstate

import "testing"

func TestSeedExportSendFailedDeclaresZeroOncePerReason(t *testing.T) {
	SeedExportSendFailed("seedtest")
	IncrementExportSendFailed("seedtest", "transport")
	SeedExportSendFailed("seedtest")
	got := map[string]uint64{}
	for _, f := range GetExportSendFailed() {
		if f.Strategy == "seedtest" {
			got[f.Reason] = f.Count
		}
	}
	want := map[string]uint64{"transport": 1, "validation": 0, "configuration": 0}
	for r, n := range want {
		if v, ok := got[r]; !ok || v != n {
			t.Errorf("reason %s = %d (present %v), want %d", r, v, ok, n)
		}
	}
}
