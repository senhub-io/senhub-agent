//go:build windows

package auto_update

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows/registry"
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

// applyMsiUpdate downloads the MSI and its detached minisign signature, verifies
// the signature (same key and check as the ZIP archive), stages the MSI in a
// temp file and launches msiexec detached to run the MajorUpgrade. msiexec —
// through the Windows Installer service — stops the running service, installs
// the new version and restarts it; so this returns right after launching and
// the caller must not self-exit. Config under %ProgramData%\SenHub is preserved
// by the MSI (config init is idempotent, and the config lives outside the MSI's
// component set).
func (a *autoUpdate) applyMsiUpdate(msiURL string) error {
	resp, err := a.httpClient.Get(msiURL) // #nosec G107 - HTTPS registry-derived URL
	if err != nil {
		return fmt.Errorf("downloading MSI: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading MSI: HTTP %d %s from %s", resp.StatusCode, resp.Status, msiURL)
	}
	const maxMSISize = 200 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMSISize))
	if err != nil {
		return fmt.Errorf("reading MSI body: %w", err)
	}

	// Verify BEFORE trusting the file — never hand an unverified MSI to msiexec.
	sig, err := a.fetchSignature(msiURL + ".minisig")
	if err != nil {
		return fmt.Errorf("fetching MSI signature: %w", err)
	}
	if err := verifyArchiveSignature(body, sig); err != nil {
		return fmt.Errorf("verifying MSI signature: %w", err)
	}

	tmp := filepath.Join(os.TempDir(), "senhub-agent-update.msi")
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("writing MSI to %s: %w", tmp, err)
	}

	// Detached so the installer survives this service stopping mid-upgrade.
	// /qn silent; the MSI's MajorUpgrade + ServiceControl restart the service.
	logPath := filepath.Join(os.TempDir(), "senhub-agent-update.log")
	cmd := exec.Command("msiexec", "/i", tmp, "/qn", "/l*v", logPath) // #nosec G204 - fixed args, staged path
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launching msiexec: %w", err)
	}

	a.msiUpgradeLaunched = true
	a.logger.Info().
		Str("msi", tmp).
		Str("log", logPath).
		Msg("Launched msiexec for MSI-managed upgrade; the installer restarts the service")
	return nil
}
