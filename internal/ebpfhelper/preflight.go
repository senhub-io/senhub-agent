package ebpfhelper

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Exit codes of the helper. A refusal is not a crash: the unit lists them in
// SuccessExitStatus-style handling so that systemd does not restart a helper
// that can never work on this host.
const (
	ExitOK          = 0
	ExitUnsupported = 3 // kernel too old or no BTF
	ExitNoCaps      = 4 // CAP_BPF / CAP_PERFMON missing
	ExitFailed      = 1
)

// MinKernel is the first kernel on which CAP_BPF and CAP_PERFMON are enough to
// load the programs (5.8) and the ring buffer exists.
var MinKernel = [2]int{5, 8}

// Capability numbers (linux/capability.h).
const (
	capPerfmon = 38
	capBPF     = 39
)

// RefusalError is a refusal to start, with the exit code that goes with it.
type RefusalError struct {
	Code   int
	Reason string
}

func (e *RefusalError) Error() string { return e.Reason }

// Host is what the start-up checks look at, injectable for tests.
type Host struct {
	KernelRelease func() (string, error)
	HasBTF        func() bool
	EffectiveCaps func() (uint64, error)
}

// Preflight checks that this host can run the helper and returns the kernel
// release. The error is a *RefusalError carrying the exit code.
func Preflight(h Host) (string, error) {
	rel, err := h.KernelRelease()
	if err != nil {
		return "", &RefusalError{Code: ExitFailed, Reason: fmt.Sprintf("reading kernel release: %v", err)}
	}
	major, minor, err := ParseKernel(rel)
	if err != nil {
		return rel, &RefusalError{Code: ExitFailed, Reason: fmt.Sprintf("unreadable kernel release %q: %v", rel, err)}
	}
	if major < MinKernel[0] || (major == MinKernel[0] && minor < MinKernel[1]) {
		return rel, &RefusalError{Code: ExitUnsupported, Reason: fmt.Sprintf(
			"kernel %s is unsupported: eBPF collection needs kernel %d.%d or newer (this includes RHEL 8 and its 4.18 kernel)",
			rel, MinKernel[0], MinKernel[1])}
	}
	if !h.HasBTF() {
		return rel, &RefusalError{Code: ExitUnsupported, Reason: "kernel BTF is missing (/sys/kernel/btf/vmlinux): eBPF collection needs a kernel built with CONFIG_DEBUG_INFO_BTF"}
	}
	caps, err := h.EffectiveCaps()
	if err != nil {
		return rel, &RefusalError{Code: ExitFailed, Reason: fmt.Sprintf("reading capabilities: %v", err)}
	}
	var missing []string
	if caps&(1<<capBPF) == 0 {
		missing = append(missing, "CAP_BPF")
	}
	if caps&(1<<capPerfmon) == 0 {
		missing = append(missing, "CAP_PERFMON")
	}
	if len(missing) > 0 {
		return rel, &RefusalError{Code: ExitNoCaps, Reason: "missing capabilities " + strings.Join(missing, ", ") + ": start the helper from its systemd unit, which grants exactly CAP_BPF and CAP_PERFMON"}
	}
	return rel, nil
}

// ParseKernel extracts major and minor from a release such as "6.8.0-142-generic".
func ParseKernel(release string) (major, minor int, err error) {
	parts := strings.SplitN(release, ".", 3)
	if len(parts) < 2 {
		return 0, 0, errors.New("no major.minor")
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("major: %w", err)
	}
	digits := parts[1]
	for i, c := range digits {
		if c < '0' || c > '9' {
			digits = digits[:i]
			break
		}
	}
	minor, err = strconv.Atoi(digits)
	if err != nil {
		return 0, 0, fmt.Errorf("minor: %w", err)
	}
	return major, minor, nil
}

// ParseCapEff reads the CapEff mask out of the text of /proc/self/status.
func ParseCapEff(status string) (uint64, error) {
	for _, line := range strings.Split(status, "\n") {
		if v, ok := strings.CutPrefix(line, "CapEff:"); ok {
			return strconv.ParseUint(strings.TrimSpace(v), 16, 64)
		}
	}
	return 0, errors.New("no CapEff line")
}
