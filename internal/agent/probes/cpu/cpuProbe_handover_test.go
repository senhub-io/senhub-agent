//go:build !windows

package cpu

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// idleHost models the CPU times of an idle host as the kernel counts
// them: in ticks of 10 ms, so a mode's cumulative time only moves when a
// whole tick has been spent in it.
type idleHost struct {
	cpus    int
	user    float64 // share of CPU time in user mode
	system  float64
	elapsed time.Duration
	// switches is the context-switch rate, per second.
	switches float64
}

func (h *idleHost) ticks(share float64) float64 {
	return math.Floor(float64(h.cpus)*h.elapsed.Seconds()*100*share) / 100
}

func (h *idleHost) times() ([]cpu.TimesStat, error) {
	total := float64(h.cpus) * h.elapsed.Seconds()
	user, system := h.ticks(h.user), h.ticks(h.system)
	return []cpu.TimesStat{{User: user, System: system, Idle: total - user - system}}, nil
}

func (h *idleHost) kernel() (*kernelCounters, error) {
	return &kernelCounters{ContextSwitches: math.Floor(h.elapsed.Seconds() * h.switches)}, nil
}

func (h *idleHost) collector(handover *hostpoll.Handover[cpu.TimesStat], kernel *hostpoll.Handover[kernelCounters]) *unixCollector {
	return &unixCollector{
		logger:     logger.NewLogger(&cliArgs.ParsedArgs{}),
		readTimes:  h.times,
		readKernel: h.kernel,
		sleep:      func(d time.Duration) { h.elapsed += d },
		times:      handover,
		kernel:     kernel,
	}
}

func valueOf(t *testing.T, points []data_store.DataPoint, name string) float64 {
	t.Helper()
	for _, p := range points {
		if p.Name == name {
			return p.Value
		}
	}
	t.Fatalf("%s not emitted: %v", name, points)
	return 0
}

// On a recette host an overlay moved the cpu probe from 30 s to 60 s; the
// new instance primed over one second and read user mode at exactly 0
// between points of 0.05 to 0.2 %. A new instance now diffs against the
// last reading of the one it replaces.
func TestARestartedCPUProbeReadsLikeThePointsAroundIt(t *testing.T) {
	boot := time.Unix(1780272000, 0)
	for _, cpus := range []int{1, 2} {
		t.Run(fmt.Sprintf("%d vCPU", cpus), func(t *testing.T) {
			host := &idleHost{cpus: cpus, user: 0.001, system: 0.002, elapsed: time.Hour, switches: 400}
			times, kernel := &hostpoll.Handover[cpu.TimesStat]{}, &hostpoll.Handover[kernelCounters]{}
			baseTags := []tags.Tag{{Key: "host", Value: "test"}}
			collect := func(u *unixCollector) []data_store.DataPoint {
				var points []data_store.DataPoint
				at := boot.Add(host.elapsed)
				if err := u.collectCPUTimes(&points, at, baseTags); err != nil {
					t.Fatal(err)
				}
				if err := u.collectKernelCounters(&points, at, baseTags); err != nil {
					t.Fatal(err)
				}
				return points
			}

			old := host.collector(times, kernel)
			collect(old)
			var around []float64
			for i := 0; i < 3; i++ {
				host.elapsed += 30 * time.Second
				around = append(around, valueOf(t, collect(old), "cpu_user"))
			}

			// The overlay lands two seconds after the old instance's last
			// cycle, and the new instance collects at once.
			host.elapsed += 2 * time.Second
			before := host.elapsed
			fresh := host.collector(times, kernel)
			points := collect(fresh)

			if host.elapsed != before {
				t.Errorf("the new instance waited %s although an earlier reading was there", host.elapsed-before)
			}
			got := valueOf(t, points, "cpu_user")
			lo, hi := around[0], around[0]
			for _, v := range around {
				lo, hi = math.Min(lo, v), math.Max(hi, v)
			}
			if got <= 0 || got < lo/2 || got > hi*2 {
				t.Errorf("first user %% after the restart = %.4f, points around it %.4f..%.4f", got, lo, hi)
			}
			if rate := valueOf(t, points, "cpu_context_switches"); math.Abs(rate-host.switches) > host.switches*0.05 {
				t.Errorf("first context-switch rate after the restart = %.1f/s, want about %.0f/s", rate, host.switches)
			}
		})
	}
}

// Without any earlier reading (the agent just started) the first
// collection waits for PrimeWindow, not one second.
func TestAColdCPUProbePrimesOverThePrimeWindow(t *testing.T) {
	host := &idleHost{cpus: 2, user: 0.001, system: 0.002, elapsed: time.Hour, switches: 400}
	u := host.collector(&hostpoll.Handover[cpu.TimesStat]{}, &hostpoll.Handover[kernelCounters]{})
	var points []data_store.DataPoint
	if err := u.collectCPUTimes(&points, time.Unix(1780272000, 0).Add(host.elapsed), nil); err != nil {
		t.Fatal(err)
	}
	if waited := host.elapsed - time.Hour; waited != hostpoll.PrimeWindow {
		t.Errorf("a cold first collection waited %s, want %s", waited, hostpoll.PrimeWindow)
	}
	if got := valueOf(t, points, "cpu_user"); got <= 0 {
		t.Errorf("user %% over the prime window on 2 vCPU at 0.1 %% = %.4f", got)
	}
}

// A cold first collection waits once: the kernel counters take their base
// before the CPU times prime, instead of priming again after them.
func TestAColdCollectionWaitsOnceForTimesAndKernelCounters(t *testing.T) {
	host := &idleHost{cpus: 2, user: 0.001, system: 0.002, elapsed: time.Hour, switches: 400}
	u := host.collector(&hostpoll.Handover[cpu.TimesStat]{}, &hostpoll.Handover[kernelCounters]{})
	at := time.Unix(1780272000, 0).Add(host.elapsed)
	u.slept, u.kernelBase = 0, nil
	if b, err := u.kernelCounters(); err == nil {
		u.kernelBase = b
	}
	var points []data_store.DataPoint
	if err := u.collectCPUTimes(&points, at, nil); err != nil {
		t.Fatal(err)
	}
	if err := u.collectKernelCounters(&points, at, nil); err != nil {
		t.Fatal(err)
	}
	if waited := host.elapsed - time.Hour; waited != hostpoll.PrimeWindow {
		t.Errorf("a cold collection waited %s in all, want one %s", waited, hostpoll.PrimeWindow)
	}
	if rate := valueOf(t, points, "cpu_context_switches"); math.Abs(rate-host.switches) > host.switches*0.05 {
		t.Errorf("context-switch rate over the shared wait = %.1f/s, want about %.0f/s", rate, host.switches)
	}
	if !u.lastKernelAt.Equal(at.Add(hostpoll.PrimeWindow)) {
		t.Errorf("kernel reading dated %s, want %s", u.lastKernelAt, at.Add(hostpoll.PrimeWindow))
	}
}
