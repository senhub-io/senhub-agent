package zabbix

import (
	"context"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func testLogger() *logger.Logger { return logger.NewLogger(&cliArgs.ParsedArgs{}) }

type fixedDefs struct{ def *transformers.ProbeDefinition }

func (f fixedDefs) GetProbeDefinition(probeType string) *transformers.ProbeDefinition {
	if probeType == "cpu" {
		return f.def
	}
	return nil
}

func cpuPoint(cpu string, value float64) datapoint.DataPoint {
	return datapoint.DataPoint{
		Name: "cpu.usage", Value: value, Timestamp: time.Now(),
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "host-cpu"}, {Key: "probe_type", Value: "cpu"},
			{Key: "cpu", Value: cpu}, {Key: "unit", Value: "%"},
		},
	}
}

func startStrategy(t *testing.T, srv *fakeServer, params configuration.StorageConfigParams) *Strategy {
	t.Helper()
	s := New(params, testLogger(), fixedDefs{cpuDefinition()})
	if err := s.ValidateConfigParams(params); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})
	return s
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestStrategyPushesOnlyTheItemsTheServerAsked(t *testing.T) {
	srv := newFakeServer(t)
	srv.setItems("senhub.system.cpu.utilization[host-cpu,0,user]")
	s := startStrategy(t, srv, configuration.StorageConfigParams{
		"server": srv.addr(), "hostname": "web-01", "interval": "100ms", "refresh_interval": "1h", "heartbeat_interval": "1h",
	})
	if err := s.AddDataPoints([]datapoint.DataPoint{cpuPoint("0", 50), cpuPoint("1", 75)}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a push", func() bool { return len(srv.requestsOf("agent data")) >= 1 })

	req := srv.requestsOf("agent data")[0]
	data := req["data"].([]interface{})
	if len(data) != 1 {
		t.Fatalf("pushed %d values, want only the requested one: %v", len(data), data)
	}
	v := data[0].(map[string]interface{})
	if v["key"] != "senhub.system.cpu.utilization[host-cpu,0,user]" || v["value"] != "0.5" {
		t.Errorf("value = %+v", v)
	}
	if reg := srv.requestsOf("active checks"); len(reg) == 0 || reg[0]["host_metadata"] != metadataWithPlatform("senhub-agent") {
		t.Errorf("the first request must register the host with its metadata: %+v", reg)
	}
}

func TestStrategyStaysQuietUntilTheServerAsksForItems(t *testing.T) {
	srv := newFakeServer(t)
	s := startStrategy(t, srv, configuration.StorageConfigParams{
		"server": srv.addr(), "hostname": "web-01", "interval": "50ms", "refresh_interval": "1h", "heartbeat_interval": "1h",
	})
	_ = s.AddDataPoints([]datapoint.DataPoint{cpuPoint("0", 50)})
	time.Sleep(200 * time.Millisecond)
	if n := len(srv.requestsOf("agent data")); n != 0 {
		t.Fatalf("%d pushes with an empty item list, want none", n)
	}
}

func TestStrategyKeepsOneSessionAcrossPushes(t *testing.T) {
	srv := newFakeServer(t)
	srv.setItems("senhub.system.cpu.utilization[host-cpu,0,user]")
	s := startStrategy(t, srv, configuration.StorageConfigParams{
		"server": srv.addr(), "hostname": "web-01", "interval": "50ms", "refresh_interval": "1h", "heartbeat_interval": "1h",
	})
	_ = s.AddDataPoints([]datapoint.DataPoint{cpuPoint("0", 50)})
	waitFor(t, "two pushes", func() bool { return len(srv.requestsOf("agent data")) >= 2 })
	pushes := srv.requestsOf("agent data")
	if pushes[0]["session"] != pushes[1]["session"] {
		t.Errorf("session changed between pushes: %v / %v", pushes[0]["session"], pushes[1]["session"])
	}
	first := pushes[0]["data"].([]interface{})[0].(map[string]interface{})["id"].(float64)
	second := pushes[1]["data"].([]interface{})[0].(map[string]interface{})["id"].(float64)
	if second <= first {
		t.Errorf("value ids must grow across pushes: %v then %v", first, second)
	}
}

func TestStrategySendsAHeartbeatAndStopsWhenRefused(t *testing.T) {
	srv := newFakeServer(t)
	srv.refuse["active check heartbeat"] = true
	s := startStrategy(t, srv, configuration.StorageConfigParams{
		"server": srv.addr(), "hostname": "web-01", "interval": "1h", "refresh_interval": "1h", "heartbeat_interval": "1s",
	})
	waitFor(t, "a heartbeat", func() bool { return len(srv.requestsOf("active check heartbeat")) >= 1 })
	time.Sleep(1200 * time.Millisecond)
	if n := len(srv.requestsOf("active check heartbeat")); n != 1 {
		t.Errorf("%d heartbeats after a refusal, want the one that was refused", n)
	}
	s.mu.Lock()
	off := s.heartbeatOff
	s.mu.Unlock()
	if !off {
		t.Error("heartbeat should be switched off after the server refused it")
	}
}

func TestStrategyKeepsBeatingWhileTheServerStaysSilent(t *testing.T) {
	srv := newFakeServer(t)
	startStrategy(t, srv, configuration.StorageConfigParams{
		"server": srv.addr(), "hostname": "web-01", "interval": "1h", "refresh_interval": "1h", "heartbeat_interval": "1s",
	})
	waitFor(t, "two heartbeats", func() bool { return len(srv.requestsOf("active check heartbeat")) >= 2 })
}

func TestStrategyWaitsForAutoregistrationThenGetsItsItems(t *testing.T) {
	srv := newFakeServer(t)
	srv.refuseInfo["active checks"] = "host [web-01] not found"
	s := startStrategy(t, srv, configuration.StorageConfigParams{
		"server": srv.addr(), "hostname": "web-01", "interval": "1h", "refresh_interval": "300ms", "heartbeat_interval": "1h",
	})
	waitFor(t, "the first check-list request", func() bool { return len(srv.requestsOf("active checks")) >= 1 })
	s.mu.Lock()
	n := len(s.requested)
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("no item can be requested before the host exists, got %d", n)
	}

	srv.mu.Lock()
	delete(srv.refuseInfo, "active checks")
	srv.mu.Unlock()
	srv.setItems("senhub.system.cpu.utilization[host-cpu,0,user]")
	waitFor(t, "the item list after registration", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.requested) == 1
	})
}

func TestStrategyStartRequiresAValidatedConfiguration(t *testing.T) {
	s := New(configuration.StorageConfigParams{}, testLogger(), nil)
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("start without validation must fail")
	}
}

func TestStoreForgetsASeriesNoLongerCollected(t *testing.T) {
	st := newStore()
	old := cpuPoint("0", 1)
	old.Timestamp = time.Now().Add(-time.Hour)
	st.upsert(old)
	st.upsert(cpuPoint("1", 2))
	if got := st.snapshot(time.Now(), 10*time.Minute); len(got) != 1 || got[0].Tags["cpu"] != "1" {
		t.Errorf("snapshot = %+v", got)
	}
	if st.size() != 1 {
		t.Errorf("size = %d after eviction", st.size())
	}
}

func TestStoreIgnoresAPointWithoutItsProbeIdentity(t *testing.T) {
	st := newStore()
	st.upsert(datapoint.DataPoint{Name: "x", Value: 1})
	if st.size() != 0 {
		t.Error("a point without probe_name/probe_type has no key and is dropped")
	}
	if tagValue([]tags.Tag{{Key: "a", Value: "b"}}, "a") != "b" {
		t.Error("tagValue")
	}
}
