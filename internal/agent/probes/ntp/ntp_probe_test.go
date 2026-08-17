package ntp

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

func newTestProbe(t *testing.T, config map[string]interface{}) *NTPProbe {
	t.Helper()
	probe, err := NewNTPProbe(config, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatalf("NewNTPProbe: %v", err)
	}
	p := probe.(*NTPProbe)
	p.SetName("ntp-check")
	return p
}

func TestParseConfigRequiresServers(t *testing.T) {
	log := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})

	// No default server, on purpose: a default would point every agent that
	// enables this probe at somebody else's infrastructure.
	if _, err := NewNTPProbe(map[string]interface{}{}, log); err == nil {
		t.Fatal("a missing servers list must be a configuration error, not a silent default")
	}
	if _, err := NewNTPProbe(map[string]interface{}{"servers": []interface{}{}}, log); err == nil {
		t.Fatal("an empty servers list must be a configuration error")
	}
	if _, err := NewNTPProbe(map[string]interface{}{"servers": "ntp.example.org"}, log); err == nil {
		t.Fatal("a scalar servers value must be a configuration error")
	}
}

func TestParseConfigCapsSamples(t *testing.T) {
	log := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	_, err := NewNTPProbe(map[string]interface{}{
		"servers": []interface{}{"ntp.example.org"},
		"samples": maxSamples + 1,
	}, log)
	if err == nil {
		t.Fatal("samples above the cap must be refused: past a handful the estimate stops improving and the traffic starts looking like abuse")
	}
}

func TestParseConfigDefaults(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"servers": []interface{}{"ntp.example.org"}})
	if p.config.Samples != defaultSamples {
		t.Errorf("samples = %d, want %d", p.config.Samples, defaultSamples)
	}
	if p.GetInterval() != defaultInterval {
		t.Errorf("interval = %v, want %v — a query is traffic to somebody else's server", p.GetInterval(), defaultInterval)
	}
}

// value returns the datapoint with the given name and, when reason is
// non-empty, the given reason tag.
func value(t *testing.T, points []data_store.DataPoint, name, reason string) float64 {
	t.Helper()
	for _, p := range points {
		if p.Name != name {
			continue
		}
		if reason == "" {
			return p.Value
		}
		for _, tag := range p.Tags {
			if tag.Key == "reason" && tag.Value == reason {
				return p.Value
			}
		}
	}
	t.Fatalf("no datapoint %q (reason %q) among %d points", name, reason, len(points))
	return 0
}

// TestCollectStateIsOneHotPerOutcome is the answer to the ambiguity an operator
// reported on the chrony probe: up=0 alone never said whether the time source
// was missing, filtered, or refusing us.
func TestCollectStateIsOneHotPerOutcome(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantReason string
		wantUp     float64
	}{
		{"success", nil, reasonOK, 1},
		{"filtered udp 123", fmt.Errorf("%w: waiting: timeout", errUnreachable), reasonUnreachable, 0},
		{"kiss of death", fmt.Errorf("%w: RATE", errRefused), reasonRefused, 0},
		{"server adrift", errUnsynchronised, reasonUnsynchronised, 0},
		{"garbage on the wire", errors.New("response carries mode 7"), reasonInvalidResponse, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestProbe(t, map[string]interface{}{
				"servers": []interface{}{"ntp.example.org"},
				"samples": 1,
			})
			p.query = func(string, time.Duration) (sample, error) {
				return sample{offset: 12 * time.Millisecond, roundTrip: 4 * time.Millisecond, stratum: 2}, tc.err
			}

			points, err := p.Collect()
			if err != nil {
				t.Fatalf("a failing server is a measurement, never a collection error: %v", err)
			}

			if got := value(t, points, "senhub.ntp.up", ""); got != tc.wantUp {
				t.Errorf("up = %v, want %v", got, tc.wantUp)
			}
			for _, r := range allReasons {
				want := float64(0)
				if r == tc.wantReason {
					want = 1
				}
				if got := value(t, points, "senhub.ntp.state", r); got != want {
					t.Errorf("state[%s] = %v, want %v", r, got, want)
				}
			}
		})
	}
}

