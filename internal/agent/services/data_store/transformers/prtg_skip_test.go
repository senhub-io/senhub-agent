package transformers

import "testing"

// The per-process series carry the process id in their identity. PRTG
// keeps every channel it has seen, so each process start left a dead
// channel on the sensor (163 channels for 75 served on the bench). The
// definitions keep them out of PRTG; the per-name roll-up stays.
func TestPerProcessSeriesStayOutOfPRTG(t *testing.T) {
	defs, err := Definitions()
	if err != nil {
		t.Fatal(err)
	}
	def := defs["process"]
	tr := &DefinitionBasedTransformer{definition: &def, moduleLogger: createTestModuleLogger()}
	for _, m := range def.Metrics {
		pidKeyed := false
		for _, l := range m.MultiInstanceLabels {
			if l == "process.pid" {
				pidKeyed = true
			}
		}
		if got := SkipsPRTG(tr, m.Name); got != pidKeyed {
			t.Errorf("%s: SkipsPRTG = %v, want %v (keyed on process.pid: %v)", m.Name, got, pidKeyed, pidKeyed)
		}
	}
}
