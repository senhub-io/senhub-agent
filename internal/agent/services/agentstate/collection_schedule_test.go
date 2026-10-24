package agentstate

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func pk(name string) ProbeKey { return ProbeKey{Name: name, Type: "t"} }

func TestCollectionStartedEndedInFlight(t *testing.T) {
	ResetCollectionScheduleForTest()
	t.Cleanup(ResetCollectionScheduleForTest)

	if got := CollectionsInFlight(); got != 0 {
		t.Fatalf("initial in-flight = %d, want 0", got)
	}

	CollectionStarted()
	CollectionStarted()
	if got := CollectionsInFlight(); got != 2 {
		t.Fatalf("in-flight after two starts = %d, want 2", got)
	}

	CollectionEnded()
	if got := CollectionsInFlight(); got != 1 {
		t.Fatalf("in-flight after one end = %d, want 1", got)
	}

	CollectionEnded()
	CollectionEnded() // floor at zero: never below 0
	if got := CollectionsInFlight(); got != 0 {
		t.Fatalf("in-flight after extra end = %d, want 0", got)
	}
}

func TestRecordCollectLagWindowMaxAndResetKeepKeys(t *testing.T) {
	ResetCollectionScheduleForTest()
	t.Cleanup(ResetCollectionScheduleForTest)

	RecordCollectLag(pk("cpu"), 2500*time.Millisecond)
	RecordCollectLag(pk("cpu"), 1500*time.Millisecond)
	RecordCollectLag(pk("cpu"), -time.Second) // counts as 0; max stays 2.5s
	RecordCollectLag(pk("memory"), 800*time.Millisecond)

	got := TakeCollectLagWindow("r")
	if got[pk("cpu")] != 2.5 || got[pk("memory")] != 0.8 || len(got) != 2 {
		t.Fatalf("first window = %v, want cpu=2.5 memory=0.8", got)
	}

	got = TakeCollectLagWindow("r")
	if got[pk("cpu")] != 0 || got[pk("memory")] != 0 || len(got) != 2 {
		t.Fatalf("second window = %v, want cpu=0 memory=0 with keys kept", got)
	}

	RecordCollectLag(pk("cpu"), 1200*time.Millisecond)
	got = TakeCollectLagWindow("r")
	if got[pk("cpu")] != 1.2 || got[pk("memory")] != 0 || len(got) != 2 {
		t.Fatalf("window after new cpu sample = %v, want cpu=1.2 memory=0", got)
	}
}

func TestCollectionScheduleEmptyProbeBecomesUnknown(t *testing.T) {
	ResetCollectionScheduleForTest()
	t.Cleanup(ResetCollectionScheduleForTest)

	RecordCollectLag(pk(""), time.Second)
	IncrementCollectOverruns(pk(""), 1)
	RecordFirstCollectionEnd(pk(""), time.Now())

	if _, ok := TakeCollectLagWindow("r")[pk("unknown")]; !ok {
		t.Fatal("empty probe should be recorded under unknown in the lag window")
	}
	if _, ok := GetCollectOverruns()[pk("unknown")]; !ok {
		t.Fatal("empty probe should be recorded under unknown in overruns")
	}
	if _, ok := GetFirstCollectionDelays()[pk("unknown")]; !ok {
		t.Fatal("empty probe should be recorded under unknown in first-collection delays")
	}
}

func TestIncrementCollectOverrunsAccumulatesAndIgnoresNonPositive(t *testing.T) {
	ResetCollectionScheduleForTest()
	t.Cleanup(ResetCollectionScheduleForTest)

	IncrementCollectOverruns(pk("cpu"), 2)
	IncrementCollectOverruns(pk("cpu"), 3)
	IncrementCollectOverruns(pk("cpu"), 0)
	IncrementCollectOverruns(pk("cpu"), -1)

	got := GetCollectOverruns()
	if len(got) != 1 || got[pk("cpu")] != 5 {
		t.Fatalf("overruns = %v, want cpu=5", got)
	}
}

func withProcessStart(t *testing.T, start time.Time) {
	t.Helper()
	ResetCollectionScheduleForTest()
	prev := ProcessStart()
	SetProcessStartForTest(start)
	t.Cleanup(func() {
		SetProcessStartForTest(prev)
		ResetCollectionScheduleForTest()
	})
}

func TestFirstCollectionDelayIsMeasuredFromProcessStartPerInstance(t *testing.T) {
	start := time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
	withProcessStart(t, start)

	RecordFirstCollectionEnd(ProbeKey{Name: "a", Type: "cpu"}, start.Add(1100*time.Millisecond))
	RecordFirstCollectionEnd(ProbeKey{Name: "b", Type: "cpu"}, start.Add(2200*time.Millisecond))
	RecordFirstCollectionEnd(ProbeKey{Name: "a", Type: "memory"}, start.Add(500*time.Millisecond))

	got := GetFirstCollectionDelays()
	want := map[ProbeKey]float64{
		{Name: "a", Type: "cpu"}:    1.1,
		{Name: "b", Type: "cpu"}:    2.2,
		{Name: "a", Type: "memory"}: 0.5,
	}
	if len(got) != len(want) {
		t.Fatalf("first delays = %v, want one series per instance %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("first delays = %v, want %v", got, want)
		}
	}
}

