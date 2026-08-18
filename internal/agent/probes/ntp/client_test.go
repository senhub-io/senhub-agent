package ntp

import (
	"encoding/hex"
	"errors"
	"math"
	"testing"
	"time"
)

// Two verbatim exchanges with time.google.com, captured on 2026-08-14 together
// with the exact instants the request left and the reply arrived. Nothing here
// is hand-written: the bytes are what came off the wire.
//
// The chrony probe shipped broken because its fixture was an invented line that
// no tool produces, so the parser was only ever proved to match the invention.
// A protocol decoder is worth exactly as much as the realism of what it is
// tested against, so these stay verbatim.
//
// The expected offset is not derived from this decoder either. `sntp` was run
// against the same server seconds later and independently reported
//
//	+0.220304 +/- 0.023745 time.google.com
//
// so the number below comes from a tool that shares no code with the one under
// test. The machine really was ~220 ms behind.
const (
	capture1Request = "ee29845f5b97bb73"
	capture1Reply   = "240100ec0000000000000005474f4f47" +
		"ee29845f97b96014ee29845f5b97bb73ee29845f97b96015ee29845f97b96017"
	capture1T1 = 1786709471357784000
	capture1T4 = 1786709471385673000

	capture2Reply = "240100ec0000000000000004474f4f47" +
		"ee29847bd24b55acee29847b95231c64ee29847bd24b55adee29847bd24b55af"
	capture2T1 = 1786709499582567000
	capture2T4 = 1786709499612140000

	// sntpReferenceOffset is the independently measured offset, in seconds.
	sntpReferenceOffset = 0.220304
)

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	return b
}

func decodeCapture(t *testing.T, reply string, t1Nano, t4Nano int64) (sample, error) {
	t.Helper()
	return decode(mustDecodeHex(t, reply), time.Unix(0, t1Nano), time.Unix(0, t4Nano))
}

// TestToNTPMatchesTheCapturedRequest pins the timestamp encoder against the
// bytes we actually put on the wire during the capture. If this drifts, the
// originate check in decode() starts rejecting every genuine reply.
func TestToNTPMatchesTheCapturedRequest(t *testing.T) {
	got := toNTP(time.Unix(0, capture1T1))
	want := mustDecodeHex(t, capture1Request)
	wantVal := uint64(0)
	for _, b := range want {
		wantVal = wantVal<<8 | uint64(b)
	}
	if got != wantVal {
		t.Errorf("toNTP = %016x, want %016x (the transmit timestamp of the captured request)", got, wantVal)
	}
}

func TestDecodeRealServerReply(t *testing.T) {
	s, err := decodeCapture(t, capture1Reply, capture1T1, capture1T4)
	if err != nil {
		t.Fatalf("a verbatim reply from a public stratum-1 server must decode: %v", err)
	}

	if s.stratum != 1 {
		t.Errorf("stratum = %d, want 1", s.stratum)
	}
	if s.leap != 0 {
		t.Errorf("leap = %d, want 0 (normal)", s.leap)
	}

	offsetErr := math.Abs(s.offset.Seconds() - sntpReferenceOffset)
	if offsetErr > 0.005 {
		t.Errorf("offset = %v, which is %.1f ms away from the %.6f s that sntp measured independently",
			s.offset, offsetErr*1000, sntpReferenceOffset)
	}

	// The exchange took t4-t1 = 27.9 ms end to end, and the server spent
	// essentially no time between receiving and replying, so the round trip
	// must land just under that.
	if s.roundTrip < 20*time.Millisecond || s.roundTrip > 30*time.Millisecond {
		t.Errorf("roundTrip = %v, want the ~27.9 ms the capture actually took", s.roundTrip)
	}

	// Root delay 0 and root dispersion 5/65536 s are what the packet carries: a
	// stratum-1 server is its own reference, so it is zero hops from it.
	if s.rootDelay != 0 {
		t.Errorf("rootDelay = %v, want 0 for a stratum-1 server", s.rootDelay)
	}
	if want := shortToDuration(5); s.rootDispersion != want {
		t.Errorf("rootDispersion = %v, want %v", s.rootDispersion, want)
	}
}

// TestBestPrefersTheLeastDelayedSample uses the two real captures rather than
// constructed ones, because the heuristic is an empirical claim and deserves
// empirical evidence.
//
// Capture 1 travelled 27.9 ms and lands 0.6 ms from the independent reference;
// capture 2 travelled 29.6 ms and lands 3.8 ms away. The less delayed exchange
// really was the more accurate one — which is the whole reason best() exists.
func TestBestPrefersTheLeastDelayedSample(t *testing.T) {
	s1, err := decodeCapture(t, capture1Reply, capture1T1, capture1T4)
	if err != nil {
		t.Fatalf("capture 1: %v", err)
	}
	s2, err := decodeCapture(t, capture2Reply, capture2T1, capture2T4)
	if err != nil {
		t.Fatalf("capture 2: %v", err)
	}
	if !(s1.roundTrip < s2.roundTrip) {
		t.Fatalf("fixture assumption broken: capture 1 (%v) should be the least delayed of the two (%v)", s1.roundTrip, s2.roundTrip)
	}

	if got := best([]sample{s2, s1}); got.roundTrip != s1.roundTrip {
		t.Errorf("best picked the %v sample, want the %v one", got.roundTrip, s1.roundTrip)
	}

	err1 := math.Abs(s1.offset.Seconds() - sntpReferenceOffset)
	err2 := math.Abs(s2.offset.Seconds() - sntpReferenceOffset)
	if err1 >= err2 {
		t.Errorf("the least delayed sample was not the more accurate one (%.4f s vs %.4f s error) — "+
			"the fixtures no longer support the heuristic best() implements", err1, err2)
	}
}

