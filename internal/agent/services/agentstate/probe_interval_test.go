package agentstate

import "testing"

func TestProbeIntervalByName(t *testing.T) {
	SetActiveProbeIntervals(map[string]int{"CPU": 60})
	t.Cleanup(func() { SetActiveProbeIntervals(nil) })

	if got, ok := ProbeIntervalByName("cpu"); !ok || got != 60 {
		t.Fatalf("ProbeIntervalByName(\"cpu\") = %d, %v; want 60, true", got, ok)
	}
	if _, ok := ProbeIntervalByName("memory"); ok {
		t.Fatal("ProbeIntervalByName(\"memory\") found an interval; want none")
	}
}
