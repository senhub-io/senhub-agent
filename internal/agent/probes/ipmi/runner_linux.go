//go:build linux

package ipmi

import (
	"fmt"
	"os/exec"
)

// runIpmitool shells out to the ipmitool binary and returns the raw
// sdr elist full output. On Linux the local mode uses the in-kernel
// OpenIPMI driver (/dev/ipmi0); no extra flags are needed.
//
// If the binary is not found or exits non-zero, the error is returned
// and the caller emits senhub.ipmi.up=0.
func runIpmitool(cfg ipmiConfig) (string, error) {
	var args []string
	if cfg.Mode == "remote" {
		args = append(args,
			"-I", cfg.RemoteIface,
			"-H", cfg.RemoteHost,
			"-U", cfg.RemoteUser,
			"-P", cfg.RemotePassword,
		)
	}
	args = append(args, "sdr", "elist", "full")

	cmd := exec.Command(cfg.IpmitoolPath, args...) // #nosec G204 -- path from operator config
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ipmitool sdr elist full: %w", err)
	}
	return string(out), nil
}
