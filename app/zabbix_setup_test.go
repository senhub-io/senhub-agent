package app

import "testing"

// Naming a probe adds it to the ones every machine runs. Replacing them
// was a trap: an operator adding one commercial template silently
// unlinked the processor, the memory, the network and the disks from the
// autoregistration action, and every host registering afterwards came up
// with none of them.
func TestNamingAProbeAddsItToTheDefaultOnes(t *testing.T) {
	got := withDefaultProbes([]string{"veeam"})
	for _, want := range defaultSetupProbes {
		found := false
		for _, p := range got {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s is no longer linked when a probe is named; got %v", want, got)
		}
	}
	if got[len(got)-1] != "veeam" {
		t.Errorf("the named probe comes last; got %v", got)
	}
	if len(withDefaultProbes(nil)) != len(defaultSetupProbes) {
		t.Errorf("naming nothing links the defaults alone; got %v", withDefaultProbes(nil))
	}
	if len(withDefaultProbes([]string{defaultSetupProbes[0]})) != len(defaultSetupProbes) {
		t.Error("naming a probe that is already a default must not link it twice")
	}
}
