package proxmox

import (
	"encoding/json"
	"testing"
)

func TestBackupCoverage(t *testing.T) {
	raw := func(s string) json.RawMessage {
		return json.RawMessage(s)
	}

	tests := []struct {
		name            string
		jobs            []pveBackupJob
		guests          []pveGuest
		wantEnabled     int
		wantUnprotected int
	}{
		{
			name: "all=1 with exclude",
			jobs: []pveBackupJob{{All: raw("1"), Exclude: "101"}},
			guests: []pveGuest{
				{pveVMListItem: pveVMListItem{VMID: 101}, Pool: "p1"},
				{pveVMListItem: pveVMListItem{VMID: 102}, Pool: "p1"},
			},
			wantEnabled:     1,
			wantUnprotected: 1,
		},
		{
			name: "explicit vmid list",
			jobs: []pveBackupJob{{VMID: "101, 103"}},
			guests: []pveGuest{
				{pveVMListItem: pveVMListItem{VMID: 101}, Pool: "p1"},
				{pveVMListItem: pveVMListItem{VMID: 102}, Pool: "p1"},
				{pveVMListItem: pveVMListItem{VMID: 103}, Pool: "p1"},
			},
			wantEnabled:     1,
			wantUnprotected: 1,
		},
		{
			name: "disabled job covers nothing and is not counted",
			jobs: []pveBackupJob{{Enabled: raw("0"), VMID: "101"}},
			guests: []pveGuest{
				{pveVMListItem: pveVMListItem{VMID: 101}, Pool: "p1"},
			},
			wantEnabled:     0,
			wantUnprotected: 1,
		},
		{
			name: "pool job",
			jobs: []pveBackupJob{{Pool: "p1"}},
			guests: []pveGuest{
				{pveVMListItem: pveVMListItem{VMID: 101}, Pool: "p1"},
				{pveVMListItem: pveVMListItem{VMID: 102}, Pool: "p2"},
			},
			wantEnabled:     1,
			wantUnprotected: 1,
		},
		{
			name: "enabled as string zero and bool false",
			jobs: []pveBackupJob{
				{Enabled: raw(`"0"`), VMID: "101"},
				{Enabled: raw(`false`), VMID: "102"},
			},
			guests: []pveGuest{
				{pveVMListItem: pveVMListItem{VMID: 101}, Pool: "p1"},
				{pveVMListItem: pveVMListItem{VMID: 102}, Pool: "p1"},
			},
			wantEnabled:     0,
			wantUnprotected: 2,
		},
		{
			name: "no jobs means all guests unprotected",
			guests: []pveGuest{
				{pveVMListItem: pveVMListItem{VMID: 101}, Pool: "p1"},
				{pveVMListItem: pveVMListItem{VMID: 102}, Pool: "p2"},
			},
			wantEnabled:     0,
			wantUnprotected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEnabled, gotUnprotected := backupCoverage(tt.jobs, tt.guests)
			if gotEnabled != tt.wantEnabled || gotUnprotected != tt.wantUnprotected {
				t.Fatalf("backupCoverage() = (%d, %d), want (%d, %d)",
					gotEnabled, gotUnprotected, tt.wantEnabled, tt.wantUnprotected)
			}
		})
	}
}