func TestCollectEmitsMeasurementsOnlyOnSuccess(t *testing.T) {
	measured := []string{
		"ntp.time.offset", "ntp.round_trip.delay", "ntp.stratum",
		"ntp.root.delay", "ntp.root.dispersion", "ntp.leap_status",
	}

	p := newTestProbe(t, map[string]interface{}{"servers": []interface{}{"ntp.example.org"}, "samples": 1})
	p.query = func(string, time.Duration) (sample, error) {
		return sample{offset: -250 * time.Millisecond, roundTrip: 8 * time.Millisecond, stratum: 3}, nil
	}
	points, _ := p.Collect()
	if got := value(t, points, "ntp.time.offset", ""); got != -250 {
		t.Errorf("offset = %v ms, want -250 — the sign says the clock is behind and must survive the wire", got)
	}
	for _, name := range measured {
		value(t, points, name, "") // fatals if absent
	}

	// A failed exchange must publish no measurement at all: a zero offset is
	// indistinguishable from a perfectly synchronised clock.
	p.query = func(string, time.Duration) (sample, error) { return sample{}, errUnsynchronised }
	points, _ = p.Collect()
	for _, pt := range points {
		for _, name := range measured {
			if pt.Name == name {
				t.Errorf("%s was published for a failed exchange; a fabricated 0 reads as a healthy clock", name)
			}
		}
	}
}

// TestMeasureStopsAfterARefusal: a kiss-o'-death applies to us, not to that one
// packet. Retrying it is the exact behaviour the code exists to stop.
func TestMeasureStopsAfterARefusal(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"servers": []interface{}{"ntp.example.org"},
		"samples": 8,
	})
	calls := 0
	p.query = func(string, time.Duration) (sample, error) {
		calls++
		return sample{}, fmt.Errorf("%w: RATE", errRefused)
	}

	if res := p.measure("ntp.example.org"); !errors.Is(res.err, errRefused) {
		t.Fatalf("err = %v, want errRefused", res.err)
	}
	if calls != 1 {
		t.Errorf("sent %d packets after being told to back off, want 1", calls)
	}
}

// TestMeasureSurvivesPartialLoss: UDP drops packets. Losing some samples must
// degrade the estimate, not the measurement.
func TestMeasureSurvivesPartialLoss(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"servers": []interface{}{"ntp.example.org"},
		"samples": 4,
	})
	calls := 0
	p.query = func(string, time.Duration) (sample, error) {
		calls++
		if calls%2 == 1 {
			return sample{}, fmt.Errorf("%w: timeout", errUnreachable)
		}
		return sample{offset: time.Duration(calls) * time.Millisecond, roundTrip: time.Duration(calls) * time.Millisecond}, nil
	}

	res := p.measure("ntp.example.org")
	if res.err != nil {
		t.Fatalf("two lost packets out of four must still yield a measurement, got %v", res.err)
	}
	if res.sample.roundTrip != 2*time.Millisecond {
		t.Errorf("roundTrip = %v, want the least delayed surviving sample (2ms)", res.sample.roundTrip)
	}
}

// TestCollectTagsEverySeriesWithItsServer: without the tag, two servers
// collapse onto one series and the disagreement between them — the entire
// reason to configure more than one — becomes invisible.
func TestCollectTagsEverySeriesWithItsServer(t *testing.T) {
	servers := []interface{}{"a.example.org", "b.example.org"}
	p := newTestProbe(t, map[string]interface{}{"servers": servers, "samples": 1})

	var mu sync.Mutex
	seen := map[string]bool{}
	p.query = func(server string, _ time.Duration) (sample, error) {
		mu.Lock()
		seen[server] = true
		mu.Unlock()
		return sample{offset: time.Millisecond, roundTrip: time.Millisecond, stratum: 2}, nil
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(seen) != 2 {
		t.Errorf("queried %d servers, want 2", len(seen))
	}

	offsets := map[string]bool{}
	for _, pt := range points {
		if pt.Name != "ntp.time.offset" {
			continue
		}
		for _, tag := range pt.Tags {
			if tag.Key == "server" {
				offsets[tag.Value] = true
			}
		}
	}
	if len(offsets) != 2 {
		t.Errorf("offset series carry %d distinct server tags, want 2", len(offsets))
	}
}
