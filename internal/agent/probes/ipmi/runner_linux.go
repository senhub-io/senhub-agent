//go:build linux

package ipmi

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// runIpmitool shells out to the ipmitool binary and returns the raw
// sdr elist full output. On Linux the local mode uses the in-kernel
// OpenIPMI driver (/dev/ipmi0); no extra flags are needed.
//
// In remote mode the password is passed via the IPMITOOL_PASSWORD
// environment variable and ipmitool is invoked with "-E" so that the
// credential never appears on the process argv (which is world-readable
// through /proc/<pid>/cmdline and ps aux).
//
// The subprocess is bounded by cfg.ExecTimeout so that an unreachable
// BMC over LAN cannot stall the poll loop indefinitely.
//
// If the binary is not found or exits non-zero, the error is returned
// and the caller emits senhub.ipmi.up=0.
func runIpmitool(cfg ipmiConfig) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ExecTimeout)
	defer cancel()

	var args []string
	if cfg.Mode == "remote" {
		args = append(args,
			"-I", cfg.RemoteIface,
			"-H", cfg.RemoteHost,
			"-U", cfg.RemoteUser,
			"-E", // read password from IPMITOOL_PASSWORD env var
		)
	}
	args = append(args, "sdr", "elist", "full")

	cmd := exec.CommandContext(ctx, cfg.IpmitoolPath, args...) // #nosec G204 -- path from operator config
	// Guarantee the poll loop is freed even if the killed process leaves a
	// child holding the stdout pipe: once the context fires, force the pipes
	// closed (and Wait to return) WaitDelay later rather than blocking on read.
	cmd.WaitDelay = 2 * time.Second
	if cfg.Mode == "remote" {
		cmd.Env = append(os.Environ(), "IPMITOOL_PASSWORD="+cfg.RemotePassword)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ipmitool sdr elist full: %w", err)
	}
	return string(out), nil
}
