package docker

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The Docker socket is root:docker 0660, so a hardened non-root agent cannot
// open it — and joining the `docker` group to fix that is root on the host by
// another route, since anyone who can reach the socket can start a container
// that mounts /.
//
// The kernel exposes most of what the probe reports without any of that. A
// container's cgroup carries its CPU, memory, block I/O and process counters,
// and those files are world-readable. This package reads them directly, so the
// probe collects on a default hardened install with no group, no capability and
// no path to root (#797).
//
// What it CANNOT provide, and there is no unprivileged substitute:
//
//   - per-container network counters, which live in the container's network
//     namespace rather than its cgroup — reading them means entering that
//     namespace (CAP_SYS_ADMIN) or asking the daemon;
//   - the restart count, which is daemon bookkeeping;
//   - every name, image, label and state, so containers are identified by id
//     alone.
//
// The socket therefore remains worth having. It is no longer required.

const defaultCgroupRoot = "/sys/fs/cgroup"

// containerIDPattern matches the 64-character hex id Docker uses. Anchored so a
// directory that merely contains a hex run cannot be mistaken for a container.
var containerIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// cgroupReader reads container statistics out of a cgroup v2 hierarchy. root is
// injectable so the parsing is tested against a fixture tree rather than
// whatever the build machine happens to run — the test host is a Mac, and a
// reader that can only be exercised on Linux is a reader nobody exercises.
type cgroupReader struct {
	root string
}

func newCgroupReader(root string) *cgroupReader {
	if root == "" {
		root = defaultCgroupRoot
	}
	return &cgroupReader{root: root}
}

// unifiedAvailable reports whether the root looks like a cgroup v2 (unified)
// hierarchy. cgroup v1 splits controllers into sibling directories and needs a
// different reader; rather than half-read it and publish partial numbers, the
// probe says so and stays on the socket.
func (r *cgroupReader) unifiedAvailable() bool {
	_, err := os.Stat(filepath.Join(r.root, "cgroup.controllers"))
	return err == nil
}

// discover returns the container ids that have a cgroup on this host, with the
// directory each one lives in.
//
// Both cgroup drivers are covered because both are in the field: systemd (the
// default on every modern distribution) nests containers as
// system.slice/docker-<id>.scope, and cgroupfs puts them under docker/<id>.
func (r *cgroupReader) discover() (map[string]string, error) {
	found := make(map[string]string)

	patterns := []struct {
		glob      string
		extractID func(base string) string
	}{
		{
			glob: filepath.Join(r.root, "system.slice", "docker-*.scope"),
			extractID: func(base string) string {
				return strings.TrimSuffix(strings.TrimPrefix(base, "docker-"), ".scope")
			},
		},
		{
			glob:      filepath.Join(r.root, "docker", "*"),
			extractID: func(base string) string { return base },
		},
	}

	for _, p := range patterns {
		matches, err := filepath.Glob(p.glob)
		if err != nil {
			return nil, fmt.Errorf("scanning %s: %w", p.glob, err)
		}
		for _, dir := range matches {
			info, statErr := os.Stat(dir)
			if statErr != nil || !info.IsDir() {
				continue
			}
			id := p.extractID(filepath.Base(dir))
			if !containerIDPattern.MatchString(id) {
				continue
			}
			found[id] = dir
		}
	}
	return found, nil
}

