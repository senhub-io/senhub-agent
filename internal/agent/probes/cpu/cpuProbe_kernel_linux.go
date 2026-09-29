//go:build linux

package cpu

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// procStat is where the kernel publishes the counters a native Zabbix
// agent reports as system.cpu.intr and system.cpu.switches, beside the
// number of processes currently runnable.
const procStat = "/proc/stat"

// readKernelCounters reads one line of each counter from /proc/stat.
// gopsutil exposes the context switches and the runnable processes but
// not the interrupt count, and reading the file once for the three is
// cheaper than asking twice.
func readKernelCounters() (*kernelCounters, error) {
	f, err := os.Open(procStat)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", procStat, err)
	}
	defer f.Close()

	out := &kernelCounters{}
	seen := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		field, rest, ok := strings.Cut(sc.Text(), " ")
		if !ok {
			continue
		}
		var target *float64
		switch field {
		case "intr":
			// The first number is the total; the rest is one count per
			// interrupt line, which we do not report.
			target = &out.Interrupts
		case "ctxt":
			target = &out.ContextSwitches
		case "procs_running":
			target = &out.ProcsRunning
		default:
			continue
		}
		first, _, _ := strings.Cut(strings.TrimSpace(rest), " ")
		v, convErr := strconv.ParseFloat(first, 64)
		if convErr != nil {
			return nil, fmt.Errorf("reading %s from %s: %w", field, procStat, convErr)
		}
		*target = v
		seen++
		if seen == 3 {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", procStat, err)
	}
	if seen == 0 {
		return nil, fmt.Errorf("%s carried none of the expected counters", procStat)
	}
	return out, nil
}
