package pdh

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

type fakeSampler struct {
	err   error
	calls int
}

func (f *fakeSampler) Collect() error { f.calls++; return f.err }

func TestPrimeSamples(t *testing.T) {
	hard := errors.New("boom")
	tests := []struct {
		name      string
		err       error
		wantErr   bool
		wantSleep bool
	}{
		{"ok", nil, false, true},
		{"no data is expected", fmt.Errorf("wrapped: %w", ErrNoData), false, true},
		{"hard error aborts before sleeping", hard, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeSampler{err: tc.err}
			var slept time.Duration
			err := primeSamples(f, func(d time.Duration) { slept += d })
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, hard) {
				t.Fatalf("error %v does not wrap the collect error", err)
			}
			if f.calls != 1 {
				t.Fatalf("Collect called %d times, want 1", f.calls)
			}
			if got := slept == PrimeInterval; got != tc.wantSleep {
				t.Fatalf("slept %v, wantSleep %v", slept, tc.wantSleep)
			}
		})
	}
}
