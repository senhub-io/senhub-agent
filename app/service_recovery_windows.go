//go:build windows

package app

import (
	"fmt"

	"golang.org/x/sys/windows/svc/mgr"

	"senhub-agent.go/internal/agent/services/logger"
)

// readRecovery snapshots the recovery configuration the SCM currently
// holds for the service, without writing anything.
func readRecovery(s *mgr.Service) (recoveryPolicy, error) {
	actions, err := s.RecoveryActions()
	if err != nil {
		return recoveryPolicy{}, fmt.Errorf("reading recovery actions: %w", err)
	}
	reset, err := s.ResetPeriod()
	if err != nil {
		return recoveryPolicy{}, fmt.Errorf("reading recovery reset period: %w", err)
	}
	nonCrash, err := s.RecoveryActionsOnNonCrashFailures()
	if err != nil {
		return recoveryPolicy{}, fmt.Errorf("reading non-crash recovery flag: %w", err)
	}
	policy := recoveryPolicy{resetPeriod: reset, nonCrash: nonCrash}
	for _, a := range actions {
		policy.actions = append(policy.actions, recoveryStep{
			restart: a.Type == mgr.ServiceRestart,
			delay:   a.Delay,
		})
	}
	return policy, nil
}

func applyServiceRecovery(s *mgr.Service) (changed bool, err error) {
	have, err := readRecovery(s)
	if err != nil {
		return false, err
	}
	want := desiredRecovery()
	if !recoveryDrift(have, want) {
		return false, nil
	}
	actions := make([]mgr.RecoveryAction, 0, len(want.actions))
	for _, step := range want.actions {
		actionType := mgr.NoAction
		if step.restart {
			actionType = mgr.ServiceRestart
		}
		actions = append(actions, mgr.RecoveryAction{Type: actionType, Delay: step.delay})
	}
	if err := s.SetRecoveryActions(actions, want.resetPeriod); err != nil {
		return false, fmt.Errorf("setting recovery actions: %w", err)
	}
	if err := s.SetRecoveryActionsOnNonCrashFailures(want.nonCrash); err != nil {
		return false, fmt.Errorf("setting non-crash recovery flag: %w", err)
	}
	return true, nil
}

// ensureServiceRecovery repairs the recovery actions of the registered
// service without ever failing the run: hosts installed by the MSI or an
// older release carry the wrong policy.
func ensureServiceRecovery(log *logger.Logger) {
	m, err := mgr.Connect()
	if err != nil {
		log.Warn().Err(err).Msg("Could not connect to the service manager to check recovery actions")
		return
	}
	defer m.Disconnect() //nolint:errcheck // best-effort release
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		log.Warn().Err(err).Msg("Could not open the service to check its recovery actions")
		return
	}
	defer s.Close() //nolint:errcheck // best-effort release
	changed, err := applyServiceRecovery(s)
	if err != nil {
		log.Warn().Err(err).Msg("Could not enforce service recovery actions")
		return
	}
	if changed {
		log.Info().Msg("Service recovery set: restart on every failure, including a clean exit with an error")
	}
}
