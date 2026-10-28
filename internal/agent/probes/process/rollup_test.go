package process

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestAnUnfilteredViewEmitsNoPerProcessSeries(t *testing.T) {
	// Every process on the machine, with the process id in the identity
	// of six series each, is what grew to four and a half thousand
	// series and fifty more a minute.
	if (config{}).detailed() {
		t.Fatal("an unfiltered view must report the roll-up alone")
	}
}

func TestNamingWhatToWatchBringsTheDetailBack(t *testing.T) {
	for _, c := range []config{
		{byName: regexp.MustCompile("^nginx")},
		{byUser: "www-data"},
		{topN: 10, perPID: true},
	} {
		if !c.detailed() {
			t.Errorf("%+v must report the per-process detail: the operator either named what to watch or bounded the sample", c)
		}
	}
}

// top_n alone reports the per-name roll-up: its membership churns, and a
// series keyed on the process id would never stop appearing. Per-process
// series under top_n are an explicit opt-in, and still do not feed Toise.
func TestTopNIsARollUpUnlessPerPIDIsAsked(t *testing.T) {
	c := config{topN: 5}
	if c.detailed() || !c.topByName() {
		t.Error("top_n alone must report the roll-up, not per-process series")
	}
	if c := (config{topN: 5, perPID: true}); !c.detailed() || c.topByName() {
		t.Error("detail.per_pid under top_n must keep the per-process series")
	}
	p, err := NewProcessProbe(map[string]interface{}{
		"filter": map[string]interface{}{"top_n": 5},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pp, ok := p.(*processProbe); ok && pp.entitySrc != nil {
		t.Error("a churning sample must not become Toise entities")
	}
}

func TestNamingWhatToWatchFeedsTheGraphToo(t *testing.T) {
	p, err := NewProcessProbe(map[string]interface{}{
		"filter": map[string]interface{}{"by_name": "^nginx"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pp, ok := p.(*processProbe)
	if !ok || pp.entitySrc == nil {
		t.Error("a named watch list is an inventory and belongs on the graph")
	}
}

func TestKernelWorkersAreCountedUnderOneName(t *testing.T) {
	for _, n := range []string{"kworker/1:1-ata_sff", "kworker/u4:2-events_freezable_power_", "kworker/0:0H"} {
		if got := rollUpName(n); got != "kworker" {
			t.Errorf("rollUpName(%q) = %q, want kworker", n, got)
		}
	}
	for _, n := range []string{"nginx", "ksoftirqd/0", "kworkerd"} {
		if got := rollUpName(n); got != n {
			t.Errorf("rollUpName(%q) = %q, want it unchanged", n, got)
		}
	}
}

func rollUpKeys(snaps []processSnapshot) map[string]bool {
	keys := map[string]bool{}
	for _, dp := range appendRollUpPoints(nil, time.Unix(0, 0), snaps, nil) {
		id := dp.Name
		for _, tg := range dp.Tags {
			id += "|" + tg.Key + "=" + tg.Value
		}
		keys[id] = true
	}
	return keys
}

func TestTopNSeriesKeysAreStableAcrossAChangingPIDSet(t *testing.T) {
	cycle1 := []processSnapshot{
		{pid: 100, createTime: 1000, name: "worker", cpuPct: 50, rss: 10},
		{pid: 101, createTime: 1001, name: "worker", cpuPct: 40, rss: 10},
		{pid: 7, createTime: 1, name: "db", cpuPct: 20, rss: 10},
		{pid: 300, createTime: 3000, name: "tiny", cpuPct: 0, rss: 1},
	}
	cycle2 := []processSnapshot{
		{pid: 900, createTime: 9000, name: "worker", cpuPct: 60, rss: 10},
		{pid: 7, createTime: 1, name: "db", cpuPct: 20, rss: 10},
		{pid: 901, createTime: 9001, name: "tiny", cpuPct: 0, rss: 1},
		{pid: 902, createTime: 9002, name: "tiny", cpuPct: 0, rss: 1},
	}
	k1 := rollUpKeys(topProcessNames(cycle1, 2, topByCPU))
	k2 := rollUpKeys(topProcessNames(cycle2, 2, topByCPU))
	if len(k1) == 0 || len(k1) != len(k2) {
		t.Fatalf("series sets differ in size: %d vs %d", len(k1), len(k2))
	}
	for k := range k1 {
		if !k2[k] {
			t.Errorf("series %q present in one cycle only", k)
		}
		if strings.Contains(k, "process.pid") || strings.Contains(k, "process.creation.time") {
			t.Errorf("series %q carries a per-process identity", k)
		}
	}
}

func TestTopProcessNamesRanksNamesBySummedMeasure(t *testing.T) {
	snaps := []processSnapshot{
		{pid: 1, name: "a", cpuPct: 30},
		{pid: 2, name: "a", cpuPct: 30},
		{pid: 3, name: "b", cpuPct: 50},
		{pid: 4, name: "c", cpuPct: 10},
	}
	got := topProcessNames(snaps, 1, topByCPU)
	if len(got) != 2 || got[0].name != "a" || got[1].name != "a" {
		t.Errorf("top 1 by summed cpu = %+v, want both a processes", got)
	}
}

func TestParseConfig_PerPID(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"filter": map[string]interface{}{"top_n": 5},
		"detail": map[string]interface{}{"per_pid": true},
	})
	if err != nil || !cfg.perPID {
		t.Fatalf("per_pid: cfg.perPID=%v err=%v", cfg.perPID, err)
	}
	if cfg, _ := parseConfig(map[string]interface{}{}); cfg.perPID {
		t.Error("per_pid must default to false")
	}
}
