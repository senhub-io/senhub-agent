package proxmox

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

type pveBackupJob struct {
	ID      string          `json:"id"`
	Enabled json.RawMessage `json:"enabled"`
	All     json.RawMessage `json:"all"`
	VMID    string          `json:"vmid"`
	Exclude string          `json:"exclude"`
	Pool    string          `json:"pool"`
}

// jobFlag interprets Proxmox backup job booleans, which may be absent/null,
// numeric 0/1, JSON booleans, or string forms such as "0"/"1".
func jobFlag(raw json.RawMessage, def bool) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return def
	}

	var b bool
	if err := json.Unmarshal(trimmed, &b); err == nil {
		return b
	}

	var s string
	if err := json.Unmarshal(trimmed, &s); err == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "1", "true":
			return true
		default:
			return false
		}
	}

	var n float64
	if err := json.Unmarshal(trimmed, &n); err == nil {
		return n != 0
	}

	return false
}

// parseCSV handles Proxmox VMID lists that may contain spaces and empty entries.
func parseCSV(s string) map[int]bool {
	result := make(map[int]bool)
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if id, err := strconv.Atoi(item); err == nil {
			result[id] = true
		}
	}
	return result
}

func backupCoverage(jobs []pveBackupJob, guests []pveGuest) (enabledJobs, unprotected int) {
	covered := make([]bool, len(guests))

	for _, job := range jobs {
		if !jobFlag(job.Enabled, true) {
			continue
		}
		enabledJobs++

		all := jobFlag(job.All, false)
		exclude := parseCSV(job.Exclude)
		vmids := parseCSV(job.VMID)
		pool := strings.TrimSpace(job.Pool)

		for i, guest := range guests {
			if all {
				if exclude[guest.VMID] {
					continue
				}
				covered[i] = true
				continue
			}

			if vmids[guest.VMID] {
				covered[i] = true
				continue
			}

			if pool != "" && guest.Pool == pool {
				covered[i] = true
			}
		}
	}

	for _, isCovered := range covered {
		if !isCovered {
			unprotected++
		}
	}

	return enabledJobs, unprotected
}

func (p *ProxmoxProbe) collectBackupCoverage(guests []pveGuest, now time.Time) []data_store.DataPoint {
	var jobs []pveBackupJob
	path := "/cluster/backup"

	if err := p.apiGet(path, &jobs); err != nil {
		if !p.denied.isDenied(p.moduleLogger, err, "cluster/backup", "Sys.Audit on /") {
			p.moduleLogger.Warn().Err(err).Msg("failed to list backup jobs")
		}
		return nil
	}

	enabledJobs, unprotected := backupCoverage(jobs, guests)
	metricTags := []tags.Tag{{Key: "metric_type", Value: "backup"}}

	return []data_store.DataPoint{
		{Name: "proxmox.backup.job.count", Value: float64(enabledJobs), Timestamp: now, Tags: metricTags},
		{Name: "proxmox.vm.backup.unprotected.count", Value: float64(unprotected), Timestamp: now, Tags: metricTags},
	}
}