// TestDecodeRejectsAReplyThatDoesNotEchoUs is the anti-spoofing and
// anti-stale-datagram check. Without it a packet from an earlier cycle, or one
// injected off-path, would be read as a fresh measurement.
func TestDecodeRejectsAReplyThatDoesNotEchoUs(t *testing.T) {
	reply := mustDecodeHex(t, capture1Reply)
	reply[31] ^= 0x01 // one bit of the originate timestamp

	if _, err := decode(reply, time.Unix(0, capture1T1), time.Unix(0, capture1T4)); err == nil {
		t.Fatal("a reply whose originate timestamp is not ours must be rejected")
	}

	// And the genuine reply must be rejected when paired with a different
	// request instant, which is the stale-datagram case.
	if _, err := decodeCapture(t, capture1Reply, capture1T1+1, capture1T4); err == nil {
		t.Fatal("a reply matched against a different request instant must be rejected")
	}
}

func TestDecodeRejectsKissOfDeath(t *testing.T) {
	for _, code := range []string{"RATE", "DENY", "RSTR"} {
		reply := mustDecodeHex(t, capture1Reply)
		reply[1] = 0 // stratum 0 marks a kiss-o'-death
		copy(reply[12:16], code)

		_, err := decode(reply, time.Unix(0, capture1T1), time.Unix(0, capture1T4))
		if !errors.Is(err, errRefused) {
			t.Errorf("%s: err = %v, want errRefused", code, err)
		}
		if err != nil && !contains(err.Error(), code) {
			t.Errorf("%s: the reason code must reach the operator, got %q", code, err)
		}
	}
}

func TestDecodeRejectsAnUnsynchronisedServer(t *testing.T) {
	reply := mustDecodeHex(t, capture1Reply)
	reply[0] = leapUnsynchronised<<6 | versionNTPv4<<3 | modeServer

	_, err := decode(reply, time.Unix(0, capture1T1), time.Unix(0, capture1T4))
	if !errors.Is(err, errUnsynchronised) {
		t.Errorf("err = %v, want errUnsynchronised — a server that admits its own clock is adrift "+
			"is not a reference, and reading it would report a fabricated offset", err)
	}
}

func TestDecodeRejectsMalformedReplies(t *testing.T) {
	cases := []struct {
		name  string
		mutta func([]byte)
	}{
		{"not a server mode", func(b []byte) { b[0] = versionNTPv4<<3 | modeClient }},
		{"stratum above the valid range", func(b []byte) { b[1] = 16 }},
		{"zero transmit timestamp", func(b []byte) { copy(b[40:48], make([]byte, 8)) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply := mustDecodeHex(t, capture1Reply)
			tc.mutta(reply)
			if _, err := decode(reply, time.Unix(0, capture1T1), time.Unix(0, capture1T4)); err == nil {
				t.Errorf("%s must be rejected", tc.name)
			}
		})
	}
}

func TestNTPTimestampRoundTrip(t *testing.T) {
	for _, in := range []time.Time{
		time.Unix(0, capture1T1),
		time.Unix(1000000000, 0),
		time.Unix(1786709471, 999999999),
	} {
		out := fromNTP(toNTP(in))
		// The wire format carries 2^-32 s of resolution, so a round trip is
		// exact to well under a microsecond but not to the nanosecond.
		if d := out.Sub(in); d > time.Microsecond || d < -time.Microsecond {
			t.Errorf("round trip of %v drifted by %v", in, d)
		}
	}
}

// TestFromNTPHandlesTheEraRollover covers a timestamp whose high seconds bit is
// clear. Read as era 0 it would land in 1900 and produce an offset of over a
// century; era 1 puts it just past 2036.
func TestFromNTPHandlesTheEraRollover(t *testing.T) {
	era1 := uint64(0x00000001) << 32 // 1 second into era 1
	got := fromNTP(era1)
	if got.Year() < 2036 || got.Year() > 2037 {
		t.Errorf("era-1 timestamp decoded to %v, want a date just past the 2036 rollover", got)
	}
	if fromNTP(0).IsZero() != true {
		t.Error("a zero NTP timestamp means unset and must decode to the zero time")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
