package process

import "testing"

func TestTopProcesses_RanksOnTheChosenMeasure(t *testing.T) {
	snaps := func() []processSnapshot {
		return []processSnapshot{
			{name: "busy", cpuPct: 90, rss: 10},
			{name: "fat", cpuPct: 1, rss: 5_000_000_000},
			{name: "idle", cpuPct: 0, rss: 20},
		}
	}
	if got := topProcesses(snaps(), 1, topByMemory); got[0].name != "fat" {
		t.Errorf("top by memory = %q, want fat", got[0].name)
	}
	if got := topProcesses(snaps(), 1, topByCPU); got[0].name != "busy" {
		t.Errorf("top by cpu = %q, want busy", got[0].name)
	}
}

func TestParseConfig_TopBy(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{"filter": map[string]interface{}{"top_n": 5, "top_by": "memory"}})
	if err != nil || cfg.topBy != topByMemory {
		t.Fatalf("top_by memory: cfg.topBy=%q err=%v", cfg.topBy, err)
	}
	if cfg, _ := parseConfig(map[string]interface{}{}); cfg.topBy != topByCPU {
		t.Errorf("default top_by = %q, want cpu", cfg.topBy)
	}
	if _, err := parseConfig(map[string]interface{}{"filter": map[string]interface{}{"top_by": "disk"}}); err == nil {
		t.Error("an unknown top_by must be refused")
	}
}
