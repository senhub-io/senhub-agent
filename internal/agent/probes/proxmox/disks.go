package proxmox

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// deniedLog remembers which endpoints already answered 403 so the missing
// permission is reported once, not at every cycle.
type deniedLog struct {
	mu   sync.Mutex
	seen map[string]bool
}

// isDenied reports whether err is a 403 and, the first time for a given
// key, logs which privilege the token lacks.
func (d *deniedLog) isDenied(log *logger.ModuleLogger, err error, key, privilege string) bool {
	var se *apiStatusError
	if !errors.As(err, &se) || se.Status != http.StatusForbidden {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen == nil {
		d.seen = map[string]bool{}
	}
	if !d.seen[key] {
		d.seen[key] = true
		log.Info().Str("endpoint", se.Path).Str("privilege", privilege).
			Msg("API token is not allowed to read this endpoint, nothing is collected from it")
	}
	return true
}

type pveDisk struct {
	DevPath string          `json:"devpath"`
	Health  string          `json:"health"`
	Wearout json.RawMessage `json:"wearout"`
	Type    string          `json:"type"`
	Model   string          `json:"model"`
	Serial  string          `json:"serial"`
}

type pveZFSPool struct {
	Name   string          `json:"name"`
	Health string          `json:"health"`
	Size   *float64        `json:"size"`
	Alloc  *float64        `json:"alloc"`
	Free   *float64        `json:"free"`
	Frag   json.RawMessage `json:"frag"`
}

// numberOrNil reads a JSON number, or a numeric string, and returns nil for
// anything else ("N/A", null, absent).
func numberOrNil(raw json.RawMessage) *float64 {
	if len(raw) == 0 {
		return nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return &f
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil
	}
	return &f
}

// stateSeries emits one point per state of an enumerated status, 1 for the
// current state and 0 for the others, as the hw.status convention does.
func stateSeries(name, stateKey string, states []string, current string, base []tags.Tag, now time.Time) []data_store.DataPoint {
	out := make([]data_store.DataPoint, 0, len(states))
	for _, st := range states {
		v := float64(0)
		if st == current {
			v = 1
		}
		t := append(append([]tags.Tag{}, base...), tags.Tag{Key: stateKey, Value: st})
		out = append(out, data_store.DataPoint{Name: name, Value: v, Timestamp: now, Tags: t})
	}
	return out
}

func boolState(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}

var diskStates = []string{"ok", "degraded", "failed", "predicted_failure"}

// diskState maps a SMART verdict to an hw.state. ok is false when the
// verdict cannot be judged (no SMART data), in which case nothing is emitted.
func diskState(health string) (state string, ok bool) {
	switch strings.ToUpper(strings.TrimSpace(health)) {
	case "", "UNKNOWN":
		return "", false
	case "PASSED", "OK":
		return "ok", true
	default:
		return "failed", true
	}
}

var poolStates = []string{"ok", "degraded", "failed"}

// poolState maps a ZFS pool state to an hw.state; OFFLINE is failed because
// an offline top-level vdev takes the pool out of service.
func poolState(health string) (state string, ok bool) {
	switch strings.ToUpper(strings.TrimSpace(health)) {
	case "":
		return "", false
	case "ONLINE":
		return "ok", true
	case "DEGRADED":
		return "degraded", true
	default:
		return "failed", true
	}
}

// collectDisks reads physical disk SMART health and ZFS pool state of one
// node. Both need Sys.Audit on /nodes/<node>; a token without it gets 403
// and the node simply contributes nothing.
func (p *ProxmoxProbe) collectDisks(node string, now time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint

	var disks []pveDisk
	if err := p.apiGet(fmt.Sprintf("/nodes/%s/disks/list", node), &disks); err != nil {
		if !p.denied.isDenied(p.moduleLogger, err, node+"/disks/list", "Sys.Audit on /nodes/"+node) {
			p.moduleLogger.Warn().Err(err).Str("node", node).Msg("failed to list physical disks")
		}
	}
	for _, d := range disks {
		id := d.Serial
		if id == "" {
			id = d.DevPath
		}
		diskTags := []tags.Tag{
			{Key: "proxmox.node", Value: node},
			{Key: "hw.id", Value: id},
			{Key: "hw.name", Value: d.DevPath},
			{Key: "hw.model", Value: d.Model},
			{Key: "hw.serial_number", Value: d.Serial},
			{Key: "hw.physical_disk.type", Value: d.Type},
			{Key: "metric_type", Value: "disk"},
		}
		if state, ok := diskState(d.Health); ok {
			points = append(points, stateSeries("hw.physical_disk.status", "hw.state", diskStates, state, diskTags, now)...)
		}
		// PVE reports the life left in percent.
		if left := numberOrNil(d.Wearout); left != nil {
			wearTags := append(append([]tags.Tag{}, diskTags...), tags.Tag{Key: "hw.physical_disk.state", Value: "remaining"})
			points = append(points, data_store.DataPoint{Name: "hw.physical_disk.endurance_utilization", Value: *left / 100, Timestamp: now, Tags: wearTags})
		}
	}

	var pools []pveZFSPool
	if err := p.apiGet(fmt.Sprintf("/nodes/%s/disks/zfs", node), &pools); err != nil {
		if !p.denied.isDenied(p.moduleLogger, err, node+"/disks/zfs", "Sys.Audit on /nodes/"+node) {
			p.moduleLogger.Warn().Err(err).Str("node", node).Msg("failed to list ZFS pools")
		}
	}
	for _, z := range pools {
		poolTags := []tags.Tag{
			{Key: "proxmox.node", Value: node},
			{Key: "hw.id", Value: z.Name},
			{Key: "metric_type", Value: "zfs"},
		}
		if state, ok := poolState(z.Health); ok {
			points = append(points, stateSeries("hw.logical_disk.status", "hw.state", poolStates, state, poolTags, now)...)
		}
		if z.Size != nil {
			points = append(points, data_store.DataPoint{Name: "hw.logical_disk.limit", Value: *z.Size, Timestamp: now, Tags: poolTags})
			if z.Alloc != nil && *z.Size > 0 {
				points = append(points, data_store.DataPoint{Name: "hw.logical_disk.utilization", Value: *z.Alloc / *z.Size, Timestamp: now, Tags: poolTags})
			}
		}
		for _, m := range []struct {
			state string
			v     *float64
		}{{"used", z.Alloc}, {"free", z.Free}} {
			if m.v != nil {
				t := append(append([]tags.Tag{}, poolTags...), tags.Tag{Key: "hw.logical_disk.state", Value: m.state})
				points = append(points, data_store.DataPoint{Name: "hw.logical_disk.usage", Value: *m.v, Timestamp: now, Tags: t})
			}
		}
		if frag := numberOrNil(z.Frag); frag != nil {
			points = append(points, data_store.DataPoint{Name: "proxmox.zfs.pool.fragmentation", Value: *frag / 100, Timestamp: now, Tags: poolTags})
		}
	}
	return points
}
