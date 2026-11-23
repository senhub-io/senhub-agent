package ebpfclient

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// State is where the helper stands on this host.
type State string

const (
	NotInstalled State = "not installed"
	NotRunning   State = "not running"
	Running      State = "running"
	Unsupported  State = "unsupported"
)

// Status is what `senhub-agent doctor` reports about eBPF collection.
type Status struct {
	State  State
	Kernel string
	BTF    bool
	// Detail carries the reason of a state that is not Running.
	Detail         string
	HelperVersion  string
	Dropped        uint64
	TrackedConns   int
	ProtocolFailed bool
}

// Inspect works out the helper's state. It never fails: an unreadable fact is
// reported as absent.
func Inspect(c *Client) Status {
	return inspect(osHost(c))
}

func osHost(c *Client) hostFacts {
	return hostFacts{
		kernel: func() string {
			b, err := os.ReadFile("/proc/sys/kernel/osrelease")
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(b))
		},
		btf: func() bool {
			_, err := os.Stat("/sys/kernel/btf/vmlinux")
			return err == nil
		},
		installed: func() bool {
			for _, p := range []string{
				"/usr/bin/senhub-ebpf", "/usr/local/bin/senhub-ebpf",
				"/usr/lib/systemd/system/senhub-ebpf.service", "/etc/systemd/system/senhub-ebpf.service",
			} {
				if _, err := os.Stat(p); err == nil {
					return true
				}
			}
			return false
		},
		present: c.Present,
		hello: func() (helloAnswer, error) {
			r, err := c.Hello()
			if err != nil {
				return helloAnswer{}, err
			}
			return helloAnswer{version: r.HelperVersion, kernel: r.Kernel, dropped: r.Dropped, tracked: r.Tracked}, nil
		},
	}
}

type helloAnswer struct {
	version string
	kernel  string
	dropped uint64
	tracked int
}

type hostFacts struct {
	kernel    func() string
	btf       func() bool
	installed func() bool
	present   func() bool
	hello     func() (helloAnswer, error)
}

func inspect(h hostFacts) Status {
	st := Status{Kernel: h.kernel(), BTF: h.btf()}
	if st.Kernel != "" && kernelTooOld(st.Kernel) {
		st.State = Unsupported
		st.Detail = fmt.Sprintf("kernel %s is older than 5.8", st.Kernel)
		return st
	}
	if h.present() {
		ans, err := h.hello()
		if err != nil {
			st.State = NotRunning
			st.Detail = fmt.Sprintf("the socket exists but the helper does not answer: %v", err)
			st.ProtocolFailed = true
			return st
		}
		st.State = Running
		st.HelperVersion = ans.version
		st.Dropped = ans.dropped
		st.TrackedConns = ans.tracked
		return st
	}
	if !h.installed() {
		st.State = NotInstalled
		return st
	}
	st.State = NotRunning
	st.Detail = "installed, not started"
	return st
}

func kernelTooOld(release string) bool {
	parts := strings.SplitN(release, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minorDigits := parts[1]
	for i, c := range minorDigits {
		if c < '0' || c > '9' {
			minorDigits = minorDigits[:i]
			break
		}
	}
	minor, err := strconv.Atoi(minorDigits)
	if err != nil {
		return false
	}
	return major < 5 || (major == 5 && minor < 8)
}
