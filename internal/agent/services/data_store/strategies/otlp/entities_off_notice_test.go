package otlp

import (
	"strings"
	"testing"
)

func TestStartSaysWhenEntityEmissionIsOff(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		buf := &syncBuffer{}
		s := &OTLPSyncStrategy{logger: bufferModuleLogger(buf)}
		s.cfg.Entities.Enabled = enabled
		s.noticeEntitiesOff()
		said := strings.Contains(buf.String(), "signals.entities.enabled")
		if said == enabled {
			t.Errorf("entities enabled=%v: notice logged=%v, want %v", enabled, said, !enabled)
		}
	}
}
