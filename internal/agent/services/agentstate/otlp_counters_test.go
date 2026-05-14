package agentstate

import (
	"sync"
	"testing"
)

func TestOTLPCounters_IncrementAndRead(t *testing.T) {
	resetOTLPCountersForTest()

	IncrementOTLPMetricsPushed(10)
	IncrementOTLPMetricsPushed(5)
	IncrementOTLPMetricsPushed(0)  // ignored
	IncrementOTLPMetricsPushed(-3) // ignored (defensive)

	IncrementOTLPLogsPushed()
	IncrementOTLPLogsPushed()
	IncrementOTLPLogsPushed()

	IncrementOTLPExportErrors()

	if got := GetOTLPMetricsPushedTotal(); got != 15 {
		t.Errorf("metrics.pushed=%d, want 15", got)
	}
	if got := GetOTLPLogsPushedTotal(); got != 3 {
		t.Errorf("logs.pushed=%d, want 3", got)
	}
	if got := GetOTLPExportErrorsTotal(); got != 1 {
		t.Errorf("export.errors=%d, want 1", got)
	}
}

func TestOTLPCounters_ConcurrentSafe(t *testing.T) {
	resetOTLPCountersForTest()

	const goroutines = 50
	const incsPerG = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incsPerG; j++ {
				IncrementOTLPMetricsPushed(1)
				IncrementOTLPLogsPushed()
				IncrementOTLPExportErrors()
			}
		}()
	}
	wg.Wait()

	expected := uint64(goroutines * incsPerG)
	for _, c := range []struct {
		name string
		got  uint64
	}{
		{"metrics", GetOTLPMetricsPushedTotal()},
		{"logs", GetOTLPLogsPushedTotal()},
		{"errors", GetOTLPExportErrorsTotal()},
	} {
		if c.got != expected {
			t.Errorf("%s counter = %d, want %d (lost updates under contention)", c.name, c.got, expected)
		}
	}
}

func TestLogChannelFillRatio_EmptyAndFilled(t *testing.T) {
	resetLogChannelForTest()
	// No subscribers → 0.
	if r := LogChannelFillRatio(); r != 0 {
		t.Errorf("empty fill ratio = %v, want 0", r)
	}

	ch := SubscribeLogs(4)
	defer UnsubscribeLogs(ch)
	if r := LogChannelFillRatio(); r != 0 {
		t.Errorf("fresh subscription fill ratio = %v, want 0", r)
	}

	// Send 2 records, don't drain → fill = 2/4 = 0.5.
	PublishLog(LogRecord{Body: "a"})
	PublishLog(LogRecord{Body: "b"})
	if r := LogChannelFillRatio(); r != 0.5 {
		t.Errorf("fill ratio after 2 of 4 = %v, want 0.5", r)
	}
}
