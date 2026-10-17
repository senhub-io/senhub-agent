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

// diskHealthy maps a SMART verdict to 1/0. ok is false when the verdict
// cannot be judged (no SMART data), in which case no point is emitted.
func diskHealthy(health string) (healthy float64, ok bool) {
	switch strings.ToUpper(strings.TrimSpace(health)) {
	case "":
		return 0, false
	case "UNKNOWN":
		return 0, false
	case "PASSED", "OK":
		return 1, true
	default:
		return 0, true
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
		diskTags := []tags.Tag{
			{Key: "proxmox.node", Value: node},
			{Key: "proxmox.disk", Value: d.DevPath},
			{Key: "proxmox.disk.type", Value: d.Type},
			{Key: "proxmox.disk.model", Value: d.Model},
			{Key: "proxmox.disk.serial", Value: d.Serial},
			{Key: "metric_type", Value: "disk"},
		}
		if h, ok := diskHealthy(d.Health); ok {
			healthTags := append(append([]tags.Tag{}, diskTags...), tags.Tag{Key: "proxmox.health", Value: d.Health})
			points = append(points, data_store.DataPoint{Name: "proxmox.disk.health", Value: h, Timestamp: now, Tags: healthTags})
		}
		// PVE reports the life left in percent; the metric is the life used.
		if left := numberOrNil(d.Wearout); left != nil {
			points = append(points, data_store.DataPoint{Name: "proxmox.disk.wearout", Value: (100 - *left) / 100, Timestamp: now, Tags: diskTags})
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
			{Key: "proxmox.zfs.pool", Value: z.Name},
			{Key: "metric_type", Value: "zfs"},
		}
		if z.Health != "" {
			online := float64(0)
			if strings.EqualFold(z.Health, "ONLINE") {
				online = 1
			}
			healthTags := append(append([]tags.Tag{}, poolTags...), tags.Tag{Key: "proxmox.health", Value: z.Health})
			points = append(points, data_store.DataPoint{Name: "proxmox.zfs.pool.health", Value: online, Timestamp: now, Tags: healthTags})
		}
		for _, m := range []struct {
			name string
			v    *float64
		}{
			{"proxmox.zfs.pool.size", z.Size},
			{"proxmox.zfs.pool.used", z.Alloc},
			{"proxmox.zfs.pool.free", z.Free},
		} {
			if m.v != nil {
				points = append(points, data_store.DataPoint{Name: m.name, Value: *m.v, Timestamp: now, Tags: poolTags})
			}
		}
		if frag := numberOrNil(z.Frag); frag != nil {
			points = append(points, data_store.DataPoint{Name: "proxmox.zfs.pool.fragmentation", Value: *frag / 100, Timestamp: now, Tags: poolTags})
		}
	}
	return points
}
