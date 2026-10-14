package exporthook

import (
	"sync"
	"testing"
	"time"
)

func TestNothingRegisteredIsInert(t *testing.T) {
	reset()
	defer reset()
	if HasSeriesObservers() {
		t.Error("no observer registered, HasSeriesObservers = true")
	}
	if Authorized("series_metering") {
		t.Error("no authorizer installed, a feature must not be allowed")
	}
	if got := SelfRecords(time.Now()); len(got) != 0 {
		t.Errorf("SelfRecords = %v, want none", got)
	}
	ObserveSeries("otlp", []SeriesPoint{{Name: "a"}}, time.Now())
}

func TestObserversProvidersAndAuthorizer(t *testing.T) {
	reset()
	defer reset()
	var seen []string
	RegisterSeriesObserver(func(output string, pts []SeriesPoint, _ time.Time) {
		for _, p := range pts {
			seen = append(seen, output+"/"+p.Probe+"/"+p.Name)
		}
	})
	RegisterSelfRecordProvider(func(time.Time) []SelfRecord {
		return []SelfRecord{{Name: "x", Type: "gauge", Value: 2}}
	})
	SetAuthorizer(func(f string) bool { return f == "series_metering" })

	ObserveSeries("otlp", []SeriesPoint{{Probe: "cpu", Name: "system.cpu.time"}}, time.Now())
	if len(seen) != 1 || seen[0] != "otlp/cpu/system.cpu.time" {
		t.Errorf("observer saw %v", seen)
	}
	if recs := SelfRecords(time.Now()); len(recs) != 1 || recs[0].Value != 2 {
		t.Errorf("SelfRecords = %v", recs)
	}
	if !Authorized("series_metering") || Authorized("other") {
		t.Error("authorizer not applied")
	}
	SetAuthorizer(nil)
	if Authorized("series_metering") {
		t.Error("a cleared authorizer must allow nothing")
	}
}

func TestConcurrentUse(t *testing.T) {
	reset()
	defer reset()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			RegisterSeriesObserver(func(string, []SeriesPoint, time.Time) {})
		}()
		go func() {
			defer wg.Done()
			ObserveSeries("otlp", nil, time.Now())
			_ = SelfRecords(time.Now())
		}()
		go func() {
			defer wg.Done()
			SetAuthorizer(func(string) bool { return true })
			_ = Authorized("f")
		}()
	}
	wg.Wait()
}
