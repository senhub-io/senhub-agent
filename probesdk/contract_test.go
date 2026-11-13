package probesdk_test

import (
	"context"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	iprobes "senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	iexporthook "senhub-agent.go/internal/agent/services/exporthook"
	idatapoint "senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/probesdk/cliargs"
	sdkexporthook "senhub-agent.go/probesdk/exporthook"
	"senhub-agent.go/probesdk/logger"
	sdkprobes "senhub-agent.go/probesdk/probes"
	sdkspec "senhub-agent.go/probesdk/spec"
)

func TestFakeProbeFileImportsNoInternal(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fakeprobe_test.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parsing fakeprobe_test.go: %v", err)
	}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatalf("unquoting import path: %v", err)
		}
		if strings.Contains(path, "/internal/") {
			t.Fatalf("fakeprobe_test.go imports internal package %q", path)
		}
	}
}

func TestRegistryBuildsTheFakeProbe(t *testing.T) {
	if !sdkprobes.GetRegisteredProbeTypes()["sdkcontract_fake"] {
		t.Fatal("sdkcontract_fake not present in registered probe types")
	}
	names := sdkprobes.RegisteredProbeNames()
	found := false
	for _, name := range names {
		if name == "sdkcontract_fake" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("sdkcontract_fake not present in registered probe names: %v", names)
	}

	decl, ok := sdkspec.For("sdkcontract_fake")
	if !ok {
		t.Fatal("spec.For returned ok=false for sdkcontract_fake")
	}
	if decl.Type != "sdkcontract_fake" {
		t.Fatalf("spec.For returned probe type %q, want %q", decl.Type, "sdkcontract_fake")
	}

	foundSpec := false
	for _, reg := range sdkspec.Registered() {
		if reg.Type == "sdkcontract_fake" {
			foundSpec = true
			break
		}
	}
	if !foundSpec {
		t.Fatal("sdkcontract_fake spec not present in spec.Registered()")
	}
}

func TestProbePollerRunsTheFakeProbe(t *testing.T) {
	t.Setenv("SENHUB_STATE_DIR", t.TempDir())

	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	params := map[string]interface{}{
		"interval":  "50ms",
		"count":     float64(3),
		"url":       "https://example.com",
		"flag":      true,
		"names":     []string{"a", "b"},
		"target_id": "contract-target-1",
	}
	cfg := configuration.ProbeConfig{
		Name:   "contract-1",
		Type:   "sdkcontract_fake",
		Params: params,
	}

	ch := make(chan []idatapoint.DataPoint, 4)
	addFn := data_store.AddCallback(func(points []idatapoint.DataPoint, _ data_store.StrategyRouter) error {
		select {
		case ch <- points:
		default:
		}
		return nil
	})

	poller, err := iprobes.NewProbePoller(cfg, baseLogger, addFn)
	if err != nil {
		t.Fatalf("NewProbePoller: %v", err)
	}

	logCh := agentstate.SubscribeLogs(4)
	defer agentstate.UnsubscribeLogs(logCh)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := poller.Start(ctx); err != nil {
		t.Fatalf("poller.Start: %v", err)
	}

	var delivered []idatapoint.DataPoint
	select {
	case delivered = <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first datapoint delivery")
	}

	select {
	case rec := <-logCh:
		if rec.Body != "sdkcontract fake collect" {
			t.Fatalf("unexpected log body %q", rec.Body)
		}
		if rec.ProducerProbeName != "contract-1" {
			t.Fatalf("unexpected log producer probe name %q", rec.ProducerProbeName)
		}
		if rec.ProducerProbeType != "sdkcontract_fake" {
			t.Fatalf("unexpected log producer probe type %q", rec.ProducerProbeType)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for published log")
	}

	iexporthook.ObserveSeries("sdkcontract-test", []iexporthook.SeriesPoint{{Name: "sdkcontract.point"}}, time.Now())
	iexporthook.SelfRecords(time.Now())
	if got := atomic.LoadInt64(&seriesObserved); got < 1 {
		t.Fatalf("series observer was not called: got count %d", got)
	}
	if got := atomic.LoadInt64(&selfRecordsProvided); got < 1 {
		t.Fatalf("self record provider was not called: got count %d", got)
	}

	if len(delivered) != 2 {
		t.Fatalf("expected 2 datapoints, got %d", len(delivered))
	}
	byName := map[string]idatapoint.DataPoint{}
	for _, dp := range delivered {
		byName[dp.Name] = dp
	}

	bytesDP, ok := byName["sdkcontract.bytes"]
	if !ok {
		t.Fatalf("missing datapoint sdkcontract.bytes: %v", delivered)
	}
	if want := float64(3 * 1024); bytesDP.Value != want {
		t.Fatalf("sdkcontract.bytes value = %v, want %v", bytesDP.Value, want)
	}

	countDP, ok := byName["sdkcontract.count"]
	if !ok {
		t.Fatalf("missing datapoint sdkcontract.count: %v", delivered)
	}
	if want := float64(3); countDP.Value != want {
		t.Fatalf("sdkcontract.count value = %v, want %v", countDP.Value, want)
	}

	for _, dp := range delivered {
		var hasName, hasType bool
		for _, tag := range dp.Tags {
			if tag.Key == "probe_name" && tag.Value == "contract-1" {
				hasName = true
			}
			if tag.Key == "probe_type" && tag.Value == "sdkcontract_fake" {
				hasType = true
			}
		}
		if !hasName || !hasType {
			t.Fatalf("datapoint %q missing poller identity tags: %v", dp.Name, dp.Tags)
		}
	}

	obs, ok := poller.Probe.EntitySource().Observe()
	if !ok {
		t.Fatal("entity source Observe returned ok=false")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	if obs.Entities[0].Type != "sdkcontract.target" {
		t.Fatalf("entity type = %q, want %q", obs.Entities[0].Type, "sdkcontract.target")
	}
	if got := obs.Entities[0].ID["sdkcontract.target.id"]; got != "contract-target-1" {
		t.Fatalf("entity target id = %v, want %q", got, "contract-target-1")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := poller.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("poller.Shutdown: %v", err)
	}
}

func TestBadParamsAreRejectedByTheConstructor(t *testing.T) {
	t.Setenv("SENHUB_STATE_DIR", t.TempDir())

	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	cfg := configuration.ProbeConfig{
		Name: "bad-contract",
		Type: "sdkcontract_fake",
		Params: map[string]interface{}{
			"interval": "50ms",
			"url":      "://invalid",
		},
	}
	addFn := data_store.AddCallback(func(_ []idatapoint.DataPoint, _ data_store.StrategyRouter) error {
		return nil
	})

	poller, err := iprobes.NewProbePoller(cfg, baseLogger, addFn)
	if err == nil {
		t.Fatal("expected error for invalid url parameter")
	}
	if poller != nil {
		t.Fatal("expected nil poller on constructor error")
	}
	if !strings.Contains(err.Error(), "invalid url parameter") {
		t.Fatalf("error %q does not contain %q", err.Error(), "invalid url parameter")
	}
}

func TestExportHookIsReachable(t *testing.T) {
	sdkexporthook.RegisterSeriesObserver(nil)
	sdkexporthook.RegisterSelfRecordProvider(nil)
	if sdkexporthook.Authorized("anything") {
		t.Fatal("expected Authorized to be false without an authorizer installed")
	}
}
