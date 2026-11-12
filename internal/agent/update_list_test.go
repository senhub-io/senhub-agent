package agent

import (
	"io"
	"os"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/auto_update"
	"senhub-agent.go/internal/agent/services/logger"
)

type listStub struct{ stubUpdater }

func (listStub) ListAvailableVersions(includeBeta bool) ([]auto_update.VersionMetadata, error) {
	if !includeBeta {
		return []auto_update.VersionMetadata{{Name: "latest", Version: "0.6.5"}, {Name: "0.6.5", Version: "0.6.5"}, {Name: "0.6.4", Version: "0.6.4"}}, nil
	}
	return []auto_update.VersionMetadata{{Name: "latest-beta", Version: "0.7.0-rc.5"}, {Name: "0.7.0-rc.5", Version: "0.7.0-rc.5"}, {Name: "0.7.0-rc.4", Version: "0.7.0-rc.4"}, {Name: "0.6.5", Version: "0.6.5"}}, nil
}

func TestListVersionsSkipsRegistryAliases(t *testing.T) {
	oldVersion := cliArgs.Version
	t.Cleanup(func() { cliArgs.Version = oldVersion })
	cliArgs.Version = "0.7.0-rc.3"

	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	listVersions(listStub{}, true, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	w.Close()
	os.Stdout = oldStdout
	outBytes, _ := io.ReadAll(r)
	out := string(outBytes)

	if strings.Count(out, "0.7.0-rc.5") != 1 || strings.Count(out, "0.6.5") != 1 || !strings.Contains(out, "0.7.0-rc.4") || !strings.Contains(out, "0.6.4") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}
