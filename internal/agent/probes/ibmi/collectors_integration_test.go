//go:build integration

package ibmi

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/probesdk/datapoint"
)

// TestIntegration_AllCollectors_PUB400 exercises every registered
// collector against real PUB400 through a live JT400 bridge. It
// validates that (a) the SQL is accepted by IBM i 7.5, (b) the Parse
// logic survives the real column layouts, and (c) every collector
// produces at least one DataPoint under normal PUB400 conditions.
//
// Run with:
//
//	PUB400_USER=... PUB400_PASSWORD=... \
//	    go test -tags=integration -v -run TestIntegration_AllCollectors ./internal/probes/ibmi/
func TestIntegration_AllCollectors_PUB400(t *testing.T) {
	user := os.Getenv("PUB400_USER")
	password := os.Getenv("PUB400_PASSWORD")
	if user == "" || password == "" {
		t.Skip("PUB400_USER and PUB400_PASSWORD must be set")
	}

	runnerDir := bridgeRunnerDirFromTest(t)

	br, err := bridge.New(context.Background(), bridge.Config{
		Host:           "pub400.com",
		User:           user,
		Password:       password,
		RunnerDir:      runnerDir,
		StartupTimeout: 20 * time.Second,
		QueryTimeout:   10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatalf("bridge.New: %v", err)
	}
	defer br.Close(context.Background())

	for _, c := range defaultCollectors() {
		t.Run(c.Name(), func(t *testing.T) {
			res, err := br.Query(context.Background(), c.SQL())
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			points, err := c.Parse(res, "pub400.com", time.Now())
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			// Metric collectors must always produce datapoints —
			// PUB400 always has system status, ASPs, subsystems,
			// etc. Event collectors may legitimately return zero
			// points on a quiet cycle: the first SQL seeds their
			// watermark to now() and the initial poll window is
			// essentially empty. We only check query + parse
			// succeed and the shape is right.
			if !c.IsEvent() && len(points) == 0 {
				t.Fatal("metric collector produced zero datapoints against live PUB400")
			}
			t.Logf("collector %s produced %d DataPoints from %d SQL rows (event=%v)",
				c.Name(), len(points), len(res.Rows), c.IsEvent())

			// Sanity: every datapoint must carry the host tag,
			// whether it's a metric or an event.
			for _, dp := range points {
				if !hasHostTag(dp, "pub400.com") {
					t.Errorf("%s: missing host tag in %#v", dp.Name, dp.Tags)
				}
			}

			// Extra sanity for event DataPoints: they must carry
			// severity and message tags so the downstream event
			// strategy's Validate() is happy.
			if c.IsEvent() {
				for _, dp := range points {
					sawSeverity, sawMessage := false, false
					for _, tg := range dp.Tags {
						if tg.Key == "severity" && tg.Value != "" {
							sawSeverity = true
						}
						if tg.Key == "message" && tg.Value != "" {
							sawMessage = true
						}
					}
					if !sawSeverity || !sawMessage {
						t.Errorf("%s: event missing severity/message tags: %#v",
							dp.Name, dp.Tags)
					}
				}
			}
		})
	}
}

func bridgeRunnerDirFromTest(t *testing.T) string {
	t.Helper()
	// runtime.Caller returns the test source file path; the bridge
	// runner assets live at ../bridge relative to this file.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "bridge")
}

func hasHostTag(dp datapoint.DataPoint, want string) bool {
	for _, tg := range dp.Tags {
		if tg.Key == "host" && tg.Value == want {
			return true
		}
	}
	return false
}
