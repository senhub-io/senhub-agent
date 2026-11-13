package snmppoll

import (
	"errors"
	"testing"
	"time"
)

// sweepCountingClient counts the reads that reach the device.
type sweepCountingClient struct {
	*fakeClient
	reads int
}

func (c *sweepCountingClient) Get(oids []string) ([]snmpVarBind, error) {
	c.reads++
	return c.fakeClient.Get(oids)
}

func (c *sweepCountingClient) BulkWalk(base string) ([]snmpVarBind, error) {
	c.reads++
	return c.fakeClient.BulkWalk(base)
}

func (c *sweepCountingClient) WalkRaw(base string) ([]snmpRawBind, error) {
	c.reads++
	return c.fakeClient.WalkRaw(base)
}

func silentDevice() *sweepCountingClient {
	timeout := errors.New("request timeout (after 2 retries)")
	return &sweepCountingClient{fakeClient: &fakeClient{getErr: timeout, walkErr: timeout, walkRawErr: timeout}}
}

// Against a device that answers nothing, a sweep made a dozen reads, each
// waiting for the request timeout and its retries, inside the probe's
// collection: on a recette host its first collection never returned. The
// first timed-out read now ends the sweep.
func TestASweepOfASilentDeviceStopsAtItsFirstTimeout(t *testing.T) {
	dev := silentDevice()
	s := newEntitySource(&config{Target: "192.0.2.10", Interval: time.Minute, TopologyInterval: time.Minute}, testLogger(t))
	now := time.Unix(1780272000, 0)
	s.now = func() time.Time { return now }

	s.maybeSweep(dev, now)

	if dev.reads != 1 {
		t.Errorf("%d reads reached a silent device in one sweep, want 1", dev.reads)
	}
	if s.DeviceID() != "" {
		t.Errorf("a silent device resolved to %q", s.DeviceID())
	}
}

// A device that answered before a read timed out is not silent: one slow
// table must not cost the rest of the sweep.
func TestATimeoutAfterAnAnswerDoesNotEndTheSweep(t *testing.T) {
	inner := &sweepCountingClient{fakeClient: &fakeClient{}}
	c := newSweepClient(inner, time.Minute, time.Now)
	if _, err := c.WalkRaw("1.3.6.1.2.1.1"); err != nil {
		t.Fatal(err)
	}
	inner.walkErr = errors.New("request timeout (after 2 retries)")
	_, _ = c.BulkWalk("1.3.6.1.2.1.2.2.1.10")
	inner.walkErr = nil
	if _, err := c.BulkWalk("1.3.6.1.2.1.2.2.1.16"); err != nil {
		t.Errorf("a read after one timeout on an answering device was refused: %v", err)
	}
	if inner.reads != 3 {
		t.Errorf("%d reads reached the device, want 3", inner.reads)
	}
}

// Past its budget a sweep skips its remaining walks but still reads the
// identity, without which the sweep would count as failed.
func TestASweepPastItsBudgetSkipsWalksButReadsTheIdentity(t *testing.T) {
	inner := &sweepCountingClient{fakeClient: &fakeClient{}}
	clock := time.Unix(1780272000, 0)
	c := newSweepClient(inner, 30*time.Second, func() time.Time { return clock })
	if _, err := c.WalkRaw("1.0.8802.1.1.2.1.4.1.1"); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(31 * time.Second)

	if _, err := c.WalkRaw("1.3.6.1.2.1.4.24.4.1"); !errors.Is(err, errSweepBudget) {
		t.Errorf("walk past the budget: err = %v, want errSweepBudget", err)
	}
	if _, err := c.Get([]string{oidSysServices}); err != nil {
		t.Errorf("identity read past the budget refused: %v", err)
	}
	if !c.cut {
		t.Error("the sweep does not report it was cut")
	}
	if inner.reads != 2 {
		t.Errorf("%d reads reached the device, want 2", inner.reads)
	}
}

// A failed sweep is retried at the next cycle, as audit D3 asks, then
// further apart while the device stays silent, up to five minutes; a good
// sweep ends the backoff.
func TestAFailedSweepIsRetriedLessAndLessOften(t *testing.T) {
	const cycle = time.Minute
	dev := silentDevice()
	s := newEntitySource(&config{Target: "192.0.2.10", Interval: cycle, TopologyInterval: cycle}, testLogger(t))
	t0 := time.Unix(1780272000, 0)
	var swept []int
	for i := 0; i <= 20; i++ {
		before := dev.reads
		s.maybeSweep(dev, t0.Add(time.Duration(i)*cycle))
		if dev.reads > before {
			swept = append(swept, i)
		}
	}
	want := []int{0, 1, 3, 7, 12, 17}
	if len(swept) != len(want) {
		t.Fatalf("sweeps at cycles %v, want %v", swept, want)
	}
	for i := range want {
		if swept[i] != want[i] {
			t.Fatalf("sweeps at cycles %v, want %v", swept, want)
		}
	}

	good := &fakeClient{walkRawResult: map[string][]snmpRawBind{
		oidSysNameBase: {{OID: oidSysName, Value: []byte("sw1")}},
	}}
	s.maybeSweep(good, t0.Add(22*cycle))
	if s.DeviceID() == "" {
		t.Fatal("the device did not resolve once it answered")
	}
	if s.sweepFailures != 0 || !s.retryAt.IsZero() {
		t.Errorf("backoff kept after a good sweep: %d failures, retry at %s", s.sweepFailures, s.retryAt)
	}
}

func TestSweepRetryDelay(t *testing.T) {
	cases := []struct {
		poll     time.Duration
		failures int
		want     time.Duration
	}{
		{time.Minute, 1, time.Minute},
		{time.Minute, 2, 2 * time.Minute},
		{time.Minute, 3, 4 * time.Minute},
		{time.Minute, 4, 5 * time.Minute},
		{time.Minute, 30, 5 * time.Minute},
		{10 * time.Minute, 3, 10 * time.Minute},
		{0, 5, 0},
	}
	for _, c := range cases {
		if got := sweepRetryDelay(c.poll, c.failures); got != c.want {
			t.Errorf("sweepRetryDelay(%s, %d) = %s, want %s", c.poll, c.failures, got, c.want)
		}
	}
}

// A device identified earlier that stops answering: each metric column
// waited for the request timeout and its retries, past the poll interval
// on a device with a few dozen columns. The first timed-out read now ends
// the cycle's walks.
func TestACycleOnASilentDeviceStopsAtItsFirstTimeout(t *testing.T) {
	dev := silentDevice()
	cfg := &config{MIBs: []string{"if-mib"}}
	_, answered := collect(dev, cfg, "192.0.2.10:161", "name:sw1", nil, time.Now(), testLogger(t))
	if answered {
		t.Error("a silent device reported as answering")
	}
	if dev.reads != 1 {
		t.Errorf("%d reads reached a silent device in one cycle, want 1", dev.reads)
	}

	refusing := &sweepCountingClient{fakeClient: &fakeClient{walkErr: errors.New("authorizationError")}}
	_, _ = collect(refusing, cfg, "192.0.2.10:161", "name:sw1", nil, time.Now(), testLogger(t))
	if _, walks := buildPlan(cfg); refusing.reads != len(walks) {
		t.Errorf("%d reads for %d columns: an error that is not a timeout must not end the cycle", refusing.reads, len(walks))
	}
}
