package app

import (
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/instanceid"
)

func fakeIdentityDeps(rec *instanceid.IdentityRecord, hostID string, elsewhere *instanceid.ConnectedElsewhere, err error) doctorDeps {
	return doctorDeps{identity: func() (*instanceid.IdentityRecord, string, *instanceid.ConnectedElsewhere, error) {
		return rec, hostID, elsewhere, err
	}}
}

func TestIdentityCheck(t *testing.T) {
	earlier := time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)
	const instA = "11111111-1111-5111-8111-111111111111"

	t.Run("nil dependency skips", func(t *testing.T) {
		if got := identityCheck(doctorDeps{}); len(got) != 0 {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("read error warns", func(t *testing.T) {
		got := identityCheck(fakeIdentityDeps(nil, "", nil, errors.New("boom")))
		if len(got) != 1 || got[0].ID != "install.identity" || got[0].Level != levelWarn {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("host mismatch fails", func(t *testing.T) {
		rec := &instanceid.IdentityRecord{InstanceID: instA, HostID: "host-one", RecordedAt: earlier}
		got := identityCheck(fakeIdentityDeps(rec, "host-two", nil, nil))
		if len(got) != 1 || got[0].ID != "install.identity" || got[0].Level != levelFail {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("fleet refusal fails", func(t *testing.T) {
		rec := &instanceid.IdentityRecord{InstanceID: instA, HostID: "host-one", RecordedAt: earlier}
		ew := &instanceid.ConnectedElsewhere{At: earlier, Detail: "duplicate connection"}
		got := identityCheck(fakeIdentityDeps(rec, "host-one", ew, nil))
		if len(got) != 1 || got[0].ID != "install.identity.fleet" || got[0].Level != levelFail {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("sound identity stays silent", func(t *testing.T) {
		rec := &instanceid.IdentityRecord{InstanceID: instA, HostID: "host-one", RecordedAt: earlier}
		if got := identityCheck(fakeIdentityDeps(rec, "host-one", nil, nil)); len(got) != 0 {
			t.Fatalf("got %+v", got)
		}
	})
}
