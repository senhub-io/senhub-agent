//go:build !windows

package logicaldisk

import (
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/tags"
)

const diskstatsFixture = `   7       0 loop0 100 0 800 10 0 0 0 0 0 10 10 0 0 0 0 0 0
   8       0 sda 5000 10 80000 900 3000 5 40000 700 0 2500 1600 0 0 0 0 0 0
   8       1 sda1 4900 10 79000 890 2900 5 39000 690 0 2400 1580 0 0 0 0 0 0
 259       0 nvme0n1 100 0 2048 5 200 0 4096 9 0 1234 14 0 0 0 0 0 0
 259       1 nvme0n1p1 90 0 1024 4 190 0 2048 8 0 1000 12 0 0 0 0 0 0
 252       0 dm-0 70 0 1000 3 60 0 900 2 0 500 5 0 0 0 0 0 0
   9       0 md0 10 0 80 1 20 0 160 2 0 30 3 0 0 0 0 0 0
   9       1 md0p1 10 0 80 1 20 0 160 2 0 30 3 0 0 0 0 0 0
 254       0 zram0 1 0 8 0 1 0 8 0 0 1 0 0 0 0 0 0 0
   1       0 ram0 1 0 8 0 1 0 8 0 0 1 0 0 0 0 0 0 0
  11       0 sr0 1 0 8 0 1 0 8 0 0 1 0 0 0 0 0 0 0
   2       0 fd0 1 0 8 0 1 0 8 0 0 1 0 0 0 0 0 0 0
 253       0 vda 10 0 160 1 20 0 320 2 0 3000 3 0 0 0 0 0 0
 253       1 vda1 10 0 160 1 20 0 320 2 0 3000 3 0 0 0 0 0 0
 202       0 xvda 10 0 160 1 20 0 320 2 0 10 3 0 0 0 0 0 0
 202       1 xvda1 10 0 160 1 20 0 320 2 0 10 3 0 0 0 0 0 0
 179       0 mmcblk0 10 0 160 1 20 0 320 2 0 10 3 0 0 0 0 0 0
 179       1 mmcblk0p1 10 0 160 1 20 0 320 2 0 10 3 0 0 0 0 0 0
   8      16 sdb 1 2 3
`

func TestShouldReportBlockDevice(t *testing.T) {
	cases := map[string]bool{
		"sda": true, "sdab": true, "vda": true, "xvda": true, "nvme0n1": true,
		"dm-0": true, "md0": true, "mmcblk0": true, "hda": true,
		"sda1": false, "sdab12": false, "vda1": false, "xvda2": false,
		"nvme0n1p1": false, "nvme10n2p3": false, "mmcblk0p2": false, "md0p1": false,
		"loop0": false, "ram0": false, "zram0": false, "fd0": false, "sr0": false,
	}
	for name, want := range cases {
		if got := shouldReportBlockDevice(name); got != want {
			t.Errorf("shouldReportBlockDevice(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestParseDiskstatsKeepsWholeDevicesOnly(t *testing.T) {
	devs, err := parseDiskstats(strings.NewReader(diskstatsFixture))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range devs {
		names = append(names, d.name)
	}
	want := "sda,nvme0n1,dm-0,md0,vda,xvda,mmcblk0"
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("devices = %s, want %s (short lines must be dropped too)", got, want)
	}
	sda := devs[0]
	if sda.readOps != 5000 || sda.writeOps != 3000 {
		t.Errorf("ops = %d/%d", sda.readOps, sda.writeOps)
	}
	if sda.readBytes != 80000*512 || sda.writeBytes != 40000*512 {
		t.Errorf("bytes = %d/%d, want sectors x 512", sda.readBytes, sda.writeBytes)
	}
	if sda.ioTimeMillis != 2500 {
		t.Errorf("io time = %d ms", sda.ioTimeMillis)
	}
}

func TestDiskIOPointsCarryDeviceAndSeconds(t *testing.T) {
	devs, err := parseDiskstats(strings.NewReader(diskstatsFixture))
	if err != nil {
		t.Fatal(err)
	}
	base := []tags.Tag{{Key: "host", Value: "h"}}
	points := diskIOPoints(devs[:1], base, time.Unix(1, 0))
	got := map[string]float64{}
	for _, p := range points {
		got[p.Name] = p.Value
		var device string
		for _, tg := range p.Tags {
			if tg.Key == "device" {
				device = tg.Value
			}
		}
		if device != "sda" {
			t.Errorf("%s: device tag = %q", p.Name, device)
		}
	}
	want := map[string]float64{
		"diskio_read_bytes":   80000 * 512,
		"diskio_write_bytes":  40000 * 512,
		"diskio_read_ops":     5000,
		"diskio_write_ops":    3000,
		"diskio_busy_seconds": 2.5,
	}
	for name, v := range want {
		if got[name] != v {
			t.Errorf("%s = %v, want %v", name, got[name], v)
		}
	}
	if len(base) != 1 {
		t.Errorf("base tags were mutated: %v", base)
	}
}
