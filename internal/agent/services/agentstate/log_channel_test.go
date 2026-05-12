package agentstate

import (
	"sync"
	"testing"
	"time"
)

func TestPublishLog_DeliversToSubscriber(t *testing.T) {
	resetLogChannelForTest()
	ch := SubscribeLogs(8)
	defer UnsubscribeLogs(ch)

	want := LogRecord{
		Timestamp: time.Unix(1700, 0),
		Severity:  LogSeverityInfo,
		Body:      "hello",
	}
	PublishLog(want)

	select {
	case got := <-ch:
		if got.Body != "hello" {
			t.Errorf("body=%q", got.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("no record delivered")
	}
}

func TestPublishLog_FanOutToMultipleSubscribers(t *testing.T) {
	resetLogChannelForTest()
	a := SubscribeLogs(8)
	b := SubscribeLogs(8)
	defer UnsubscribeLogs(a)
	defer UnsubscribeLogs(b)

	PublishLog(LogRecord{Body: "x"})

	for _, ch := range []<-chan LogRecord{a, b} {
		select {
		case got := <-ch:
			if got.Body != "x" {
				t.Errorf("body=%q", got.Body)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber did not receive")
		}
	}
}

func TestPublishLog_DropsOldestOnFull(t *testing.T) {
	resetLogChannelForTest()
	ch := SubscribeLogs(2)
	defer UnsubscribeLogs(ch)

	// Fill the buffer + overflow by 2.
	for i := 0; i < 4; i++ {
		PublishLog(LogRecord{Body: "x"})
	}

	if GetDroppedLogRecordsTotal() == 0 {
		t.Errorf("expected drop count > 0")
	}
}

func TestSubscribeLogs_DefaultsBuffer(t *testing.T) {
	resetLogChannelForTest()
	// buf<=0 should default to a non-zero buffer; the test just
	// confirms a Subscribe call with 0 doesn't panic and delivers.
	ch := SubscribeLogs(0)
	defer UnsubscribeLogs(ch)
	PublishLog(LogRecord{Body: "ok"})
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("default-buffer subscribe didn't deliver")
	}
}

func TestUnsubscribeLogs_ClosesChannel(t *testing.T) {
	resetLogChannelForTest()
	ch := SubscribeLogs(2)
	UnsubscribeLogs(ch)

	// After unsubscribe the channel must be closed (range exits).
	select {
	case _, ok := <-ch:
		if ok {
			t.Errorf("channel still open after Unsubscribe")
		}
	case <-time.After(time.Second):
		t.Fatal("channel did not close")
	}
}

func TestPublishLog_ConcurrentProducers(t *testing.T) {
	resetLogChannelForTest()
	ch := SubscribeLogs(1024)
	defer UnsubscribeLogs(ch)

	const producers = 10
	const perProducer = 100

	var wg sync.WaitGroup
	wg.Add(producers)
	for i := 0; i < producers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perProducer; j++ {
				PublishLog(LogRecord{Body: "msg"})
			}
		}()
	}
	wg.Wait()

	// Drain everything that made it to the channel; the test passes
	// if we don't deadlock and observed a reasonable count.
	deadline := time.After(2 * time.Second)
	received := 0
loop:
	for {
		select {
		case <-ch:
			received++
		case <-deadline:
			break loop
		default:
			break loop
		}
	}
	if received == 0 {
		t.Fatal("no records received")
	}
	// Every producer should have made at least one record visible
	// (with a 1024-deep buffer no drops should happen at this scale).
	if received+int(GetDroppedLogRecordsTotal()) < producers*perProducer {
		t.Errorf("received=%d dropped=%d, want >=%d total",
			received, GetDroppedLogRecordsTotal(), producers*perProducer)
	}
}

func TestSyslogPriorityToSeverity_Mapping(t *testing.T) {
	// Smoke-test the standard mapping. Out-of-range returns Unspecified.
	cases := map[int]LogSeverity{
		0:   24,                   // FATAL4
		3:   LogSeverityError,     // ERROR
		4:   LogSeverityWarn,      // WARN
		6:   LogSeverityInfo,      // INFO
		7:   LogSeverityDebug,     // DEBUG
		99:  LogSeverityUnspecified,
		-1:  LogSeverityUnspecified,
	}
	for pri, want := range cases {
		if got := SyslogPriorityToSeverity(pri); got != want {
			t.Errorf("pri=%d → severity=%d, want %d", pri, got, want)
		}
	}
}

func TestSyslogPriorityToText_Mapping(t *testing.T) {
	if got := SyslogPriorityToText(3); got != "ERROR" {
		t.Errorf("pri=3 text=%q, want ERROR", got)
	}
	if got := SyslogPriorityToText(99); got != "" {
		t.Errorf("pri=99 text=%q, want empty", got)
	}
}
