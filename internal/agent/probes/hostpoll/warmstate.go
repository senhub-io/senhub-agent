package hostpoll

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/host"
)

// StateAware is implemented by a collector that keeps its last cumulative
// readings across a restart. SetState is called once by Probe.OnStart,
// before the first Collect: path is the file the readings go to at Close,
// maxAge the oldest reading worth reusing.
type StateAware interface {
	SetState(path string, maxAge time.Duration)
}

// BootIdentity returns a value that changes across host reboots; it is a
// variable so tests can replace it.
var BootIdentity = readBootIdentity

// readBootIdentity prefers the kernel boot id, falling back to the boot time
// reported by gopsutil.
func readBootIdentity() (string, error) {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err == nil {
		if id := strings.TrimSpace(string(b)); id != "" {
			return id, nil
		}
	}
	bt, err := host.BootTime()
	if err != nil || bt == 0 {
		return "", fmt.Errorf("read boot identity: boot_id unavailable and boot time unavailable: %w", err)
	}
	return strconv.FormatUint(bt, 10), nil
}

// SaveWarm atomically writes v as JSON to path.
func SaveWarm(path string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal warm state: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create warm state dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write warm state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) // best effort: the rename error is what is returned
		return fmt.Errorf("rename warm state: %w", err)
	}
	return nil
}

// TakeWarm reads, unmarshals and removes the warm state file in one go.
func TakeWarm(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read warm state: %w", err)
	}
	if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		return fmt.Errorf("remove warm state: %w", rmErr)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("unmarshal warm state: %w", err)
	}
	return nil
}

// WarmUsable reports whether a saved reading is still safe to reuse.
func WarmUsable(savedBoot string, readAt, now time.Time, maxAge time.Duration) bool {
	if savedBoot == "" || readAt.IsZero() {
		return false
	}
	id, err := BootIdentity()
	if err != nil || id != savedBoot {
		return false
	}
	age := now.Sub(readAt)
	return age >= 0 && age <= maxAge
}
