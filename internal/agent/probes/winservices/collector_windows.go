//go:build windows

package winservices

import (
	"fmt"
	"sort"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

// The probe only reads. mgr.Connect and mgr.OpenService ask for full access,
// which the SCM refuses on protected services (WinDefend, BFE, RpcSs,
// DcomLaunch...) even to LocalSystem, and those are the services whose stop
// matters most.
const (
	scmReadAccess     = windows.SC_MANAGER_CONNECT | windows.SC_MANAGER_ENUMERATE_SERVICE
	serviceReadAccess = windows.SERVICE_QUERY_STATUS | windows.SERVICE_QUERY_CONFIG
)

// connectReadOnly opens the local SCM with read rights only.
func connectReadOnly() (*mgr.Mgr, error) {
	h, err := windows.OpenSCManager(nil, nil, scmReadAccess)
	if err != nil {
		return nil, err
	}
	return &mgr.Mgr{Handle: h}, nil
}

// openServiceReadOnly opens a service with query rights only.
func openServiceReadOnly(m *mgr.Mgr, name string) (*mgr.Service, error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.OpenService(m.Handle, namePtr, serviceReadAccess)
	if err != nil {
		return nil, err
	}
	return &mgr.Service{Name: name, Handle: h}, nil
}

// collectServices queries the Service Control Manager for the state of the
// selected services. An empty selection enumerates every service the SCM
// reports. A service that cannot be opened or queried is skipped (best
// effort) rather than failing the whole cycle — a single ACL-restricted
// service must not blind the operator to the rest.
func collectServices(selected []string) ([]serviceState, error) {
	m, err := connectReadOnly()
	if err != nil {
		return nil, fmt.Errorf("connecting to service control manager: %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	names := selected
	if len(names) == 0 {
		names, err = m.ListServices()
		if err != nil {
			return nil, fmt.Errorf("listing services: %w", err)
		}
		sort.Strings(names)
	}

	out := make([]serviceState, 0, len(names))
	for _, name := range names {
		s, err := openServiceReadOnly(m, name)
		if err != nil {
			// Selected-but-absent or access-denied: skip silently; the
			// missing row is itself the signal (no datapoint emitted).
			continue
		}
		status, qerr := s.Query()
		startType := -1
		if cfg, cerr := s.Config(); cerr == nil {
			startType = int(cfg.StartType)
			if startType == int(mgr.StartAutomatic) && cfg.DelayedAutoStart {
				startType = startAutomaticDelayed
			}
		}
		_ = s.Close()
		if qerr != nil {
			continue
		}
		out = append(out, serviceState{name: name, state: int(status.State), startType: startType})
	}
	return out, nil
}
