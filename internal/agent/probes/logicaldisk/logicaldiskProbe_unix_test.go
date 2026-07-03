//go:build !windows

package logicaldisk

import "testing"

// TestShouldCollectMount_Blocklist pins the blocklist model: every real on-disk
// filesystem is collected, and only pseudo/network filesystems are skipped. The
// pre-fix code used a whitelist naming just ext*/xfs/vfat/fuse/apfs, so btrfs,
// zfs, f2fs, ntfs, exfat and bcachefs were silently dropped — those cases fail
// on the old code and pass on the new.
func TestShouldCollectMount_Blocklist(t *testing.T) {
	c := &unixLogicalDiskCollector{}

	cases := []struct {
		name       string
		fsType     string
		mountPoint string
		device     string
		want       bool
	}{
		// Real on-disk filesystems the whitelist used to drop.
		{"btrfs", "btrfs", "/", "/dev/sda1", true},
		{"zfs", "zfs", "/data", "tank/data", true},
		{"f2fs", "f2fs", "/mnt/flash", "/dev/mmcblk0p1", true},
		{"ntfs", "ntfs", "/mnt/win", "/dev/sdb1", true},
		{"ntfs3", "ntfs3", "/mnt/win2", "/dev/sdb2", true},
		{"exfat", "exfat", "/mnt/usb", "/dev/sdc1", true},
		{"bcachefs", "bcachefs", "/srv", "/dev/sdd1", true},
		// Real filesystems that already worked — must keep working.
		{"ext4", "ext4", "/", "/dev/sda2", true},
		{"xfs", "xfs", "/var", "/dev/sda3", true},
		// Pseudo filesystems — still skipped.
		{"proc", "proc", "/proc", "proc", false},
		{"sysfs", "sysfs", "/sys", "sysfs", false},
		{"overlay", "overlay", "/var/lib/docker/overlay2/x", "overlay", false},
		{"squashfs", "squashfs", "/snap/core/1", "/dev/loop0", false},
		{"devtmpfs", "devtmpfs", "/dev", "devtmpfs", false},
		{"cgroup2", "cgroup2", "/sys/fs/cgroup", "cgroup2", false},
		// Network filesystems — skipped to avoid a stale-mount statfs hang.
		{"nfs", "nfs4", "/mnt/nfs", "server:/export", false},
		{"cifs", "cifs", "/mnt/share", "//server/share", false},
		// FUSE network/user filesystems — skipped (whole fuse.* family), same
		// stale-mount statfs-hang risk as NFS/CIFS.
		{"fuse.rclone", "fuse.rclone", "/mnt/remote", "rclone", false},
		{"fuse.s3fs", "fuse.s3fs", "/mnt/s3", "s3fs", false},
		{"fuse.gcsfuse", "fuse.gcsfuse", "/mnt/gcs", "gcsfuse", false},
		{"fuse.sshfs", "fuse.sshfs", "/mnt/ssh", "user@host:/", false},
		{"fuse.portal", "fuse.portal", "/run/user/1000/doc", "portal", false},
		// Plain local fuse passthrough stays included.
		{"fuse", "fuse", "/mnt/local-fuse", "/dev/fuse", true},
		// Guard clauses preserved.
		{"empty device", "ext4", "/weird", "", false},
		{"none device", "ext4", "/weird", "none", false},
		// tmpfs allowlist preserved, now prefix-matching /run/user/<uid>.
		{"tmpfs /run", "tmpfs", "/run", "tmpfs", true},
		{"tmpfs /run/user/0", "tmpfs", "/run/user/0", "tmpfs", true},
		{"tmpfs /run/user/1000", "tmpfs", "/run/user/1000", "tmpfs", true},
		{"tmpfs /home", "tmpfs", "/home", "tmpfs", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.shouldCollectMount(tc.fsType, tc.mountPoint, tc.device); got != tc.want {
				t.Errorf("shouldCollectMount(%q, %q, %q) = %v, want %v",
					tc.fsType, tc.mountPoint, tc.device, got, tc.want)
			}
		})
	}
}
