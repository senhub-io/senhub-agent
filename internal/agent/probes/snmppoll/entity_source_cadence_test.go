package snmppoll

import (
	"testing"
	"time"
)

func TestSnmpEntitySourceCadence(t *testing.T) {
	t.Run("without discovery", func(t *testing.T) {
		src := newEntitySource(&config{TopologyInterval: 5 * time.Minute}, nil)
		if got := src.Cadence(); got != 5*time.Minute {
			t.Fatalf("Cadence() = %s, want %s", got, 5*time.Minute)
		}
	})

	t.Run("with discovery coarser than topology", func(t *testing.T) {
		src := newEntitySource(&config{
			TopologyInterval: 5 * time.Minute,
			Discovery:        &discoveryConfig{Interval: 15 * time.Minute},
		}, nil)
		if got := src.Cadence(); got != 15*time.Minute {
			t.Fatalf("Cadence() = %s, want %s", got, 15*time.Minute)
		}
	})

	t.Run("with discovery finer than topology", func(t *testing.T) {
		src := newEntitySource(&config{
			TopologyInterval: 15 * time.Minute,
			Discovery:        &discoveryConfig{Interval: 5 * time.Minute},
		}, nil)
		if got := src.Cadence(); got != 15*time.Minute {
			t.Fatalf("Cadence() = %s, want %s", got, 15*time.Minute)
		}
	})
}
