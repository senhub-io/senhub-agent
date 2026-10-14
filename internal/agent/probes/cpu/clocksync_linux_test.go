//go:build linux

package cpu

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestClockSyncFromTimex(t *testing.T) {
	tests := []struct {
		name       string
		state      int
		status     int32
		esterror   int64
		maxerror   int64
		wantSynced bool
		wantEst    time.Duration
		wantMax    time.Duration
	}{
		{
			name:       "synchronised",
			state:      0,
			esterror:   1500,
			maxerror:   500,
			wantSynced: true,
			wantEst:    1500 * time.Microsecond,
			wantMax:    500 * time.Microsecond,
		},
		{
			name:       "unsynced status",
			state:      0,
			status:     int32(unix.STA_UNSYNC),
			esterror:   1500,
			maxerror:   500,
			wantSynced: false,
			wantEst:    1500 * time.Microsecond,
			wantMax:    500 * time.Microsecond,
		},
		{
			name:       "time error",
			state:      unix.TIME_ERROR,
			esterror:   1500,
			maxerror:   500,
			wantSynced: false,
			wantEst:    1500 * time.Microsecond,
			wantMax:    500 * time.Microsecond,
		},
		{
			name:       "time error and unsynced",
			state:      unix.TIME_ERROR,
			status:     int32(unix.STA_UNSYNC),
			esterror:   1500,
			maxerror:   500,
			wantSynced: false,
			wantEst:    1500 * time.Microsecond,
			wantMax:    500 * time.Microsecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := unix.Timex{
				Status:   tt.status,
				Esterror: tt.esterror,
				Maxerror: tt.maxerror,
			}

			gotSynced, gotEst, gotMax := clockSyncFromTimex(tt.state, tx)
			if gotSynced != tt.wantSynced {
				t.Fatalf("synced = %v, want %v", gotSynced, tt.wantSynced)
			}
			if gotEst != tt.wantEst {
				t.Fatalf("estErr = %v, want %v", gotEst, tt.wantEst)
			}
			if gotMax != tt.wantMax {
				t.Fatalf("maxErr = %v, want %v", gotMax, tt.wantMax)
			}
		})
	}
}

func TestDetectClockSource(t *testing.T) {
	root := t.TempDir()
	if got := detectClockSource(root); got != "unknown" {
		t.Fatalf("empty tree source = %q, want unknown", got)
	}

	chronyDir := t.TempDir()
	writeTestFile(t, filepath.Join(chronyDir, "run/chrony"), true)
	if got := detectClockSource(chronyDir); got != "chrony" {
		t.Fatalf("chrony dir source = %q, want chrony", got)
	}

	chronyPID := t.TempDir()
	writeTestFile(t, filepath.Join(chronyPID, "var/run/chrony/chronyd.pid"), false)
	if got := detectClockSource(chronyPID); got != "chrony" {
		t.Fatalf("chrony pid source = %q, want chrony", got)
	}

	ntpd := t.TempDir()
	writeTestFile(t, filepath.Join(ntpd, "var/run/ntpd.pid"), false)
	if got := detectClockSource(ntpd); got != "ntpd" {
		t.Fatalf("ntpd source = %q, want ntpd", got)
	}

	timesyncdFile := t.TempDir()
	writeTestFile(t, filepath.Join(timesyncdFile, "run/systemd/timesync/synchronized"), false)
	if got := detectClockSource(timesyncdFile); got != "timesyncd" {
		t.Fatalf("timesyncd file source = %q, want timesyncd", got)
	}

	timesyncdDir := t.TempDir()
	writeTestFile(t, filepath.Join(timesyncdDir, "run/systemd/timesync"), true)
	if got := detectClockSource(timesyncdDir); got != "timesyncd" {
		t.Fatalf("timesyncd dir source = %q, want timesyncd", got)
	}
}

func writeTestFile(t *testing.T, path string, dir bool) {
	t.Helper()
	if dir {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
}
