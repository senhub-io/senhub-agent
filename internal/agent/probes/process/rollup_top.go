//go:build linux || windows || darwin

package process

import (
	"sort"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

type nameRollup struct {
	count  int
	cpu    float64
	memory uint64
}

func groupByName(snaps []processSnapshot) map[string]*nameRollup {
	groups := make(map[string]*nameRollup)
	for _, snap := range snaps {
		r, ok := groups[snap.name]
		if !ok {
			r = &nameRollup{}
			groups[snap.name] = r
		}
		r.count++
		r.cpu += snap.cpuPct
		r.memory += snap.rss
	}
	return groups
}

func topNameRollups(groups map[string]*nameRollup, n int, by string) map[string]*nameRollup {
	if n <= 0 || len(groups) <= n {
		return groups
	}

	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}

	sort.Slice(names, func(i, j int) bool {
		ri := groups[names[i]]
		rj := groups[names[j]]
		var keyI, keyJ float64
		if by == topByMemory {
			keyI = float64(ri.memory)
			keyJ = float64(rj.memory)
		} else {
			keyI = ri.cpu
			keyJ = rj.cpu
		}
		if keyI != keyJ {
			return keyI > keyJ
		}
		return names[i] < names[j]
	})

	top := make(map[string]*nameRollup, n)
	for _, name := range names[:n] {
		top[name] = groups[name]
	}
	return top
}

func rollUpPoints(ts time.Time, baseTags []tags.Tag, groups map[string]*nameRollup) []data_store.DataPoint {
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)

	points := make([]data_store.DataPoint, 0, len(names)*3)
	for _, name := range names {
		r := groups[name]
		aggTags := append([]tags.Tag{}, baseTags...)
		aggTags = append(aggTags, tags.Tag{Key: "process.name", Value: name})

		points = append(points,
			data_store.DataPoint{Name: "process.count", Timestamp: ts, Value: float64(r.count), Tags: aggTags},
			data_store.DataPoint{Name: "process.group.cpu.utilization", Timestamp: ts, Value: r.cpu, Tags: aggTags},
			data_store.DataPoint{Name: "process.group.memory.usage", Timestamp: ts, Value: float64(r.memory), Tags: aggTags},
		)
	}
	return points
}

func snapshotPoints(ts time.Time, cfg config, baseTags []tags.Tag, hostname string, snaps []processSnapshot) ([]data_store.DataPoint, []processSnapshot) {
	points := make([]data_store.DataPoint, 0, len(snaps)*7+1)

	if cfg.topN > 0 {
		if cfg.perPID {
			if len(snaps) > cfg.topN {
				snaps = topProcesses(snaps, cfg.topN, cfg.topBy)
			}
			if cfg.detailed() {
				processEntityMode := cfg.byName != nil || cfg.byUser != ""
				for _, snap := range snaps {
					includeHostID := processEntityMode && snap.createTime > 0
					processTags := buildProcessTags(baseTags, snap, hostname, includeHostID)
					points = appendProcessPoints(points, ts, snap, processTags)
				}
			}
			if cfg.aggregate {
				groups := groupByName(snaps)
				points = append(points, rollUpPoints(ts, baseTags, groups)...)
			}
			return points, snaps
		}

		// roll-up mode
		groups := groupByName(snaps)
		topGroups := topNameRollups(groups, cfg.topN, cfg.topBy)
		points = rollUpPoints(ts, baseTags, topGroups)

		// entity source should receive top processes (by individual metric)
		copySnaps := make([]processSnapshot, len(snaps))
		copy(copySnaps, snaps)
		if len(copySnaps) > cfg.topN {
			copySnaps = topProcesses(copySnaps, cfg.topN, cfg.topBy)
		}
		return points, copySnaps
	}

	// topN == 0
	if cfg.detailed() {
		processEntityMode := cfg.byName != nil || cfg.byUser != ""
		for _, snap := range snaps {
			includeHostID := processEntityMode && snap.createTime > 0
			processTags := buildProcessTags(baseTags, snap, hostname, includeHostID)
			points = appendProcessPoints(points, ts, snap, processTags)
		}
	}
	if cfg.aggregate {
		groups := groupByName(snaps)
		points = append(points, rollUpPoints(ts, baseTags, groups)...)
	}
	return points, snaps
}
