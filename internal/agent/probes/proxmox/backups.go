package proxmox

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

const (
	// maxBackupTasks bounds the vzdump task history read per node: the
	// newest first, which is all the last-backup question needs.
	maxBackupTasks = 200
	// maxBackupStorages bounds the storages whose backup archives are listed.
	maxBackupStorages = 8
)

type guestRef struct {
	Name string
	Type string
}

type pveTask struct {
	UPID      string `json:"upid"`
	Node      string `json:"node"`
	Type      string `json:"type"`
	ID        string `json:"id"`
	User      string `json:"user"`
	StartTime int64  `json:"starttime"`
	EndTime   int64  `json:"endtime"`
	Status    string `json:"status"`
}

type pveBackupVolume struct {
	VMID  int   `json:"vmid"`
	CTime int64 `json:"ctime"`
}

// backupOK reports whether a finished vzdump task succeeded. Warnings do
// not fail a backup; any other text is the error.
func backupOK(status string) bool {
	return status == "OK" || strings.HasPrefix(status, "WARNINGS")
}

// collectBackups emits, per guest of the node, the age of its last
// successful backup and whether its last backup task succeeded.
//
// A scheduled job covering several guests is a single task with no guest
// id, so the task history alone cannot date those backups. The archives
// listed on the backup-capable storages (vmid and creation time) can, so
// the age takes the newest of the two sources; the last status only comes
// from tasks that name their guest and is absent otherwise.
func (p *ProxmoxProbe) collectBackups(node string, known map[int]guestRef, storages []pveStorageItem, now time.Time) []data_store.DataPoint {
	if len(known) == 0 {
		return nil
	}

	lastOK := map[int]int64{}
	lastStatus := map[int]struct {
		end int64
		ok  bool
	}{}

	var tasks []pveTask
	q := url.Values{"typefilter": {"vzdump"}, "limit": {strconv.Itoa(maxBackupTasks)}}
	if err := p.apiGet(fmt.Sprintf("/nodes/%s/tasks?%s", node, q.Encode()), &tasks); err != nil {
		if !p.denied.isDenied(p.moduleLogger, err, node+"/tasks", "Sys.Audit on /nodes/"+node) {
			p.moduleLogger.Warn().Err(err).Str("node", node).Msg("failed to list backup tasks")
		}
	}
	for _, t := range tasks {
		vmid, err := strconv.Atoi(t.ID)
		if err != nil || t.EndTime == 0 {
			continue
		}
		if cur, seen := lastStatus[vmid]; !seen || t.EndTime > cur.end {
			lastStatus[vmid] = struct {
				end int64
				ok  bool
			}{t.EndTime, backupOK(t.Status)}
		}
		if backupOK(t.Status) && t.EndTime > lastOK[vmid] {
			lastOK[vmid] = t.EndTime
		}
	}

	listed := 0
	for _, st := range storages {
		if st.Active != nil && *st.Active == 0 {
			continue
		}
		if !contentHas(st.Content, "backup") || listed >= maxBackupStorages {
			continue
		}
		listed++
		var vols []pveBackupVolume
		path := fmt.Sprintf("/nodes/%s/storage/%s/content?content=backup", node, url.PathEscape(st.Storage))
		if err := p.apiGet(path, &vols); err != nil {
			if !p.denied.isDenied(p.moduleLogger, err, node+"/storage/"+st.Storage+"/content", "Datastore.Audit on /storage/"+st.Storage) {
				p.moduleLogger.Warn().Err(err).Str("node", node).Str("storage", st.Storage).Msg("failed to list backup archives")
			}
			continue
		}
		for _, v := range vols {
			if v.CTime > lastOK[v.VMID] {
				lastOK[v.VMID] = v.CTime
			}
		}
	}

	vmids := make([]int, 0, len(known))
	for id := range known {
		vmids = append(vmids, id)
	}
	sort.Ints(vmids)

	var points []data_store.DataPoint
	for _, id := range vmids {
		g := known[id]
		vmTags := []tags.Tag{
			{Key: "proxmox.node", Value: node},
			{Key: "proxmox.vmid", Value: strconv.Itoa(id)},
			{Key: "proxmox.vm.name", Value: g.Name},
			{Key: "proxmox.vm.type", Value: g.Type},
			{Key: "metric_type", Value: "backup"},
		}
		if end := lastOK[id]; end > 0 {
			age := now.Sub(time.Unix(end, 0)).Seconds()
			if age < 0 {
				age = 0
			}
			points = append(points, data_store.DataPoint{Name: "proxmox.vm.backup.age", Value: age, Timestamp: now, Tags: vmTags})
		}
		if st, ok := lastStatus[id]; ok {
			points = append(points, stateSeries("proxmox.vm.backup.status", "proxmox.vm.backup.state", []string{"ok", "failed"}, boolState(st.ok, "ok", "failed"), vmTags, now)...)
		}
	}
	return points
}

func contentHas(content, kind string) bool {
	for _, c := range strings.Split(content, ",") {
		if strings.TrimSpace(c) == kind {
			return true
		}
	}
	return false
}
