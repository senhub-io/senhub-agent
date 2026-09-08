package agentstate

import "testing"

func TestEventsNewestFirstAndBounded(t *testing.T) {
	ResetEventsForTest()
	defer ResetEventsForTest()
	if got := GetEvents(); len(got) != 0 {
		t.Fatalf("empty buffer must list nothing, got %d", len(got))
	}
	RecordEvent(EventInfo, EventKindProbe, "a", "first")
	RecordEvent(EventWarn, EventKindProbe, "b", "second")
	got := GetEvents()
	if len(got) != 2 || got[0].Subject != "b" || got[1].Subject != "a" {
		t.Fatalf("want newest first [b a], got %+v", got)
	}
	for i := 0; i < eventBufferSize+7; i++ {
		RecordEvent(EventInfo, EventKindConsole, "x", "n")
	}
	got = GetEvents()
	if len(got) != eventBufferSize {
		t.Fatalf("buffer must hold %d events, got %d", eventBufferSize, len(got))
	}
	for _, e := range got {
		if e.Subject != "x" {
			t.Fatalf("the oldest events must have been evicted, found %+v", e)
		}
	}
	if GetEvents()[0].Level != EventInfo {
		t.Error("an empty level must read as info")
	}
}
