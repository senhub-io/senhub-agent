//go:build !linux

package process

import "testing"

func TestNoSecuritySignalsOutsideLinux(t *testing.T) {
	signals, errs := hostSecuritySignals()
	if len(signals) != 0 || len(errs) != 0 {
		t.Errorf("got %v, %v; want nothing", signals, errs)
	}
}
