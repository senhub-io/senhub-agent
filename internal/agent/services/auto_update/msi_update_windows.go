//go:build windows

package auto_update

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// msiRegistryKey is the marker the MSI writes (see packaging/windows/
// senhub-agent.wxs). Its presence means the install is MSI-managed.
const msiRegistryKey = `SOFTWARE\Sensor Factory\SenHub Agent`

// Windows process-creation flags to launch msiexec so it survives this service
// being stopped mid-upgrade.
const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

// MSI transport sanity bounds, mirroring the ZIP path (doUpdate): an MSI weighs
// tens of MB, so a body under 1 MB is a landing/error page, and 200 MB caps a
// runaway or truncated transfer.
const (
	maxMSISize = 200 * 1024 * 1024
	minMSISize = 1 * 1024 * 1024
)

// stagedUpdateMaxAge bounds how long an abandoned staging directory may linger
// before the next run sweeps it. A day-old staged MSI is dead weight: msiexec
// caches the running install, so nothing reads it back.
const stagedUpdateMaxAge = 24 * time.Hour

// isMsiManaged reports whether this install was created by the Windows MSI,
// detected via the registry marker. Only then does the agent update by applying
// a new MSI instead of self-replacing its binary, so Windows Installer tracking
// (repair, upgrade, ARP version) stays coherent.
func isMsiManaged() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, msiRegistryKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Version")
	return err == nil && v != ""
}

// updateStagingBaseDir returns %ProgramData%\SenHub\update — the application-
// owned base under which MSI updates are staged, matching where config
// (%ProgramData%\SenHub) and logs (%ProgramData%\SenHub\logs) already live.
func updateStagingBaseDir() string {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return filepath.Join(programData, "SenHub", "update")
}

// msiexecPath resolves msiexec from System32 rather than the process PATH: under
// LocalSystem the PATH is machine-controlled, but resolving an absolute path
// removes any dependency on a writable directory wrongly present in it (m5).
func msiexecPath() string {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	return filepath.Join(systemRoot, "System32", "msiexec.exe")
}

// applyMsiUpdate downloads the MSI and its detached minisign signature, verifies
// the signature (same key and check as the ZIP archive), stages the MSI in a
// private per-run directory and launches msiexec detached to run the
// MajorUpgrade. msiexec — through the Windows Installer service — stops the
// running service, installs the new version and restarts it; so this returns
// right after launching and the caller must not self-exit. Config under
// %ProgramData%\SenHub is preserved by the MSI (config init is idempotent, and
// the config lives outside the MSI's component set).
func (a *autoUpdate) applyMsiUpdate(msiURL, version string) error {
	if a.dryRun {
		a.logger.Info().Str("msi_url", msiURL).Msg("Dry run: skipping MSI update")
		return nil
	}

	// Defense-in-depth parity with the ZIP path: signature verification below is
	// the real gate, but never even fetch an installer over plaintext HTTP from
	// a config-settable registry URL (m1).
	if !strings.HasPrefix(msiURL, "https://") {
		return fmt.Errorf("unsafe URL scheme - only HTTPS URLs are allowed: %s", msiURL)
	}

	// Sweep abandoned stagings from prior runs before we add a new one, so a
	// persistently failing install cannot grow the disk unboundedly (M2).
	sweepStagedUpdates(updateStagingBaseDir(), stagedUpdateMaxAge, a.logger)

	resp, err := a.httpClient.Get(msiURL) // #nosec G107 - URL is validated for HTTPS scheme above
	if err != nil {
		return fmt.Errorf("downloading MSI: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading MSI: HTTP %d %s from %s", resp.StatusCode, resp.Status, msiURL)
	}

	// Reject an error/landing page early: the release server answers unknown
	// paths (e.g. a beta MSI that was never built) with a 200 HTML body, which
	// would otherwise surface later as a misleading signature failure (m2).
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/json") {
		return fmt.Errorf("invalid content type for MSI download: %s (expected msi/octet-stream, from %s)",
			contentType, msiURL)
	}

	// Read max+1 so a body pinned exactly at the cap is detected as oversized
	// rather than silently truncated (m2).
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMSISize+1))
	if err != nil {
		return fmt.Errorf("reading MSI body: %w", err)
	}
	if int64(len(body)) > maxMSISize {
		return fmt.Errorf("MSI too large (> %d bytes)", maxMSISize)
	}
	if len(body) < minMSISize {
		return fmt.Errorf("MSI too small: %d bytes (expected at least %d)", len(body), minMSISize)
	}

	// Verify BEFORE trusting the file — never hand an unverified MSI to msiexec.
	// Feed the same update_rejected self-metric the ZIP path uses so a
	// mis-published or tampered MSI is visible to fleet monitoring (m3, #266).
	sig, err := a.fetchSignature(msiURL + ".minisig")
	if err != nil {
		agentstate.IncrementUpdateRejected("signature_unavailable")
		return fmt.Errorf("update REJECTED — MSI signature unavailable: %w", err)
	}
	if err := verifyArchiveSignature(body, sig); err != nil {
		agentstate.IncrementUpdateRejected("signature_invalid")
		return fmt.Errorf("update REJECTED — verifying MSI signature: %w", err)
	}

	// Stage under an application-owned directory with an unpredictable per-run
	// name, never the shared temp dir: msiexec re-reads this file from disk while
	// elevated, so a fixed path in a world-writable location is a TOCTOU handle a
	// local attacker could use to swap in an unverified MSI after the check above.
	stageDir, err := secureStageDir(updateStagingBaseDir())
	if err != nil {
		return fmt.Errorf("staging MSI update: %w", err)
	}
	// Purge the staging dir on every failure after it is created; only a
	// successful launch keeps it (msiexec reads it back detached). Without this a
	// failing writeStagedFile/launch leaks tens of MB each cycle (M2).
	launched := false
	defer func() {
		if !launched {
			if rmErr := os.RemoveAll(stageDir); rmErr != nil {
				a.logger.Warn().Err(rmErr).Str("dir", stageDir).Msg("Failed to clean up staging dir")
			}
		}
	}()

	tmp, err := writeStagedFile(stageDir, "senhub-agent-update.msi", body)
	if err != nil {
		return fmt.Errorf("staging MSI: %w", err)
	}

	// Detached so the installer survives this service stopping mid-upgrade.
	// /qn silent; the MSI's MajorUpgrade + ServiceControl restart the service.
	logPath := filepath.Join(stageDir, "senhub-agent-update.log")
	if err := a.launchMsiInstaller(tmp, logPath); err != nil {
		return fmt.Errorf("launching msiexec: %w", err)
	}
	launched = true

	a.msiUpgradeLaunched.Store(true)
	a.lastMsiAttempt.Store(&msiAttempt{version: version, logPath: logPath})
	a.logger.Info().
		Str("msi", tmp).
		Str("log", logPath).
		Str("version", version).
		Msg("Launched msiexec for MSI-managed upgrade; the installer restarts the service")
	return nil
}

// launchMsiInstaller starts the staged MSI. Tests inject a.launchInstaller to
// assert orchestration without spawning a process; production launches msiexec
// detached from an absolute System32 path.
func (a *autoUpdate) launchMsiInstaller(msiPath, logPath string) error {
	if a.launchInstaller != nil {
		return a.launchInstaller(msiPath, logPath)
	}
	cmd := exec.Command(msiexecPath(), "/i", msiPath, "/qn", "/l*v", logPath) // #nosec G204 - fixed args, staged path, absolute msiexec
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
	return cmd.Start()
}
