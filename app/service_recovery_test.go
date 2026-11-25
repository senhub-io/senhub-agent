package app

import (
	"testing"
	"time"
)

func TestRecoveryDrift(t *testing.T) {
	tests := []struct {
		name string
		have recoveryPolicy
		want bool
	}{
		{
			name: "identical policy",
			have: recoveryPolicy{
				actions: []recoveryStep{
					{restart: true, delay: 10 * time.Second},
					{restart: true, delay: 10 * time.Second},
					{restart: true, delay: 10 * time.Second},
				},
				resetPeriod: 86400,
				nonCrash:    true,
			},
			want: false,
		},
		{
			name: "one action fewer",
			have: recoveryPolicy{
				actions: []recoveryStep{
					{restart: true, delay: 10 * time.Second},
					{restart: true, delay: 10 * time.Second},
				},
				resetPeriod: 86400,
				nonCrash:    true,
			},
			want: true,
		},
		{
			name: "different delay",
			have: recoveryPolicy{
				actions: []recoveryStep{
					{restart: true, delay: time.Second},
					{restart: true, delay: 10 * time.Second},
					{restart: true, delay: 10 * time.Second},
				},
				resetPeriod: 86400,
				nonCrash:    true,
			},
			want: true,
		},
		{
			name: "none action",
			have: recoveryPolicy{
				actions: []recoveryStep{
					{restart: false, delay: 10 * time.Second},
					{restart: true, delay: 10 * time.Second},
					{restart: true, delay: 10 * time.Second},
				},
				resetPeriod: 86400,
				nonCrash:    true,
			},
			want: true,
		},
		{
			name: "different reset period",
			have: recoveryPolicy{
				actions: []recoveryStep{
					{restart: true, delay: 10 * time.Second},
					{restart: true, delay: 10 * time.Second},
					{restart: true, delay: 10 * time.Second},
				},
				resetPeriod: 10,
				nonCrash:    true,
			},
			want: true,
		},
		{
			name: "non-crash flag false",
			have: recoveryPolicy{
				actions: []recoveryStep{
					{restart: true, delay: 10 * time.Second},
					{restart: true, delay: 10 * time.Second},
					{restart: true, delay: 10 * time.Second},
				},
				resetPeriod: 86400,
				nonCrash:    false,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recoveryDrift(tt.have, desiredRecovery()); got != tt.want {
				t.Errorf("recoveryDrift() = %v, want %v", got, tt.want)
			}
		})
	}
}
