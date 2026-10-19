package proxmox

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

const (
	// maxFailedTasks bounds the failed tasks read per node and per cycle.
	maxFailedTasks = 50
	// failedTaskLookback is how far back failed tasks are asked for. It
	// exceeds the longest realistic task (a backup of several hours) because
	// the API filters on start time, not end time; the seen set removes the
	// overlap between cycles.
	failedTaskLookback = 24 * time.Hour
	// failedTaskLogLines is how many trailing log lines go in a record body.
	failedTaskLogLines = 20
	// maxFailedTaskLogs bounds the log fetches per node and per cycle.
	maxFailedTaskLogs = 20
)

// failedTasks remembers which failed tasks were already reported, so a task
// is counted and published once however many cycles still see it.
type failedTasks struct {
	mu    sync.Mutex
	floor time.Time
	seen  map[string]int64
	types map[string]map[string]struct{}
}

// claim returns the tasks not yet reported and marks them reported. The first
// call fixes the floor: tasks that ended before it belong to a time the probe
// was not running and are left alone rather than replayed at every restart.
func (f *failedTasks) claim(node string, tasks []pveTask, now time.Time, interval time.Duration) []pveTask {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seen == nil {
		f.seen = map[string]int64{}
		f.types = map[string]map[string]struct{}{}
		f.floor = now.Add(-interval)
	}
	for upid, end := range f.seen {
		if time.Unix(end, 0).Before(now.Add(-2 * failedTaskLookback)) {
			delete(f.seen, upid)
		}
	}
	var fresh []pveTask
	for _, t := range tasks {
		if t.UPID == "" || t.EndTime == 0 || time.Unix(t.EndTime, 0).Before(f.floor) {
			continue
		}
		if _, dup := f.seen[t.UPID]; dup {
			continue
		}
		f.seen[t.UPID] = t.EndTime
		fresh = append(fresh, t)
	}
	return fresh
}

// typesFor returns the task types ever seen failing on the node, recording
// the ones in fresh, so a type that stopped failing reports 0 instead of
// vanishing.
func (f *failedTasks) typesFor(node string, fresh []pveTask) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	set := f.types[node]
	if set == nil {
		set = map[string]struct{}{}
		f.types[node] = set
	}
	for _, t := range fresh {
		set[t.Type] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for ty := range set {
		out = append(out, ty)
	}
	sort.Strings(out)
	return out
}

type pveTaskLogLine struct {
	N int    `json:"n"`
	T string `json:"t"`
}

// collectFailedTasks counts the tasks that failed since the previous cycle,
// per task type, and publishes each of them once as an ERROR log record.
func (p *ProxmoxProbe) collectFailedTasks(node string, now time.Time) []data_store.DataPoint {
	var tasks []pveTask
	q := url.Values{
		"errors": {"1"},
		"since":  {strconv.FormatInt(now.Add(-failedTaskLookback).Unix(), 10)},
		"limit":  {strconv.Itoa(maxFailedTasks)},
	}
	if err := p.apiGet(fmt.Sprintf("/nodes/%s/tasks?%s", node, q.Encode()), &tasks); err != nil {
		if !p.denied.isDenied(p.moduleLogger, err, node+"/tasks", "Sys.Audit on /nodes/"+node) {
			p.moduleLogger.Warn().Err(err).Str("node", node).Msg("failed to list failed tasks")
		}
		return nil
	}

	fresh := p.tasks.claim(node, tasks, now, p.cfg.Interval)
	counts := map[string]int{}
	for _, t := range fresh {
		counts[t.Type]++
	}

	for i, t := range fresh {
		body := t.Status
		if i < maxFailedTaskLogs {
			if tail := p.fetchTaskLogTail(node, t.UPID); tail != "" {
				body = tail
			}
		}
		p.publishFailedTask(node, t, body)
	}

	var points []data_store.DataPoint
	for _, ty := range p.tasks.typesFor(node, fresh) {
		points = append(points, data_store.DataPoint{
			Name:      "proxmox.task.count",
			Value:     float64(counts[ty]),
			Timestamp: now,
			Tags: []tags.Tag{
				{Key: "proxmox.node", Value: node},
				{Key: "proxmox.task.type", Value: ty},
				{Key: "proxmox.task.state", Value: "failed"},
				{Key: "metric_type", Value: "tasks"},
			},
		})
	}
	return points
}

// fetchTaskLogTail returns the last lines of a task log, or "" when it cannot
// be read; the caller then falls back to the task's status text.
func (p *ProxmoxProbe) fetchTaskLogTail(node, upid string) string {
	base := fmt.Sprintf("/nodes/%s/tasks/%s/log", node, url.PathEscape(upid))
	var first []pveTaskLogLine
	total, err := p.apiGetTotal(base+"?limit=1", &first)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("node", node).Str("upid", upid).Msg("failed to read task log")
		return ""
	}
	start := total - failedTaskLogLines
	if start < 0 {
		start = 0
	}
	var lines []pveTaskLogLine
	q := url.Values{"start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(failedTaskLogLines)}}
	if err := p.apiGet(base+"?"+q.Encode(), &lines); err != nil {
		p.moduleLogger.Warn().Err(err).Str("node", node).Str("upid", upid).Msg("failed to read task log")
		return ""
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, l.T)
	}
	return strings.Join(out, "\n")
}

func (p *ProxmoxProbe) publishFailedTask(node string, t pveTask, body string) {
	severity, text := agentstate.LogSeverityError, "ERROR"
	if strings.HasPrefix(t.Status, "WARNINGS") {
		severity, text = agentstate.LogSeverityWarn, "WARN"
	}
	attrs := map[string]string{
		"proxmox.node":        node,
		"proxmox.task.type":   t.Type,
		"proxmox.task.upid":   t.UPID,
		"proxmox.task.status": t.Status,
	}
	if _, err := strconv.Atoi(t.ID); err == nil {
		attrs["proxmox.vmid"] = t.ID
	}
	if id := p.entitySrc.InstanceID(); id != "" {
		attrs["service.instance.id"] = id
	}
	agentstate.PublishLog(agentstate.LogRecord{
		Timestamp:         time.Unix(t.EndTime, 0),
		Severity:          severity,
		SeverityText:      text,
		Body:              body,
		Attributes:        attrs,
		ProducerProbeName: p.GetName(),
		ProducerProbeType: ProbeType,
		TargetStrategies:  p.LogTargets(),
	})
}
