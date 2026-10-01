//go:build !windows

package logicaldisk

import "testing"

// Taken from the shape of a host running the agent under a systemd unit with
// ProtectSystem=strict, ReadWritePaths=/var/lib/senhub-agent, PrivateTmp=yes:
// two real filesystems (8:1 root, 8:17 /data) plus bind mounts of 8:1 and 8:17.
const sandboxMountInfo = `22 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw,errors=remount-ro
23 22 0:5 / /proc rw,nosuid,nodev,noexec,relatime shared:2 - proc proc rw
24 22 0:6 / /sys ro,nosuid,nodev,noexec,relatime - sysfs sysfs ro
25 22 0:7 / /dev rw,nosuid shared:3 - devtmpfs udev rw,size=1000k
26 22 0:24 / /run rw,nosuid,nodev,noexec,relatime shared:5 - tmpfs tmpfs rw,size=400000k,mode=755
27 22 8:17 / /data rw,relatime shared:9 - xfs /dev/sdb1 rw,attr2
301 22 8:1 /usr /usr ro,relatime shared:1 - ext4 /dev/sda1 rw,errors=remount-ro
302 22 8:1 /etc /etc ro,relatime shared:1 - ext4 /dev/sda1 rw,errors=remount-ro
303 22 8:1 /etc/senhub-agent /etc/senhub-agent rw,relatime shared:1 - ext4 /dev/sda1 rw,errors=remount-ro
304 22 8:1 /var/lib/senhub-agent /var/lib/senhub-agent rw,relatime shared:1 - ext4 /dev/sda1 rw,errors=remount-ro
305 22 8:1 /tmp/systemd-private-abc-senhub-agent.service-xyz/tmp /tmp rw,relatime shared:1 - ext4 /dev/sda1 rw,errors=remount-ro
306 22 8:17 /backups /srv/backups rw,relatime shared:9 - xfs /dev/sdb1 rw,attr2
307 22 8:1 /home/my\040dir /home/my\040dir rw,relatime shared:1 - ext4 /dev/sda1 rw,errors=remount-ro
`

func collectedFromMountInfo(t *testing.T, content string) []mountInfo {
	t.Helper()
	c := &unixLogicalDiskCollector{}
	var candidates []mountInfo
	for _, m := range parseMountInfo(content) {
		if c.shouldCollectMount(m.fstype, m.mountpoint, m.device) {
			candidates = append(candidates, m)
		}
	}
	return dedupeMountsByDevice(candidates)
}

func TestParseMountInfo_Fields(t *testing.T) {
	mounts := parseMountInfo(sandboxMountInfo)
	if len(mounts) != 13 {
		t.Fatalf("parsed %d mounts, want 13", len(mounts))
	}
	last := mounts[len(mounts)-1]
	if last.mountpoint != "/home/my dir" || last.root != "/home/my dir" {
		t.Errorf("octal escape not decoded: %+v", last)
	}
	if first := mounts[0]; first.device != "/dev/sda1" || first.fstype != "ext4" || first.majMin != "8:1" || first.root != "/" {
		t.Errorf("unexpected first mount: %+v", first)
	}
}

func TestSandboxBindMountsReportedOnce(t *testing.T) {
	got := collectedFromMountInfo(t, sandboxMountInfo)

	points := map[string]bool{}
	for _, m := range got {
		points[m.mountpoint] = true
	}
	// Two real filesystems plus the persistent /run tmpfs the probe always kept.
	for _, want := range []string{"/", "/data", "/run"} {
		if !points[want] {
			t.Errorf("missing %s in %v", want, points)
		}
	}
	if len(got) != 3 {
		t.Errorf("got %d mounts (%v), want 3: two real filesystems and /run", len(got), points)
	}
}

func TestDedupeKeepsShortestWhenNoRootMount(t *testing.T) {
	got := dedupeMountsByDevice([]mountInfo{
		{mountpoint: "/srv/a/deep", majMin: "8:33", root: "/a/deep"},
		{mountpoint: "/srv/a", majMin: "8:33", root: "/a"},
	})
	if len(got) != 1 || got[0].mountpoint != "/srv/a" {
		t.Errorf("got %+v, want the shortest mount point", got)
	}
}

func TestDedupePassesThroughMountsWithoutDeviceNumber(t *testing.T) {
	in := []mountInfo{{mountpoint: "/a"}, {mountpoint: "/b"}}
	if got := dedupeMountsByDevice(in); len(got) != 2 {
		t.Errorf("got %d mounts, want 2", len(got))
	}
}