// stats fills the same containerStats the Docker API path produces, from the
// cgroup at dir. Sharing the struct is deliberate: buildDatapoints stays the
// single place metric names are decided, so the two sources cannot drift into
// emitting different names for the same measurement.
//
// Fields with no cgroup equivalent are left zero, and the ones that matter are
// documented at the top of this file rather than silently absent.
func (r *cgroupReader) stats(dir string) (*containerStats, error) {
	var s containerStats

	cpu, err := readKeyedFile(filepath.Join(dir, "cpu.stat"))
	if err != nil {
		return nil, fmt.Errorf("reading cpu.stat: %w", err)
	}
	// cgroup reports microseconds; the Docker API reports nanoseconds, and
	// buildDatapoints publishes the API's unit. Convert here so the two sources
	// are interchangeable downstream.
	const usecToNsec = 1000
	s.CPUStats.CPUUsage.TotalUsage = cpu["usage_usec"] * usecToNsec
	s.CPUStats.CPUUsage.UsageInUsermode = cpu["user_usec"] * usecToNsec
	s.CPUStats.CPUUsage.UsageInKernelmode = cpu["system_usec"] * usecToNsec
	s.CPUStats.ThrottlingData.ThrottlingPeriods = cpu["nr_periods"]
	s.CPUStats.ThrottlingData.ThrottledPeriods = cpu["nr_throttled"]
	s.CPUStats.ThrottlingData.ThrottledTime = cpu["throttled_usec"] * usecToNsec

	if v, err := readUintFile(filepath.Join(dir, "memory.current")); err == nil {
		s.MemoryStats.Usage = v
	}
	// memory.max is the literal string "max" when unlimited. Docker reports the
	// host's total in that case; publishing 0 would read as "limited to
	// nothing", so leave it unset and let the metric be absent instead.
	if v, err := readUintFile(filepath.Join(dir, "memory.max")); err == nil {
		s.MemoryStats.Limit = v
	}
	if mem, err := readKeyedFile(filepath.Join(dir, "memory.stat")); err == nil {
		s.MemoryStats.Stats = mem
	}

	if v, err := readUintFile(filepath.Join(dir, "pids.current")); err == nil {
		s.PidsStats.Current = v
	}
	if v, err := readUintFile(filepath.Join(dir, "pids.max")); err == nil {
		s.PidsStats.Limit = v
	}

	if read, write, err := readIOStat(filepath.Join(dir, "io.stat")); err == nil {
		s.BlkioStats.IOServiceBytesRecursive = []blkioEntry{
			{Op: "read", Value: read},
			{Op: "write", Value: write},
		}
	}

	return &s, nil
}

// readKeyedFile parses the "key value" per line format used by cpu.stat and
// memory.stat. Unparseable lines are skipped rather than failing the file: a
// kernel that adds a field with a different shape must not cost us every other
// counter in it.
func readKeyedFile(path string) (map[string]uint64, error) {
	data, err := os.ReadFile(path) // #nosec G304 - path is built from a validated cgroup dir
	if err != nil {
		return nil, err
	}
	out := make(map[string]uint64)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		v, convErr := strconv.ParseUint(fields[1], 10, 64)
		if convErr != nil {
			continue
		}
		out[fields[0]] = v
	}
	return out, nil
}

// readUintFile reads a single-value cgroup file. The literal "max" means
// unlimited and is reported as an error so the caller leaves the field unset
// rather than publishing a zero that reads as a limit of nothing.
func readUintFile(path string) (uint64, error) {
	data, err := os.ReadFile(path) // #nosec G304 - path is built from a validated cgroup dir
	if err != nil {
		return 0, err
	}
	text := strings.TrimSpace(string(data))
	if text == "max" {
		return 0, fmt.Errorf("%s is unlimited", filepath.Base(path))
	}
	return strconv.ParseUint(text, 10, 64)
}

// readIOStat sums the per-device counters in io.stat into total bytes read and
// written. Format is one line per device:
//
//	8:0 rbytes=0 wbytes=1520070656 rios=0 wios=592840 dbytes=0 dios=0
func readIOStat(path string) (read, write uint64, err error) {
	data, err := os.ReadFile(path) // #nosec G304 - path is built from a validated cgroup dir
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		for _, field := range strings.Fields(line) {
			key, value, found := strings.Cut(field, "=")
			if !found {
				continue // the leading "major:minor" device field
			}
			v, convErr := strconv.ParseUint(value, 10, 64)
			if convErr != nil {
				continue
			}
			switch key {
			case "rbytes":
				read += v
			case "wbytes":
				write += v
			}
		}
	}
	return read, write, nil
}
