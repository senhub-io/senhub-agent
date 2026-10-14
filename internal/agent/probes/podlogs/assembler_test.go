package podlogs

import (
	"testing"
	"time"
)

func TestAssemblerInterleavedStreams(t *testing.T) {
	t1 := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	t2 := t1.Add(time.Second)
	t3 := t2.Add(time.Second)
	t4 := t3.Add(time.Second)

	a := Assembler{}

	if got, ok := a.Add(Entry{Time: t1, Stream: "stdout", Partial: true, Message: "hello"}); ok {
		t.Fatalf("partial stdout chunk returned complete entry: %+v", got)
	}
	if got, ok := a.Add(Entry{Time: t2, Stream: "stderr", Partial: true, Message: "bad"}); ok {
		t.Fatalf("partial stderr chunk returned complete entry: %+v", got)
	}

	got, ok := a.Add(Entry{Time: t3, Stream: "stdout", Message: " world"})
	if !ok {
		t.Fatal("stdout F did not close the pending run")
	}
	checkEntry(t, got, Entry{Time: t1, Stream: "stdout", Message: "hello world"})

	got, ok = a.Add(Entry{Time: t4, Stream: "stderr", Message: "bad"})
	if !ok {
		t.Fatal("stderr F did not close the pending run")
	}
	checkEntry(t, got, Entry{Time: t2, Stream: "stderr", Message: "badbad"})
}

func TestAssemblerTruncation(t *testing.T) {
	t1 := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	a := Assembler{MaxBytes: 6}

	if got, ok := a.Add(Entry{Time: t1, Stream: "stdout", Partial: true, Message: "abc"}); ok {
		t.Fatalf("first partial returned complete entry: %+v", got)
	}
	if got, ok := a.Add(Entry{Time: t1.Add(time.Second), Stream: "stdout", Partial: true, Message: "def"}); ok {
		t.Fatalf("second partial returned complete entry: %+v", got)
	}

	got, ok := a.Add(Entry{Time: t1.Add(2 * time.Second), Stream: "stdout", Message: "ghi"})
	if !ok {
		t.Fatal("F did not close the pending run")
	}
	checkEntry(t, got, Entry{Time: t1, Stream: "stdout", Message: "abcdef", Truncated: true})
}

func TestAssemblerTruncatesSingleFullEntry(t *testing.T) {
	t1 := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	a := Assembler{MaxBytes: 4}

	got, ok := a.Add(Entry{Time: t1, Stream: "stdout", Message: "abcdef"})
	if !ok {
		t.Fatal("full entry should be returned")
	}
	checkEntry(t, got, Entry{Time: t1, Stream: "stdout", Message: "abcd", Truncated: true})
}

func TestAssemblerFlush(t *testing.T) {
	t1 := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	t2 := t1.Add(time.Second)

	a := Assembler{MaxBytes: 100}
	_, _ = a.Add(Entry{Time: t1, Stream: "stdout", Partial: true, Message: "out-partial"})
	_, _ = a.Add(Entry{Time: t2, Stream: "stderr", Partial: true, Message: "err-partial"})

	got := a.Flush()
	want := []Entry{
		{Time: t2, Stream: "stderr", Partial: true, Message: "err-partial"},
		{Time: t1, Stream: "stdout", Partial: true, Message: "out-partial"},
	}

	if len(got) != len(want) {
		t.Fatalf("len(Flush()) = %d, want %d", len(got), len(want))
	}
	for i := range got {
		checkEntry(t, got[i], want[i])
	}
}

func TestAssemblerFlushEmpty(t *testing.T) {
	a := Assembler{}
	if got := a.Flush(); got != nil {
		t.Fatalf("Flush() = %+v, want nil", got)
	}
}

func checkEntry(t *testing.T, got, want Entry) {
	t.Helper()
	if !got.Time.Equal(want.Time) {
		t.Fatalf("Time = %v, want %v", got.Time, want.Time)
	}
	if got.Stream != want.Stream || got.Partial != want.Partial || got.Message != want.Message || got.Truncated != want.Truncated {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
