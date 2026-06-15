package sanitize

import (
	"math"
	"testing"
	"time"
)

func TestDuration_NilAndZero(t *testing.T) {
	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)

	if _, ok := Duration(nil, now); ok {
		t.Errorf("nil pointer should return ok=false")
	}

	zero := time.Time{}
	if _, ok := Duration(&zero, now); ok {
		t.Errorf("zero time should return ok=false")
	}
}

func TestDuration_Normal(t *testing.T) {
	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	past := now.Add(-90 * time.Second)

	sec, ok := Duration(&past, now)
	if !ok {
		t.Fatalf("normal duration should be ok")
	}
	if sec != 90 {
		t.Errorf("expected 90, got %v", sec)
	}
}

func TestDuration_FutureClampsToZero(t *testing.T) {
	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	future := now.Add(5 * time.Minute)

	sec, ok := Duration(&future, now)
	if !ok {
		t.Fatalf("future time should still be ok (clamped, not rejected)")
	}
	if sec != 0 {
		t.Errorf("future should clamp to 0, got %v", sec)
	}
}

func TestCountInt32_Bounds(t *testing.T) {
	cases := []struct {
		in      int64
		wantOk  bool
		wantVal float64
	}{
		{0, true, 0},
		{42, true, 42},
		{math.MaxInt32, true, MaxInt32},
		{int64(math.MaxInt32) + 1, false, MaxInt32}, // clamped
		{1 << 40, false, MaxInt32},                  // wildly over
		{-1, false, 0},                              // negative
	}
	for _, c := range cases {
		got, ok := CountInt32(c.in)
		if ok != c.wantOk {
			t.Errorf("CountInt32(%d) ok=%v, want %v", c.in, ok, c.wantOk)
		}
		if got != c.wantVal {
			t.Errorf("CountInt32(%d) value=%v, want %v", c.in, got, c.wantVal)
		}
	}
}

func TestBytes_BeyondInt32(t *testing.T) {
	// Bytes counters routinely exceed 2 GB. Verify that values above
	// MaxInt32 come back ok=true (precision loss is acceptable; cap
	// is unacceptable).
	cases := []struct {
		in     int64
		wantOk bool
	}{
		{0, true},
		{1024, true},
		{int64(math.MaxInt32), true},     // exactly 2 GB - 1 byte
		{int64(math.MaxInt32) + 1, true}, // 2 GB, was the old cap
		{int64(1) << 40, true},           // 1 TiB
		{int64(1) << 50, true},           // 1 PiB
		{-1, false},                      // negative bytes makes no sense
	}
	for _, c := range cases {
		got, ok := Bytes(c.in)
		if ok != c.wantOk {
			t.Errorf("Bytes(%d) ok=%v, want %v (value=%v)", c.in, ok, c.wantOk, got)
		}
		if c.wantOk && got < 0 {
			t.Errorf("Bytes(%d) returned negative %v — should not happen for non-negative input", c.in, got)
		}
	}

	// Spot-check: 2 GB round-trips through float64 exactly (2^31 fits
	// within the 53-bit float64 mantissa, so the conversion is lossless).
	twoGB := int64(2) * 1024 * 1024 * 1024
	v, _ := Bytes(twoGB)
	round := int64(v)
	delta := round - twoGB
	if delta != 0 {
		t.Errorf("Bytes(2 GiB) round-trip delta = %d bytes, want 0 (exact)", delta)
	}
}

func TestEnumValue_HitMiss(t *testing.T) {
	mapping := map[string]float64{
		"None":   0,
		"Source": 1,
		"Target": 4,
	}

	// Exact hit
	if v, ok := EnumValue("Source", mapping); !ok || v != 1 {
		t.Errorf("Source: got (%v,%v), want (1,true)", v, ok)
	}

	// Case-insensitive hit
	if v, ok := EnumValue("source", mapping); !ok || v != 1 {
		t.Errorf("source (lowercase): got (%v,%v), want (1,true)", v, ok)
	}

	// Miss — must return ok=false, NOT silently 0
	if v, ok := EnumValue("UNKNOWN_NEW_VALUE", mapping); ok {
		t.Errorf("unknown value should be ok=false, got (%v,%v)", v, ok)
	}

	// Empty string — also a miss
	if _, ok := EnumValue("", mapping); ok {
		t.Errorf("empty string should be ok=false")
	}
}

func TestIsFinite(t *testing.T) {
	if !IsFinite(0) || !IsFinite(42) || !IsFinite(-1.5) {
		t.Errorf("finite values should pass")
	}
	if IsFinite(math.NaN()) {
		t.Errorf("NaN should fail")
	}
	if IsFinite(math.Inf(1)) || IsFinite(math.Inf(-1)) {
		t.Errorf("Inf should fail")
	}
}
