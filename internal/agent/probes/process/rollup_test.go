package process

import (
	"regexp"
	"testing"
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
		{topN: 10},
	} {
		if !c.detailed() {
			t.Errorf("%+v must report the per-process detail: the operator either named what to watch or bounded the sample", c)
		}
	}
}

// The metric rail now follows the rule the entity rail already followed,
// with one difference that is deliberate: top_n bounds the series count
// but its membership still churns, so it feeds the detail and not Toise.
func TestTopNFeedsTheDetailButNotTheGraph(t *testing.T) {
	c := config{topN: 5}
	if !c.detailed() {
		t.Error("a bounded sample is safe for series")
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
