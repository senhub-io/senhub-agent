// Package operstate is the single mapping from the two producers of interface
// operational state — SNMP IF-MIB ifOperStatus and Linux sysfs operstate — to
// the closed oper_state vocabulary (RFC 2863 ifOperStatus, snake_case).
package operstate

import (
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

const (
	Up             = "up"
	Down           = "down"
	Testing        = "testing"
	Unknown        = "unknown"
	Dormant        = "dormant"
	NotPresent     = "not_present"
	LowerLayerDown = "lower_layer_down"
)

// FromIFMIB maps the IF-MIB ifOperStatus integer (1..7) to the oper_state
// value. Anything outside 1..7 is not in the vocabulary → omitted (ok=false).
func FromIFMIB(v int) (string, bool) {
	switch v {
	case 1:
		return Up, true
	case 2:
		return Down, true
	case 3:
		return Testing, true
	case 4:
		return Unknown, true
	case 5:
		return Dormant, true
	case 6:
		return NotPresent, true
	case 7:
		return LowerLayerDown, true
	default:
		reportUnmapped("if-mib", strconv.Itoa(v))
		return "", false
	}
}

// FromLinuxSysfs maps the Linux sysfs operstate string to the oper_state
// value. The string is trimmed and lowercased first; anything outside the
// table (including an empty/missing file) → omitted (ok=false).
func FromLinuxSysfs(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "up":
		return Up, true
	case "down":
		return Down, true
	case "testing":
		return Testing, true
	case "unknown":
		return Unknown, true
	case "dormant":
		return Dormant, true
	case "notpresent":
		return NotPresent, true
	case "lowerlayerdown":
		return LowerLayerDown, true
	default:
		if v := strings.TrimSpace(s); v != "" {
			reportUnmapped("linux-sysfs", v)
		}
		return "", false
	}
}

// maxUnmapped bounds the values remembered: an SNMP device answers any
// integer, so an unbounded set would let it grow memory and the log.
const maxUnmapped = 64

// unmapped remembers the values already reported, so each one is logged
// once per process: an omitted oper_state is otherwise indistinguishable
// from a state never measured, and the table would stop being complete
// without anyone learning it.
var unmapped = struct {
	sync.Mutex
	seen   map[string]struct{}
	capped bool
}{seen: map[string]struct{}{}}

func reportUnmapped(source, value string) {
	key := source + "\x00" + value
	unmapped.Lock()
	defer unmapped.Unlock()
	if _, ok := unmapped.seen[key]; ok {
		return
	}
	if len(unmapped.seen) >= maxUnmapped {
		if !unmapped.capped {
			unmapped.capped = true
			log.Warn().Int("reported", maxUnmapped).Msg("Further interface operational states outside the oper_state table are not reported")
		}
		return
	}
	unmapped.seen[key] = struct{}{}
	log.Warn().Str("source", source).Str("value", value).Msg("Interface operational state outside the oper_state table; the attribute is omitted until the table maps it")
}
