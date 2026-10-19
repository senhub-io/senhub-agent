package proxmox

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

type pveReplicationJob struct {
	ID        string          `json:"id"`
	Guest     json.RawMessage `json:"guest"`
	Target    string          `json:"target"`
	LastSync  json.RawMessage `json:"last_sync"`
	FailCount json.RawMessage `json:"fail_count"`
	Error     string          `json:"error"`
}

func (p *ProxmoxProbe) collectReplication(node string, now time.Time) []data_store.DataPoint {
	var jobs []pveReplicationJob
	if err := p.apiGet(fmt.Sprintf("/nodes/%s/replication", node), &jobs); err != nil {
		if !p.denied.isDenied(p.moduleLogger, err, node+"/replication", "Sys.Audit on /nodes/"+node) {
			p.moduleLogger.Warn().Err(err).Str("node", node).Msg("failed to read replication status")
		}
		return nil
	}

	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })

	var points []data_store.DataPoint
	for _, j := range jobs {
		base := []tags.Tag{
			{Key: "proxmox.node", Value: node},
			{Key: "proxmox.replication.job", Value: j.ID},
			{Key: "metric_type", Value: "replication"},
		}
		if guest := numberOrNil(j.Guest); guest != nil {
			base = append(base, tags.Tag{Key: "proxmox.vmid", Value: strconv.FormatFloat(*guest, 'f', -1, 64)})
		}
		if j.Target != "" {
			base = append(base, tags.Tag{Key: "proxmox.replication.target", Value: j.Target})
		}

		failed := false
		if failCount := numberOrNil(j.FailCount); failCount != nil && *failCount > 0 {
			failed = true
		}
		if strings.TrimSpace(j.Error) != "" {
			failed = true
		}

		points = append(points, stateSeries("proxmox.replication.job.status", "proxmox.replication.job.state", []string{"ok", "failed"}, boolState(failed, "failed", "ok"), base, now)...)

		if lastSync := numberOrNil(j.LastSync); lastSync != nil && *lastSync > 0 {
			age := now.Sub(time.Unix(int64(*lastSync), 0)).Seconds()
			if age < 0 {
				age = 0
			}
			points = append(points, data_store.DataPoint{Name: "proxmox.replication.job.age", Value: age, Timestamp: now, Tags: base})
		}
	}
	return points
}
