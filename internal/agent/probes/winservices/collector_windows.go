//go:build windows

package winservices

import (
	"fmt"
	"sort"

	"golang.org/x/sys/windows/svc/mgr"
)

// collectServices queries the Service Control Manager for the state of the
// selected services. An empty selection enumerates every service the SCM
// reports. A service that cannot be opened or queried is skipped (best
// effort) rather than failing the whole cycle — a single ACL-restricted
// service must not blind the operator to the rest.
func collectServices(selected []string) ([]serviceState, error) {
	m, err := mgr.Connect()
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
		s, err := m.OpenService(name)
		if err != nil {
			// Selected-but-absent or access-denied: skip silently; the
			// missing row is itself the signal (no datapoint emitted).
			continue
		}
		status, qerr := s.Query()
		_ = s.Close()
		if qerr != nil {
			continue
		}
		out = append(out, serviceState{name: name, state: int(status.State)})
	}
	return out, nil
}
