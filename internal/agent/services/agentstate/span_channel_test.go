package agentstate

import (
	"sync"
	"testing"
	"time"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func spanBatch(name string) []*tracepb.ResourceSpans {
	return []*tracepb.ResourceSpans{{
		ScopeSpans: []*tracepb.ScopeSpans{{
			Spans: []*tracepb.Span{{Name: name}},
		}},
	}}
}

func TestPublishSpans_DeliversToSubscriber(t *testing.T) {
	resetSpanChannelForTest()
	ch := SubscribeSpans(8)
	defer UnsubscribeSpans(ch)

	PublishSpans(spanBatch("hello"))

	select {
	case got := <-ch:
		if len(got) != 1 || got[0].GetScopeSpans()[0].GetSpans()[0].GetName() != "hello" {
			t.Errorf("delivered batch = %+v, want one span named hello", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no batch delivered")
	}
}

func TestPublishSpans_FanOutToMultipleSubscribers(t *testing.T) {
	resetSpanChannelForTest()
	a := SubscribeSpans(8)
	b := SubscribeSpans(8)
	defer UnsubscribeSpans(a)
	defer UnsubscribeSpans(b)

	PublishSpans(spanBatch("x"))

	for _, ch := range []<-chan []*tracepb.ResourceSpans{a, b} {
		select {
		case got := <-ch:
			if got[0].GetScopeSpans()[0].GetSpans()[0].GetName() != "x" {
				t.Errorf("span name = %q, want x", got[0].GetScopeSpans()[0].GetSpans()[0].GetName())
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber did not receive")
		}
	}
}

func TestPublishSpans_DropsOldestOnFull(t *testing.T) {
	resetSpanChannelForTest()
	ch := SubscribeSpans(2)
	defer UnsubscribeSpans(ch)

	// Fill the buffer + overflow by 2.
	for i := 0; i < 4; i++ {
		PublishSpans(spanBatch("x"))
	}

	if GetDroppedSpanBatchesTotal() == 0 {
		t.Errorf("expected drop count > 0")
	}
}

func TestPublishSpans_EmptyBatchIsNoOp(t *testing.T) {
	resetSpanChannelForTest()
	ch := SubscribeSpans(2)
	defer UnsubscribeSpans(ch)

	PublishSpans(nil)

	select {
	case got := <-ch:
		t.Errorf("empty publish delivered %+v", got)
	default:
	}
}

func TestSubscribeSpans_DefaultsBuffer(t *testing.T) {
	resetSpanChannelForTest()
	// buf<=0 should default to a non-zero buffer; the test just
	// confirms a Subscribe call with 0 doesn't panic and delivers.
	ch := SubscribeSpans(0)
	defer UnsubscribeSpans(ch)
	PublishSpans(spanBatch("ok"))
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("default-buffer subscribe didn't deliver")
	}
}

func TestUnsubscribeSpans_StopsDelivery(t *testing.T) {
	// Same contract as logs after #262: Unsubscribe removes the
	// subscription but does NOT close the channel (a consumer-side close
	// races PublishSpans into a send-on-closed-channel panic).
	resetSpanChannelForTest()
	ch := SubscribeSpans(2)
	UnsubscribeSpans(ch)

	PublishSpans(spanBatch("after-unsubscribe"))

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("received a batch on an unsubscribed channel")
		} else {
			t.Error("channel was closed — #262 regression (consumer-side close reintroduced)")
		}
	default:
		// Nothing delivered, channel open: the contract.
	}
}

func TestSpanSubscriberCount(t *testing.T) {
	resetSpanChannelForTest()
	if got := SpanSubscriberCount(); got != 0 {
		t.Fatalf("initial count = %d, want 0", got)
	}
	a := SubscribeSpans(2)
	b := SubscribeSpans(2)
	if got := SpanSubscriberCount(); got != 2 {
		t.Errorf("count = %d, want 2", got)
	}
	UnsubscribeSpans(a)
	if got := SpanSubscriberCount(); got != 1 {
		t.Errorf("count after unsubscribe = %d, want 1", got)
	}
	UnsubscribeSpans(b)
	if got := SpanSubscriberCount(); got != 0 {
		t.Errorf("count after both unsubscribed = %d, want 0", got)
	}
}

func TestPublishSpansTo_BroadcastReachesNamedSubscriber(t *testing.T) {
	resetSpanChannelForTest()
	ch := SubscribeSpansFor("otlp", 8)
	defer UnsubscribeSpans(ch)

	// PublishSpans / empty targets = broadcast; a named subscriber must
	// still receive it (pre-#294 behavior preserved).
	PublishSpans(spanBatch("broadcast"))

	select {
	case got := <-ch:
		if got[0].GetScopeSpans()[0].GetSpans()[0].GetName() != "broadcast" {
			t.Errorf("span name = %q, want broadcast", got[0].GetScopeSpans()[0].GetSpans()[0].GetName())
		}
	case <-time.After(time.Second):
		t.Fatal("named subscriber did not receive broadcast batch")
	}
}

func TestPublishSpansTo_RoutesOnlyToNamedStrategy(t *testing.T) {
	resetSpanChannelForTest()
	otlp := SubscribeSpansFor("otlp", 8)
	other := SubscribeSpansFor("otlp-toise", 8)
	defer UnsubscribeSpans(otlp)
	defer UnsubscribeSpans(other)

	PublishSpansTo(spanBatch("for-otlp"), []string{"otlp"})

	select {
	case got := <-otlp:
		if got[0].GetScopeSpans()[0].GetSpans()[0].GetName() != "for-otlp" {
			t.Errorf("otlp span name = %q", got[0].GetScopeSpans()[0].GetSpans()[0].GetName())
		}
	case <-time.After(time.Second):
		t.Fatal("otlp subscriber did not receive targeted batch")
	}

	select {
	case got := <-other:
		t.Errorf("non-targeted strategy received a batch targeted at otlp: %+v", got)
	case <-time.After(50 * time.Millisecond):
		// Correct: routing kept the batch away from the other strategy.
	}
}

func TestPublishSpansTo_CatchAllReceivesTargetedBatch(t *testing.T) {
	resetSpanChannelForTest()
	// SubscribeSpans (no strategy) is a catch-all tap; it sees batches
	// regardless of their routing target.
	ch := SubscribeSpans(8)
	defer UnsubscribeSpans(ch)

	PublishSpansTo(spanBatch("for-toise"), []string{"otlp-toise"})

	select {
	case got := <-ch:
		if got[0].GetScopeSpans()[0].GetSpans()[0].GetName() != "for-toise" {
			t.Errorf("span name = %q, want for-toise", got[0].GetScopeSpans()[0].GetSpans()[0].GetName())
		}
	case <-time.After(time.Second):
		t.Fatal("catch-all subscriber did not receive targeted batch")
	}
}

func TestPublishSpansTo_NonTargetedSubscriberNotDropped(t *testing.T) {
	resetSpanChannelForTest()
	// A named subscriber a batch is NOT routed to must be skipped
	// entirely: no delivery attempt, so no spurious drop accounting even
	// when its buffer is tiny and we publish many batches it never wants.
	SubscribeSpansFor("otlp-toise", 1)

	for i := 0; i < 10; i++ {
		PublishSpansTo(spanBatch("x"), []string{"otlp"})
	}

	if got := GetDroppedSpanBatchesTotal(); got != 0 {
		t.Errorf("dropped=%d, want 0 (non-targeted subscriber must not be charged drops)", got)
	}
}

func TestPublishSpans_ConcurrentProducers(t *testing.T) {
	resetSpanChannelForTest()
	ch := SubscribeSpans(1024)
	defer UnsubscribeSpans(ch)

	const producers = 10
	const perProducer = 100

	var wg sync.WaitGroup
	wg.Add(producers)
	for i := 0; i < producers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perProducer; j++ {
				PublishSpans(spanBatch("msg"))
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
		t.Fatal("no batches received")
	}
	if received+int(GetDroppedSpanBatchesTotal()) < producers*perProducer {
		t.Errorf("received=%d dropped=%d, want >=%d total",
			received, GetDroppedSpanBatchesTotal(), producers*perProducer)
	}
}
