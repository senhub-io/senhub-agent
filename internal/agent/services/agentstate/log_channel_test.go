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

func TestUnsubscribeLogs_StopsDelivery(t *testing.T) {
	// Contract changed by #262: Unsubscribe no longer closes the
	// channel (a consumer-side close raced PublishLog into a
	// send-on-closed-channel panic). It removes the subscription —
	// records published afterwards are not delivered; consumers exit
	// via their own context, and the channel is garbage-collected.
	resetLogChannelForTest()
	ch := SubscribeLogs(2)
	UnsubscribeLogs(ch)

	PublishLog(LogRecord{Body: "after-unsubscribe"})

	select {
	case rec, ok := <-ch:
		if ok {
			t.Errorf("received %q on an unsubscribed channel", rec.Body)
		} else {
			t.Error("channel was closed — #262 regression (consumer-side close reintroduced)")
		}
	default:
		// Nothing delivered, channel open: the new contract.
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

func TestRecordRoutesTo(t *testing.T) {
	cases := []struct {
		name     string
		targets  []string
		strategy string
		want     bool
	}{
		{"catch-all takes everything", []string{"otlp"}, "", true},
		{"catch-all takes broadcast", nil, "", true},
		{"broadcast reaches named", nil, "otlp", true},
		{"empty-slice broadcast reaches named", []string{}, "otlp", true},
		{"targeted reaches its strategy", []string{"otlp"}, "otlp", true},
		{"targeted skips other strategy", []string{"event"}, "otlp", false},
		{"multi-target reaches one member", []string{"event", "otlp"}, "otlp", true},
		{"multi-target skips non-member", []string{"event", "senhub"}, "otlp", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := recordRoutesTo(tc.targets, tc.strategy); got != tc.want {
				t.Errorf("recordRoutesTo(%v, %q)=%v, want %v", tc.targets, tc.strategy, got, tc.want)
			}
		})
	}
}

func TestPublishLog_BroadcastReachesNamedSubscriber(t *testing.T) {
	resetLogChannelForTest()
	ch := SubscribeLogsFor("otlp", 8)
	defer UnsubscribeLogs(ch)

	// Empty TargetStrategies = broadcast; a named subscriber must still
	// receive it (pre-#294 behavior preserved).
	PublishLog(LogRecord{Body: "broadcast"})

	select {
	case got := <-ch:
		if got.Body != "broadcast" {
			t.Errorf("body=%q", got.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("named subscriber did not receive broadcast record")
	}
}

func TestPublishLog_TargetedRoutesOnlyToNamedStrategy(t *testing.T) {
	resetLogChannelForTest()
	otlp := SubscribeLogsFor("otlp", 8)
	event := SubscribeLogsFor("event", 8)
	defer UnsubscribeLogs(otlp)
	defer UnsubscribeLogs(event)

	PublishLog(LogRecord{Body: "for-otlp", TargetStrategies: []string{"otlp"}})

	select {
	case got := <-otlp:
		if got.Body != "for-otlp" {
			t.Errorf("otlp body=%q", got.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("otlp subscriber did not receive targeted record")
	}

	select {
	case got := <-event:
		t.Errorf("event subscriber received a record targeted at otlp: %q", got.Body)
	case <-time.After(50 * time.Millisecond):
		// Correct: routing kept the record away from the event strategy.
	}
}

func TestPublishLog_CatchAllReceivesTargetedRecord(t *testing.T) {
	resetLogChannelForTest()
	// SubscribeLogs (no strategy) is a catch-all tap; it must see records
	// regardless of their routing target.
	ch := SubscribeLogs(8)
	defer UnsubscribeLogs(ch)

	PublishLog(LogRecord{Body: "for-event", TargetStrategies: []string{"event"}})

	select {
	case got := <-ch:
		if got.Body != "for-event" {
			t.Errorf("body=%q", got.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("catch-all subscriber did not receive targeted record")
	}
}

func TestPublishLog_NonTargetedSubscriberNotDropped(t *testing.T) {
	resetLogChannelForTest()
	// A named subscriber that a record is NOT routed to must be skipped
	// entirely: no delivery attempt, so no spurious drop accounting even
	// when its buffer is tiny and we publish many records it never wants.
	SubscribeLogsFor("event", 1)

	for i := 0; i < 10; i++ {
		PublishLog(LogRecord{Body: "x", TargetStrategies: []string{"otlp"}})
	}

	if got := GetDroppedLogRecordsTotal(); got != 0 {
		t.Errorf("dropped=%d, want 0 (non-targeted subscriber must not be charged drops)", got)
	}
}

func TestSyslogPriorityToSeverity_Mapping(t *testing.T) {
	// Smoke-test the standard mapping. Out-of-range returns Unspecified.
	cases := map[int]LogSeverity{
		0:  24,               // FATAL4
		3:  LogSeverityError, // ERROR
		4:  LogSeverityWarn,  // WARN
		6:  LogSeverityInfo,  // INFO
		7:  LogSeverityDebug, // DEBUG
		99: LogSeverityUnspecified,
		-1: LogSeverityUnspecified,
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
