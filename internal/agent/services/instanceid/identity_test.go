package instanceid

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckIdentity(t *testing.T) {
	now := time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)
	earlier := now.Add(-24 * time.Hour)
	const instA = "11111111-1111-5111-8111-111111111111"
	const instB = "22222222-2222-5222-8222-222222222222"

	cases := []struct {
		name       string
		seed       *IdentityRecord // nil: no file; set via raw for the corrupt case
		rawSeed    string
		instanceID string
		hostID     string
		wantStatus IdentityStatus
		wantDisk   *IdentityRecord // nil: the file must be absent or unchanged
	}{
		{
			name: "first start records", instanceID: instA, hostID: "host-one",
			wantStatus: IdentityRecorded, wantDisk: &IdentityRecord{InstanceID: instA, HostID: "host-one", RecordedAt: now},
		},
		{
			name: "same host matches", seed: &IdentityRecord{InstanceID: instA, HostID: "host-one", RecordedAt: earlier},
			instanceID: instA, hostID: "host-one",
			wantStatus: IdentityMatches, wantDisk: &IdentityRecord{InstanceID: instA, HostID: "host-one", RecordedAt: earlier},
		},
		{
			name: "another host is not rewritten", seed: &IdentityRecord{InstanceID: instA, HostID: "host-one", RecordedAt: earlier},
			instanceID: instA, hostID: "host-two",
			wantStatus: IdentityHostChanged, wantDisk: &IdentityRecord{InstanceID: instA, HostID: "host-one", RecordedAt: earlier},
		},
		{
			name: "another instance rekeys", seed: &IdentityRecord{InstanceID: instA, HostID: "host-one", RecordedAt: earlier},
			instanceID: instB, hostID: "host-one",
			wantStatus: IdentityRekeyed, wantDisk: &IdentityRecord{InstanceID: instB, HostID: "host-one", RecordedAt: now},
		},
		{
			name: "unknown host creates nothing", instanceID: instA, hostID: "",
			wantStatus: IdentityUnchecked,
		},
		{
			name: "host case and padding ignored", seed: &IdentityRecord{InstanceID: instA, HostID: "  Host-ONE ", RecordedAt: earlier},
			instanceID: instA, hostID: " host-one ",
			wantStatus: IdentityMatches, wantDisk: &IdentityRecord{InstanceID: instA, HostID: "  Host-ONE ", RecordedAt: earlier},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.rawSeed != "" {
				if err := os.WriteFile(filepath.Join(dir, IdentityFileName), []byte(tc.rawSeed), 0o644); err != nil {
					t.Fatal(err)
				}
			} else if tc.seed != nil {
				if err := writeJSONAtomic(filepath.Join(dir, IdentityFileName), tc.seed); err != nil {
					t.Fatal(err)
				}
			}

			status, prev, err := CheckIdentity(dir, tc.instanceID, tc.hostID, now)
			if tc.rawSeed != "" {
				if err == nil {
					t.Fatal("corrupt record must be an error")
				}
				raw, rerr := os.ReadFile(filepath.Join(dir, IdentityFileName))
				if rerr != nil || string(raw) != tc.rawSeed {
					t.Fatalf("corrupt record was overwritten: %q, %v", raw, rerr)
				}
				return
			}
			if err != nil {
				t.Fatalf("CheckIdentity: %v", err)
			}
			if status != tc.wantStatus {
				t.Fatalf("status = %v, want %v", status, tc.wantStatus)
			}
			if (prev == nil) != (tc.seed == nil) {
				t.Fatalf("previous record = %v, want nil-ness %v", prev, tc.seed == nil)
			}
			if prev != nil && *prev != *tc.seed {
				t.Fatalf("previous record = %+v, want %+v", *prev, *tc.seed)
			}
			disk, rerr := ReadIdentity(dir)
			if rerr != nil {
				t.Fatalf("ReadIdentity: %v", rerr)
			}
			switch {
			case tc.wantDisk == nil:
				if disk != nil {
					t.Fatalf("no file expected, got %+v", *disk)
				}
			case disk == nil:
				t.Fatal("expected a record on disk")
			default:
				if *disk != *tc.wantDisk {
					t.Fatalf("on-disk record = %+v, want %+v", *disk, *tc.wantDisk)
				}
			}
		})
	}

	t.Run("corrupt record errors and is not overwritten", func(t *testing.T) {
		dir := t.TempDir()
		const raw = "{not json"
		if err := os.WriteFile(filepath.Join(dir, IdentityFileName), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err := CheckIdentity(dir, instA, "host-one", now)
		if err == nil {
			t.Fatal("expected an error")
		}
		got, rerr := os.ReadFile(filepath.Join(dir, IdentityFileName))
		if rerr != nil || string(got) != raw {
			t.Fatalf("record changed: %q, %v", got, rerr)
		}
	})

	t.Run("empty instance id is an error", func(t *testing.T) {
		if _, _, err := CheckIdentity(t.TempDir(), "", "host-one", now); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestConnectedElsewhereRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rec, err := ReadConnectedElsewhere(dir)
	if err != nil || rec != nil {
		t.Fatalf("absent marker: %v, %v", rec, err)
	}
	at := time.Date(2024, 6, 2, 8, 30, 0, 0, time.UTC)
	if err := RecordConnectedElsewhere(dir, at, "already connected"); err != nil {
		t.Fatalf("RecordConnectedElsewhere: %v", err)
	}
	got, err := ReadConnectedElsewhere(dir)
	if err != nil {
		t.Fatalf("ReadConnectedElsewhere: %v", err)
	}
	if got == nil || !got.At.Equal(at) || got.Detail != "already connected" {
		t.Fatalf("marker = %+v", got)
	}
	for i := 0; i < 2; i++ {
		if err := ClearConnectedElsewhere(dir); err != nil {
			t.Fatalf("Clear #%d: %v", i+1, err)
		}
	}
	if rec, err := ReadConnectedElsewhere(dir); err != nil || rec != nil {
		t.Fatalf("after clear: %v, %v", rec, err)
	}
}
