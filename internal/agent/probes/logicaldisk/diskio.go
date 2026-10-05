//go:build !windows

package logicaldisk

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

// diskstatsSectorBytes is the unit /proc/diskstats counts sectors in,
// whatever the device's physical sector size: the kernel documents it
// as fixed at 512 bytes.
const diskstatsSectorBytes = 512

// blockDeviceStats holds the cumulative counters of one block device.
type blockDeviceStats struct {
	name         string
	readOps      uint64
	writeOps     uint64
	readBytes    uint64
	writeBytes   uint64
	ioTimeMillis uint64
}

var (
	partitionName = regexp.MustCompile(`^((sd|vd|xvd|hd)[a-z]+[0-9]+|(nvme[0-9]+n[0-9]+|mmcblk[0-9]+|md[0-9]+)p[0-9]+)$`)
	// Virtual devices that carry no I/O of their own: loop and ram
	// disks, compressed swap, floppies and optical drives.
	skippedDevicePrefixes = []string{"loop", "ram", "zram", "fd", "sr"}
)

// shouldReportBlockDevice keeps whole devices and drops partitions and
// the virtual devices above. Device-mapper (dm-*) and md arrays are kept:
// they are what a filesystem is mounted on, and the whole-device rule of
// the OTel host metrics receiver is followed rather than second-guessing
// which members are also reported.
func shouldReportBlockDevice(name string) bool {
	for _, p := range skippedDevicePrefixes {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	return !partitionName.MatchString(name)
}

// parseDiskstats reads the content of /proc/diskstats and returns the
// devices worth reporting. Lines too short to carry the I/O time and
// values that do not parse are ignored: a counter that cannot be read is
// better absent than zero.
func parseDiskstats(r io.Reader) ([]blockDeviceStats, error) {
	var out []blockDeviceStats
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		// major minor name reads merged sectors ms writes merged sectors ms inflight io_ms ...
		f := strings.Fields(sc.Text())
		if len(f) < 13 {
			continue
		}
		name := f[2]
		if !shouldReportBlockDevice(name) {
			continue
		}
		var v [5]uint64
		ok := true
		for i, idx := range []int{3, 7, 5, 9, 12} {
			n, err := strconv.ParseUint(f[idx], 10, 64)
			if err != nil {
				ok = false
				break
			}
			v[i] = n
		}
		if !ok {
			continue
		}
		out = append(out, blockDeviceStats{
			name:         name,
			readOps:      v[0],
			writeOps:     v[1],
			readBytes:    v[2] * diskstatsSectorBytes,
			writeBytes:   v[3] * diskstatsSectorBytes,
			ioTimeMillis: v[4],
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading diskstats: %w", err)
	}
	return out, nil
}

// diskIOPoints turns the counters of each device into datapoints. Values
// are cumulative since boot; the I/O time is carried in seconds, the
// unit OTel's system.disk.io_time declares.
func diskIOPoints(devs []blockDeviceStats, base []tags.Tag, ts time.Time) []data_store.DataPoint {
	points := make([]data_store.DataPoint, 0, len(devs)*5)
	for _, d := range devs {
		devTags := append(append([]tags.Tag{}, base...), tags.Tag{Key: "device", Value: d.name})
		for _, m := range []struct {
			name  string
			value float64
		}{
			{"diskio_read_bytes", float64(d.readBytes)},
			{"diskio_write_bytes", float64(d.writeBytes)},
			{"diskio_read_ops", float64(d.readOps)},
			{"diskio_write_ops", float64(d.writeOps)},
			{"diskio_busy_seconds", float64(d.ioTimeMillis) / 1000},
		} {
			points = append(points, data_store.DataPoint{Name: m.name, Timestamp: ts, Value: m.value, Tags: devTags})
		}
	}
	return points
}
