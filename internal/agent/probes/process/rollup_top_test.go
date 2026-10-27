//go:build linux || windows || darwin

package process

import (
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

func buildSnap(pid int32, name, owner string, cpuPct float64, rss, vms uint64, threads, fds int32, createTime int64) processSnapshot {
	return processSnapshot{
		pid:        pid,
		name:       name,
		owner:      owner,
		cpuPct:     cpuPct,
		rss:        rss,
		vms:        vms,
		threads:    threads,
		fds:        fds,
		createTime: createTime,
	}
}

func pointIdentity(p data_store.DataPoint) string {
	tagsCopy := make([]tags.Tag, len(p.Tags))
	copy(tagsCopy, p.Tags)
	sort.Slice(tagsCopy, func(i, j int) bool { return tagsCopy[i].Key < tagsCopy[j].Key })
	var sb strings.Builder
	sb.WriteString(p.Name)
	sb.WriteString("|")
	for i, t := range tagsCopy {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(t.Key)
		sb.WriteString("=")
		sb.WriteString(t.Value)
	}
	return sb.String()
}

func identitySet(points []data_store.DataPoint) string {
	ids := make([]string, 0, len(points))
	for _, p := range points {
		ids = append(ids, pointIdentity(p))
	}
	sort.Strings(ids)
	return strings.Join(ids, "\n")
}

func TestTopRollUpIdentityIgnoresPids(t *testing.T) {
	cfg := config{
		topN:      2,
		topBy:     topByCPU,
		aggregate: true,
		perPID:    false,
	}
	baseTags := []tags.Tag{{Key: "host", Value: "h1"}}
	hostname := "h1"
	ts := time.Now()

	snaps1 := []processSnapshot{
		buildSnap(101, "nginx", "root", 50.0, 100, 200, 4, 8, 1111),
		buildSnap(202, "nginx", "root", 40.0, 90, 180, 3, 7, 2222),
		buildSnap(303, "postgres", "db", 80.0, 300, 400, 6, 10, 3333),
	}
	snaps2 := []processSnapshot{
		buildSnap(404, "nginx", "root", 50.0, 100, 200, 4, 8, 4444),
		buildSnap(505, "nginx", "root", 40.0, 90, 180, 3, 7, 5555),
		buildSnap(606, "postgres", "db", 80.0, 300, 400, 6, 10, 6666),
	}

	points1, _ := snapshotPoints(ts, cfg, baseTags, hostname, snaps1)
	points2, _ := snapshotPoints(ts, cfg, baseTags, hostname, snaps2)

	if identitySet(points1) != identitySet(points2) {
		t.Fatalf("identities differ:\n%s\nvs\n%s", identitySet(points1), identitySet(points2))
	}

	allowed := map[string]bool{
		"process.count":                 true,
		"process.group.cpu.utilization": true,
		"process.group.memory.usage":    true,
	}
	forbidden := map[string]bool{
		"process.pid":           true,
		"process.creation.time": true,
		"service.instance.id":   true,
		"process.owner":         true,
	}

	for _, p := range points1 {
		if !allowed[p.Name] {
			t.Errorf("unexpected metric name %q", p.Name)
		}
		for _, tag := range p.Tags {
			if forbidden[tag.Key] {
				t.Errorf("forbidden tag key %q present", tag.Key)
			}
		}
	}
}

func TestTopRollUpKeepsTopNNamesBySummedKey(t *testing.T) {
	baseTags := []tags.Tag{}
	hostname := "h"
	ts := time.Now()

	// CPU: web sum=120 > db=100, topN=1 should keep web
	snapsCPU := []processSnapshot{
		buildSnap(1, "web", "u", 40.0, 100, 100, 1, 1, 1000),
		buildSnap(2, "web", "u", 40.0, 100, 100, 1, 1, 1000),
		buildSnap(3, "web", "u", 40.0, 100, 100, 1, 1, 1000),
		buildSnap(4, "db", "u", 100.0, 200, 200, 2, 2, 1000),
	}
	cfgCPU := config{topN: 1, topBy: topByCPU, aggregate: true, perPID: false}
	pointsCPU, _ := snapshotPoints(ts, cfgCPU, baseTags, hostname, snapsCPU)

	foundCPU := false
	for _, p := range pointsCPU {
		if p.Name == "process.count" {
			for _, tag := range p.Tags {
				if tag.Key == "process.name" && tag.Value == "web" {
					if p.Value != 3.0 {
						t.Errorf("process.count for web = %v, want 3", p.Value)
					}
					foundCPU = true
					break
				}
			}
		}
	}
	if !foundCPU {
		t.Error("roll-up point for web (CPU) not found")
	}

	// Memory: memA sum=1000 > memB=900, topN=1 should keep memA
	snapsMem := []processSnapshot{
		buildSnap(5, "memA", "u", 10.0, 500, 500, 1, 1, 1000),
		buildSnap(6, "memA", "u", 10.0, 500, 500, 1, 1, 1000),
		buildSnap(7, "memB", "u", 20.0, 900, 900, 1, 1, 1000),
	}
	cfgMem := config{topN: 1, topBy: topByMemory, aggregate: true, perPID: false}
	pointsMem, _ := snapshotPoints(ts, cfgMem, baseTags, hostname, snapsMem)

	foundMem := false
	for _, p := range pointsMem {
		if p.Name == "process.count" {
			for _, tag := range p.Tags {
				if tag.Key == "process.name" && tag.Value == "memA" {
					if p.Value != 2.0 {
						t.Errorf("process.count for memA = %v, want 2", p.Value)
					}
					foundMem = true
					break
				}
			}
		}
	}
	if !foundMem {
		t.Error("roll-up point for memA (memory) not found")
	}
}

func TestPerPIDKeepsPidTags(t *testing.T) {
	cfg := config{
		topN:      2,
		perPID:    true,
		aggregate: false,
	}
	baseTags := []tags.Tag{}
	hostname := "h"
	ts := time.Now()

	snaps := []processSnapshot{
		buildSnap(10, "app", "u", 30.0, 100, 100, 1, 1, 1234),
		buildSnap(11, "app", "u", 25.0, 90, 90, 1, 1, 1234),
		buildSnap(12, "db", "u", 80.0, 300, 300, 2, 2, 1234),
	}

	points, _ := snapshotPoints(ts, cfg, baseTags, hostname, snaps)

	found := false
	for _, p := range points {
		if p.Name == "process.cpu.utilization" {
			hasPID := false
			hasCreate := false
			for _, tag := range p.Tags {
				if tag.Key == "process.pid" {
					hasPID = true
				}
				if tag.Key == "process.creation.time" {
					hasCreate = true
				}
			}
			if hasPID && hasCreate {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("process.cpu.utilization point missing process.pid and/or process.creation.time tags")
	}
}

func TestByNameWithoutTopNIsUnchanged(t *testing.T) {
	cfg := config{
		byName:    regexp.MustCompile("^nginx"),
		topN:      0,
		aggregate: true,
		perPID:    false,
	}
	baseTags := []tags.Tag{}
	hostname := "h"
	ts := time.Now()

	snaps := []processSnapshot{
		buildSnap(20, "nginx", "u", 10.0, 100, 100, 1, 1, 1000),
		buildSnap(21, "nginx", "u", 20.0, 200, 200, 2, 2, 1000),
		buildSnap(22, "nginx-worker", "u", 30.0, 300, 300, 3, 3, 1000),
	}

	points, returnedSnaps := snapshotPoints(ts, cfg, baseTags, hostname, snaps)

	if len(returnedSnaps) != 3 {
		t.Fatalf("returned snaps length = %d, want 3", len(returnedSnaps))
	}

	hasPerProcess := false
	hasRollup := false
	for _, p := range points {
		hasPIDTag := false
		for _, tag := range p.Tags {
			if tag.Key == "process.pid" {
				hasPIDTag = true
				break
			}
		}
		if hasPIDTag {
			hasPerProcess = true
		} else {
			// Roll-up points have no process.pid, but may have process.name
			for _, tag := range p.Tags {
				if tag.Key == "process.name" {
					hasRollup = true
					break
				}
			}
		}
	}
	if !hasPerProcess {
		t.Error("expected per-process detail points with process.pid tag")
	}
	if !hasRollup {
		t.Error("expected aggregate roll-up points")
	}
}

func TestTopRollUpReturnsTopProcessesForEntities(t *testing.T) {
	cfg := config{
		topN:      2,
		perPID:    false,
		aggregate: false,
	}
	baseTags := []tags.Tag{}
	hostname := "h"
	ts := time.Now()

	snaps := []processSnapshot{
		buildSnap(1, "a", "u", 50.0, 100, 100, 1, 1, 1000),
		buildSnap(2, "b", "u", 40.0, 90, 90, 1, 1, 1000),
		buildSnap(3, "c", "u", 30.0, 80, 80, 1, 1, 1000),
		buildSnap(4, "d", "u", 20.0, 70, 70, 1, 1, 1000),
		buildSnap(5, "e", "u", 10.0, 60, 60, 1, 1, 1000),
	}

	_, returnedSnaps := snapshotPoints(ts, cfg, baseTags, hostname, snaps)

	if len(returnedSnaps) != cfg.topN {
		t.Fatalf("returned snaps length = %d, want %d", len(returnedSnaps), cfg.topN)
	}
}
