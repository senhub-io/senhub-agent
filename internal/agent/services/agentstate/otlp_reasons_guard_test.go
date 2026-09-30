package agentstate

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// Every reason the OTLP output drops a record for, and every signal it
// counts an export error on, must be in the lists emitted from 0;
// otherwise that series appears only after its first increment and a
// dashboard reads its absence as "no drop" (#972).
func TestOTLPLabelListsCoverTheOutput(t *testing.T) {
	files, err := filepath.Glob("../data_store/strategies/otlp/*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("OTLP sources not found: %v", err)
	}
	dropped := regexp.MustCompile(`IncrementOTLPDropped\("([^"]+)"\)`)
	exportErr := regexp.MustCompile(`IncrementOTLPExportErrors\("([^"]+)"\)`)
	for _, f := range files {
		if filepath.Ext(f) != ".go" || regexp.MustCompile(`_test\.go$`).MatchString(f) {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range dropped.FindAllStringSubmatch(string(src), -1) {
			if !slices.Contains(OTLPDropReasons, m[1]) {
				t.Errorf("%s drops for reason %q, missing from OTLPDropReasons", filepath.Base(f), m[1])
			}
		}
		for _, m := range exportErr.FindAllStringSubmatch(string(src), -1) {
			if !slices.Contains(OTLPSignals, m[1]) {
				t.Errorf("%s counts export errors for signal %q, missing from OTLPSignals", filepath.Base(f), m[1])
			}
		}
	}
}
