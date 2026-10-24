package proxmox

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

var haResourceStates = []string{
	"started", "stopped", "disabled", "ignored", "error", "fence",
	"freeze", "migrate", "relocate", "recovery", "request_stop",
}

type pveHAEntry struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Node    string          `json:"node"`
	Sid     string          `json:"sid"`
	State   string          `json:"state"`
	Status  string          `json:"status"`
	Quorate json.RawMessage `json:"quorate"`
}

func haSidVMID(sid string) string {
	parts := strings.SplitN(sid, ":", 2)
	if len(parts) != 2 || (parts[0] != "vm" && parts[0] != "ct") {
		return ""
	}
	vmid := strings.TrimSpace(parts[1])
	if vmid == "" {
		return ""
	}
	if _, err := strconv.Atoi(vmid); err != nil {
		return ""
	}
	return vmid
}

func haQuorate(raw json.RawMessage) *float64 {
	if v := numberOrNil(raw); v != nil {
		return v
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	// quorate is usually numeric, but some versions send a bare boolean.
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil
	}
	v := 0.0
	if b {
		v = 1.0
	}
	return &v
}

func (p *ProxmoxProbe) collectHA(ctx context.Context, now time.Time) []data_store.DataPoint {
	var entries []pveHAEntry
	if err := p.apiGet(ctx, "/cluster/ha/status/current", &entries); err != nil {
		if !p.denied.isDenied(p.moduleLogger, err, "cluster/ha/status", "Sys.Audit on /") {
			p.moduleLogger.Warn().Err(err).Msg("failed to read HA status")
		}
		return nil
	}

	var points []data_store.DataPoint

	for _, entry := range entries {
		if entry.Type != "quorum" {
			continue
		}
		q := haQuorate(entry.Quorate)
		if q == nil {
			break
		}
		current := "lost"
		if *q != 0 {
			current = "ok"
		}
		quorumTags := []tags.Tag{{Key: "metric_type", Value: "ha"}}
		points = append(points, stateSeries("proxmox.ha.quorum.status", "proxmox.ha.quorum.state",
			[]string{"ok", "lost"}, current, quorumTags, now)...)
		break
	}

	type haServiceState struct {
		sid   string
		node  string
		state string
	}
	var services []haServiceState
	for _, entry := range entries {
		if entry.Type != "service" || entry.Sid == "" {
			continue
		}
		state := strings.ToLower(strings.TrimSpace(entry.State))
		valid := false
		for _, st := range haResourceStates {
			if st == state {
				valid = true
				break
			}
		}
		if !valid {
			continue
		}
		services = append(services, haServiceState{entry.Sid, entry.Node, state})
	}
	sort.Slice(services, func(i, j int) bool { return services[i].sid < services[j].sid })

	for _, svc := range services {
		base := []tags.Tag{
			{Key: "proxmox.ha.resource", Value: svc.sid},
			{Key: "metric_type", Value: "ha"},
		}
		if svc.node != "" {
			base = append(base, tags.Tag{Key: "proxmox.node", Value: svc.node})
		}
		if vmid := haSidVMID(svc.sid); vmid != "" {
			base = append(base, tags.Tag{Key: "proxmox.vmid", Value: vmid})
		}
		points = append(points, stateSeries("proxmox.ha.resource.status", "proxmox.ha.resource.state",
			haResourceStates, svc.state, base, now)...)
	}

	return points
}
