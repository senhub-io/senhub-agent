package transformers

import (
	"strings"
	"testing"
)

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

// IBM i per-job series are named after the job, whose number changes at
// every run ("851034/QUSER/QP0ZSPWT"). In PRTG each run left a dead
// channel: 2,583 channels for 1,447 served on the bench, enough to
// saturate the PRTG server. The per-job metrics stay out of PRTG.
func TestIBMiPerJobSeriesStayOutOfPRTG(t *testing.T) {
	defs, err := Definitions()
	if err != nil {
		t.Fatal(err)
	}
	def := defs["ibmi"]
	tr := &DefinitionBasedTransformer{definition: &def, moduleLogger: createTestModuleLogger()}
	for _, m := range def.Metrics {
		perJob := strings.HasPrefix(m.Name, "ibmi.job.")
		if got := SkipsPRTG(tr, m.Name); got != perJob {
			t.Errorf("%s: SkipsPRTG = %v, want %v", m.Name, got, perJob)
		}
	}
}
