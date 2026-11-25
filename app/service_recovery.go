package app

import "time"

// recoveryPolicy is the service recovery every Windows install carries.
type recoveryPolicy struct {
	actions     []recoveryStep // in order
	resetPeriod uint32         // seconds
	nonCrash    bool
}

type recoveryStep struct {
	restart bool
	delay   time.Duration
}

func desiredRecovery() recoveryPolicy {
	return recoveryPolicy{
		actions: []recoveryStep{
			{restart: true, delay: 10 * time.Second},
			{restart: true, delay: 10 * time.Second},
			{restart: true, delay: 10 * time.Second},
		},
		resetPeriod: 86400,
		nonCrash:    true,
	}
}

func recoveryDrift(have, want recoveryPolicy) bool {
	if len(have.actions) != len(want.actions) {
		return true
	}
	for i := range want.actions {
		if have.actions[i] != want.actions[i] {
			return true
		}
	}
	return have.resetPeriod != want.resetPeriod || have.nonCrash != want.nonCrash
}
