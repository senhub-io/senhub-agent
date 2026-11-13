package agent

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteUpdateSummary(t *testing.T) {
	testCases := []struct {
		name        string
		summary     updateSummary
		contains    []string
		notContains []string
		exact       string
	}{
		{
			name: "dry run with update available",
			summary: updateSummary{
				dryRun:  true,
				updated: true,
				current: "0.7.0-rc.5",
				wanted:  "0.6.5",
			},
			contains:    []string{"Dry run: would install 0.6.5 over 0.7.0-rc.5; no change made."},
			notContains: []string{"Update installed", "Restart"},
		},
		{
			name: "dry run already at requested version",
			summary: updateSummary{
				dryRun:  true,
				updated: false,
				current: "0.7.0",
				wanted:  "0.7.0",
			},
			contains:    []string{"no change made"},
			notContains: []string{"Update installed"},
		},
		{
			name: "updated not dry run",
			summary: updateSummary{
				dryRun:      false,
				updated:     true,
				self:        "/usr/local/bin/senhub-agent",
				restartLine: "Restart the agent to use the new version.",
			},
			contains: []string{"Update installed: /usr/local/bin/senhub-agent", "Restart the agent to use the new version."},
		},
		{
			name: "not updated not dry run",
			summary: updateSummary{
				dryRun:  false,
				updated: false,
			},
			exact: "Already up to date.\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			writeUpdateSummary(&buf, tc.summary)
			out := buf.String()
			for _, want := range tc.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output %q does not contain %q", out, want)
				}
			}
			for _, unwanted := range tc.notContains {
				if strings.Contains(out, unwanted) {
					t.Errorf("output %q should not contain %q", out, unwanted)
				}
			}
			if tc.exact != "" && out != tc.exact {
				t.Errorf("output %q != exact %q", out, tc.exact)
			}
		})
	}
}
