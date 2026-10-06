//go:build !linux

package app

import "testing"

// Windows and macOS have no unit file and no boot-enable switch to compare:
// the production probes must never turn a registered service into drift.
func TestNonLinuxProbesNeverReportDrift(t *testing.T) {
	if !installUnitCurrent(defaultServiceUser, testServiceArgs) {
		t.Error("installUnitCurrent reported drift outside Linux")
	}
	if !serviceEnabledOnHost() {
		t.Error("serviceEnabledOnHost reported a disabled service outside Linux")
	}
	if changed, err := reconcileInstalledServiceOnHost(defaultServiceUser, testServiceArgs); err != nil || len(changed) != 0 {
		t.Errorf("reconcile outside Linux: changed=%v err=%v", changed, err)
	}
	if !installBinaryCurrent("/any/where/senhub-agent.exe", defaultServiceUser) {
		t.Error("installBinaryCurrent compared binaries outside Linux")
	}
}
