//go:build windows

package winservices

import (
	"fmt"
	"sort"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceConfigTriggerInfo = 8

var (
	advapi32DLL              = syscall.NewLazyDLL("advapi32.dll")
	procQueryServiceConfig2W = advapi32DLL.NewProc("QueryServiceConfig2W")
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
		startDelayed := false
		if cfg, cerr := s.Config(); cerr == nil {
			startType = int(cfg.StartType)
			startDelayed = cfg.DelayedAutoStart
			if startType == int(mgr.StartAutomatic) && cfg.DelayedAutoStart {
				startType = startAutomaticDelayed
			}
		}

		var triggerStart *bool
		if triggered, known := hasServiceTriggers(s); known {
			triggerStart = &triggered
		}

		_ = s.Close()
		if qerr != nil {
			continue
		}
		out = append(out, serviceState{
			name:         name,
			state:        int(status.State),
			startType:    startType,
			startDelayed: startDelayed,
			triggerStart: triggerStart,
		})
	}
	return out, nil
}

// hasServiceTriggers reports whether a service has at least one trigger
// configured. The read is best effort: on error ok is false and the caller
// omits the attribute rather than guessing.
func hasServiceTriggers(s *mgr.Service) (triggered, ok bool) {
	var needed uint32
	buf := make([]byte, 256) // SERVICE_TRIGGER_INFO header; resized when triggers exist

	ret, _, _ := procQueryServiceConfig2W.Call(
		uintptr(s.Handle),
		uintptr(serviceConfigTriggerInfo),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		uintptr(unsafe.Pointer(&needed)),
	)
	if ret == 0 {
		if needed > uint32(len(buf)) {
			buf = make([]byte, needed)
			ret, _, _ = procQueryServiceConfig2W.Call(
				uintptr(s.Handle),
				uintptr(serviceConfigTriggerInfo),
				uintptr(unsafe.Pointer(&buf[0])),
				uintptr(len(buf)),
				uintptr(unsafe.Pointer(&needed)),
			)
		}
		if ret == 0 {
			return false, false
		}
	}
	if len(buf) < 4 {
		return false, false
	}
	cTriggers := *(*uint32)(unsafe.Pointer(&buf[0]))
	return cTriggers > 0, true
}
