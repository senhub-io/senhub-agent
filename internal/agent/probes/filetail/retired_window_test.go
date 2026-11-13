package filetail

import (
	"testing"
	"time"
)

func TestRetiredWindowByPlatform(t *testing.T) {
	cases := []struct {
		goos  string
		grace time.Duration
		idle  time.Duration
	}{
		{"windows", time.Second, retiredPollInterval},
		{"linux", 30 * time.Second, 0},
		{"darwin", 30 * time.Second, 0},
	}
	for _, c := range cases {
		grace, idle := retiredWindow(c.goos)
		if grace != c.grace || idle != c.idle {
			t.Errorf("retiredWindow(%q) = %v, %v; want %v, %v", c.goos, grace, idle, c.grace, c.idle)
		}
	}
}