func TestFirstCollectionDelayIsRecordedOncePerInstance(t *testing.T) {
	start := time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
	withProcessStart(t, start)

	RecordFirstCollectionEnd(pk("a"), start.Add(time.Second))
	RecordFirstCollectionEnd(pk("a"), start.Add(time.Hour)) // a reload of the same probe

	if got := GetFirstCollectionDelays()[pk("a")]; got != 1 {
		t.Fatalf("first delay = %v, want 1 (a later call of the same instance must not count)", got)
	}
}

func TestCollectLagWindowsAreIndependentPerReader(t *testing.T) {
	ResetCollectionScheduleForTest()
	t.Cleanup(ResetCollectionScheduleForTest)

	RecordCollectLag(pk("cpu"), 3*time.Second)
	if got := TakeCollectLagWindow("prometheus")[pk("cpu")]; got != 3 {
		t.Fatalf("prometheus first window = %v, want 3 (everything since the process start)", got)
	}
	if got := TakeCollectLagWindow("otlp")[pk("cpu")]; got != 3 {
		t.Fatalf("otlp first window = %v, want 3", got)
	}

	RecordCollectLag(pk("cpu"), 2*time.Second)
	if got := TakeCollectLagWindow("prometheus")[pk("cpu")]; got != 2 {
		t.Fatalf("prometheus second window = %v, want 2", got)
	}

	RecordCollectLag(pk("cpu"), time.Second)
	if got := TakeCollectLagWindow("prometheus")[pk("cpu")]; got != 1 {
		t.Fatalf("prometheus third window = %v, want 1", got)
	}
	// otlp has not read since the 3s window: it sees the max of 2s and 1s,
	// untouched by the two prometheus reads in between.
	if got := TakeCollectLagWindow("otlp")[pk("cpu")]; got != 2 {
		t.Fatalf("otlp second window = %v, want 2", got)
	}
}

func TestResetCollectionScheduleForTestClearsAll(t *testing.T) {
	CollectionStarted()
	RecordCollectLag(pk("cpu"), time.Second)
	IncrementCollectOverruns(pk("cpu"), 2)
	RecordFirstCollectionEnd(pk("cpu"), time.Now())

	ResetCollectionScheduleForTest()
	t.Cleanup(ResetCollectionScheduleForTest)

	if got := CollectionsInFlight(); got != 0 {
		t.Errorf("in-flight after reset = %d, want 0", got)
	}
	if got := TakeCollectLagWindow("r"); len(got) != 0 {
		t.Errorf("lag window after reset = %v, want empty", got)
	}
	if got := GetCollectOverruns(); len(got) != 0 {
		t.Errorf("overruns after reset = %v, want empty", got)
	}
	if got := GetFirstCollectionDelays(); len(got) != 0 {
		t.Errorf("first delays after reset = %v, want empty", got)
	}
}

func TestCollectionScheduleConcurrent(t *testing.T) {
	ResetCollectionScheduleForTest()
	t.Cleanup(ResetCollectionScheduleForTest)

	const workers = 32
	const loops = 100
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < loops; i++ {
				CollectionStarted()
				CollectionEnded()
				RecordCollectLag(pk("cpu"), time.Duration(i%17)*time.Millisecond)
				IncrementCollectOverruns(pk("cpu"), 1)
				RecordFirstCollectionEnd(ProbeKey{Name: fmt.Sprint(seed, i%11), Type: "cpu"}, time.Now())
			}
		}(w)
	}

	wg.Wait()

	if got := CollectionsInFlight(); got != 0 {
		t.Errorf("in-flight after concurrent test = %d, want 0", got)
	}

	overruns := GetCollectOverruns()
	if overruns[pk("cpu")] != workers*loops {
		t.Errorf("overruns[cpu] = %d, want %d", overruns[pk("cpu")], workers*loops)
	}

	lag := TakeCollectLagWindow("r")
	if want := (16 * time.Millisecond).Seconds(); lag[pk("cpu")] != want {
		t.Errorf("lag[cpu] = %v, want %v", lag[pk("cpu")], want)
	}

	if got := len(GetFirstCollectionDelays()); got != workers*11 {
		t.Errorf("first delays recorded for %d instances, want %d", got, workers*11)
	}
}

func TestTwoProbesOfOneTypeAreTwoSeries(t *testing.T) {
	start := time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
	withProcessStart(t, start)
	t.Cleanup(ResetCollectionScheduleForTest)

	a := ProbeKey{Name: "web-a", Type: "http"}
	b := ProbeKey{Name: "web-b", Type: "http"}
	RecordCollectLag(a, time.Second)
	RecordCollectLag(b, 2*time.Second)
	IncrementCollectOverruns(a, 1)
	IncrementCollectOverruns(b, 4)
	RecordFirstCollectionEnd(a, start.Add(time.Second))
	RecordFirstCollectionEnd(b, start.Add(5*time.Second))

	lag := TakeCollectLagWindow("r")
	if len(lag) != 2 || lag[a] != 1 || lag[b] != 2 {
		t.Errorf("lag = %v, want web-a=1 web-b=2", lag)
	}
	over := GetCollectOverruns()
	if len(over) != 2 || over[a] != 1 || over[b] != 4 {
		t.Errorf("overruns = %v, want web-a=1 web-b=4", over)
	}
	first := GetFirstCollectionDelays()
	if len(first) != 2 || first[a] != 1 || first[b] != 5 {
		t.Errorf("first delays = %v, want web-a=1 web-b=5", first)
	}
}
