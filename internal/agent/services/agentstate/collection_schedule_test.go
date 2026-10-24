package agentstate

import (
	"sync"
	"testing"
	"time"
)

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

	RecordCollectLag("cpu", 2500*time.Millisecond)
	RecordCollectLag("cpu", 1500*time.Millisecond)
	RecordCollectLag("cpu", -time.Second) // counts as 0; max stays 2.5s
	RecordCollectLag("memory", 800*time.Millisecond)

	got := TakeCollectLagWindow()
	if got["cpu"] != 2.5 || got["memory"] != 0.8 || len(got) != 2 {
		t.Fatalf("first window = %v, want cpu=2.5 memory=0.8", got)
	}

	got = TakeCollectLagWindow()
	if got["cpu"] != 0 || got["memory"] != 0 || len(got) != 2 {
		t.Fatalf("second window = %v, want cpu=0 memory=0 with keys kept", got)
	}

	RecordCollectLag("cpu", 1200*time.Millisecond)
	got = TakeCollectLagWindow()
	if got["cpu"] != 1.2 || got["memory"] != 0 || len(got) != 2 {
		t.Fatalf("window after new cpu sample = %v, want cpu=1.2 memory=0", got)
	}
}

func TestCollectionScheduleEmptyProbeBecomesUnknown(t *testing.T) {
	ResetCollectionScheduleForTest()
	t.Cleanup(ResetCollectionScheduleForTest)

	RecordCollectLag("", time.Second)
	IncrementCollectOverruns("", 1)
	RecordFirstCollectionDelay("", time.Second)

	if _, ok := TakeCollectLagWindow()["unknown"]; !ok {
		t.Fatal("empty probe should be recorded under unknown in the lag window")
	}
	if _, ok := GetCollectOverruns()["unknown"]; !ok {
		t.Fatal("empty probe should be recorded under unknown in overruns")
	}
	if _, ok := GetFirstCollectionDelays()["unknown"]; !ok {
		t.Fatal("empty probe should be recorded under unknown in first-collection delays")
	}
}

func TestIncrementCollectOverrunsAccumulatesAndIgnoresNonPositive(t *testing.T) {
	ResetCollectionScheduleForTest()
	t.Cleanup(ResetCollectionScheduleForTest)

	IncrementCollectOverruns("cpu", 2)
	IncrementCollectOverruns("cpu", 3)
	IncrementCollectOverruns("cpu", 0)
	IncrementCollectOverruns("cpu", -1)

	got := GetCollectOverruns()
	if len(got) != 1 || got["cpu"] != 5 {
		t.Fatalf("overruns = %v, want cpu=5", got)
	}
}

func TestRecordFirstCollectionDelayKeepsMax(t *testing.T) {
	ResetCollectionScheduleForTest()
	t.Cleanup(ResetCollectionScheduleForTest)

	RecordFirstCollectionDelay("cpu", 1100*time.Millisecond)
	RecordFirstCollectionDelay("cpu", 2200*time.Millisecond)
	RecordFirstCollectionDelay("cpu", 1800*time.Millisecond)
	RecordFirstCollectionDelay("memory", 500*time.Millisecond)

	got := GetFirstCollectionDelays()
	if got["cpu"] != 2.2 || got["memory"] != 0.5 || len(got) != 2 {
		t.Fatalf("first delays = %v, want cpu=2.2 memory=0.5", got)
	}
}

func TestResetCollectionScheduleForTestClearsAll(t *testing.T) {
	CollectionStarted()
	RecordCollectLag("cpu", time.Second)
	IncrementCollectOverruns("cpu", 2)
	RecordFirstCollectionDelay("cpu", 3*time.Second)

	ResetCollectionScheduleForTest()
	t.Cleanup(ResetCollectionScheduleForTest)

	if got := CollectionsInFlight(); got != 0 {
		t.Errorf("in-flight after reset = %d, want 0", got)
	}
	if got := TakeCollectLagWindow(); len(got) != 0 {
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
				RecordCollectLag("cpu", time.Duration(i%17)*time.Millisecond)
				IncrementCollectOverruns("cpu", 1)
				RecordFirstCollectionDelay("cpu", time.Duration(i%11)*time.Millisecond)
			}
		}(w)
	}

	wg.Wait()

	if got := CollectionsInFlight(); got != 0 {
		t.Errorf("in-flight after concurrent test = %d, want 0", got)
	}

	overruns := GetCollectOverruns()
	if overruns["cpu"] != workers*loops {
		t.Errorf("overruns[cpu] = %d, want %d", overruns["cpu"], workers*loops)
	}

	lag := TakeCollectLagWindow()
	if want := (16 * time.Millisecond).Seconds(); lag["cpu"] != want {
		t.Errorf("lag[cpu] = %v, want %v", lag["cpu"], want)
	}

	delays := GetFirstCollectionDelays()
	if want := (10 * time.Millisecond).Seconds(); delays["cpu"] != want {
		t.Errorf("first delay[cpu] = %v, want %v", delays["cpu"], want)
	}
}
