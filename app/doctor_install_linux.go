//go:build linux

package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

func platformInstallProbe() installProbe {
	return installProbe{
		unit: func() (string, error) {
			data, err := os.ReadFile(loadedUnitPath())
			if err != nil {
				return "", fmt.Errorf("reading %s: %w", loadedUnitPath(), err)
			}
			return string(data), nil
		},
		binaryExists: func(path string) bool {
			info, err := os.Stat(path)
			return err == nil && !info.IsDir()
		},
		mainPID: func() (string, error) {
			out, err := exec.Command("systemctl", "show", "-p", "MainPID", "--value", serviceUnitName).Output()
			if err != nil {
				return "", fmt.Errorf("systemctl show: %w", err)
			}
			return strings.TrimSpace(string(out)), nil
		},
		exeLink: func(pid string) (string, error) {
			target, err := os.Readlink("/proc/" + pid + "/exe")
			if err != nil {
				return "", fmt.Errorf("reading /proc/%s/exe: %w", pid, err)
			}
			return target, nil
		},
		sameContents: sameContents,
		logGroup:     logReaderGroup,
		groupMember:  serviceUserInGroup,
		logDirAccess: hostLogDirAccess,
		serviceBin:   installedServiceBinary,
		binVersion:   binaryVersion,
	}
}

func serviceUserInGroup(name string) (member, groupExists bool, err error) {
	group, err := user.LookupGroup(logReaderGroup)
	if err != nil {
		var unknown user.UnknownGroupError
		if errors.As(err, &unknown) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("looking up group %s: %w", logReaderGroup, err)
	}
	u, err := user.Lookup(name)
	if err != nil {
		return false, true, fmt.Errorf("looking up %s: %w", name, err)
	}
	gids, err := u.GroupIds()
	if err != nil {
		return false, true, fmt.Errorf("reading the groups of %s: %w", name, err)
	}
	return isGroupMember(gids, group.Gid), true, nil
}
