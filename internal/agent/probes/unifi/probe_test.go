package unifi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func testLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func newTestProbe(t *testing.T, srv *httptest.Server) *unifiProbe {
	t.Helper()
	p, err := NewUnifiProbe(map[string]interface{}{
		"endpoint": srv.URL,
		"username": "admin",
		"password": "secret",
		"site":     "default",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewUnifiProbe: %v", err)
	}
	up := p.(*unifiProbe)
	up.SetName("unifi-test")
	return up
}

// stubController wires up a minimal fake UniFi controller that answers
// /api/login + the three stat endpoints with the provided payloads.
func stubController(t *testing.T, health, devices, clients interface{}) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	serve := func(path string, payload interface{}) {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(payload)
		})
	}
	serve("/api/s/default/stat/health", health)
	serve("/api/s/default/stat/device", devices)
	serve("/api/s/default/stat/sta", clients)
	return httptest.NewServer(mux)
}

func collectByName(t *testing.T, p *unifiProbe) map[string]float64 {
	t.Helper()
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := make(map[string]float64, len(points))
	for _, dp := range points {
		got[dp.Name] = dp.Value
	}
	return got
}

func TestCollect_Up(t *testing.T) {
	srv := stubController(t,
		map[string]interface{}{"data": []interface{}{}},
		map[string]interface{}{"data": []interface{}{}},
		map[string]interface{}{"data": []interface{}{}},
	)
	defer srv.Close()

	p := newTestProbe(t, srv)
	got := collectByName(t, p)
	if got["senhub.unifi.up"] != 1 {
		t.Errorf("senhub.unifi.up = %v; want 1", got["senhub.unifi.up"])
	}
}

func TestCollect_Down_LoginFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := newTestProbe(t, srv)
	got := collectByName(t, p)
	if got["senhub.unifi.up"] != 0 {
		t.Errorf("senhub.unifi.up = %v; want 0", got["senhub.unifi.up"])
	}
}

func TestCollect_DeviceInventory(t *testing.T) {
	devices := map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{"type": "uap", "state": 1, "adopted": true, "name": "AP-1", "num_sta": 5.0, "satisfaction": 82.0, "system-stats": map[string]interface{}{"cpu": "10", "mem": "40"}},
			map[string]interface{}{"type": "uap", "state": 0, "adopted": true, "name": "AP-2", "num_sta": 0.0, "satisfaction": 0.0, "system-stats": map[string]interface{}{"cpu": "5", "mem": "30"}},
			map[string]interface{}{"type": "usw", "state": 1, "adopted": true, "name": "SW-1", "num_sta": 0.0, "satisfaction": 0.0, "system-stats": map[string]interface{}{"cpu": "2", "mem": "20"}},
		},
	}
	srv := stubController(t,
		map[string]interface{}{"data": []interface{}{}},
		devices,
		map[string]interface{}{"data": []interface{}{}},
	)
	defer srv.Close()

	p := newTestProbe(t, srv)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	byNameType := map[string]float64{}
	for _, dp := range points {
		typeVal := ""
		nameVal := ""
		for _, tg := range dp.Tags {
			if tg.Key == "device_type" {
				typeVal = tg.Value
			}
			if tg.Key == "device_name" {
				nameVal = tg.Value
			}
		}
		key := dp.Name + "|" + typeVal + "|" + nameVal
		byNameType[key] = dp.Value
	}

	if v := byNameType["unifi.devices.total|uap|"]; v != 2 {
		t.Errorf("unifi.devices.total[uap] = %v; want 2", v)
	}
	if v := byNameType["unifi.devices.total|usw|"]; v != 1 {
		t.Errorf("unifi.devices.total[usw] = %v; want 1", v)
	}
	if v := byNameType["unifi.devices.disconnected|uap|"]; v != 1 {
		t.Errorf("unifi.devices.disconnected[uap] = %v; want 1", v)
	}
	if v := byNameType["unifi.devices.adopted|uap|"]; v != 2 {
		t.Errorf("unifi.devices.adopted[uap] = %v; want 2", v)
	}
}

func TestCollect_APMetrics(t *testing.T) {
	devices := map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{
				"type": "uap", "state": 1, "adopted": true,
				"name": "MyAP", "num_sta": 12.0, "satisfaction": 90.0,
				"system-stats": map[string]interface{}{"cpu": "25", "mem": "60"},
			},
		},
	}
	srv := stubController(t,
		map[string]interface{}{"data": []interface{}{}},
		devices,
		map[string]interface{}{"data": []interface{}{}},
	)
	defer srv.Close()

	p := newTestProbe(t, srv)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	byName := map[string]float64{}
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}

	if v := byName["unifi.ap.clients"]; v != 12 {
		t.Errorf("unifi.ap.clients = %v; want 12", v)
	}
	if v := byName["unifi.ap.satisfaction"]; v != 0.9 {
		t.Errorf("unifi.ap.satisfaction = %v; want 0.9", v)
	}
	if v := byName["unifi.device.cpu"]; v != 25 {
		t.Errorf("unifi.device.cpu = %v; want 25", v)
	}
	if v := byName["unifi.device.memory"]; v != 60 {
		t.Errorf("unifi.device.memory = %v; want 60", v)
	}
}

func TestCollect_NetworkThroughput(t *testing.T) {
	health := map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{"subsystem": "wan", "tx_bytes-r": 1000000.0, "rx_bytes-r": 2000000.0},
			map[string]interface{}{"subsystem": "lan", "tx_bytes-r": 999.0, "rx_bytes-r": 999.0},
		},
	}
	srv := stubController(t,
		health,
		map[string]interface{}{"data": []interface{}{}},
		map[string]interface{}{"data": []interface{}{}},
	)
	defer srv.Close()

	p := newTestProbe(t, srv)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// unifi.network.io is emitted twice (transmit + receive) under the same
	// metric name; key by name+direction to distinguish them.
	byDir := map[string]float64{}
	for _, dp := range points {
		if dp.Name != "unifi.network.io" {
			continue
		}
		dir := ""
		for _, tg := range dp.Tags {
			if tg.Key == "direction" {
				dir = tg.Value
			}
		}
		byDir[dir] = dp.Value
	}

	if v := byDir["transmit"]; v != 1000000 {
		t.Errorf("unifi.network.io{direction=transmit} = %v; want 1000000", v)
	}
	if v := byDir["receive"]; v != 2000000 {
		t.Errorf("unifi.network.io{direction=receive} = %v; want 2000000", v)
	}
}

func TestCollect_ClientCounts(t *testing.T) {
	clients := map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{"is_wired": false},
			map[string]interface{}{"is_wired": false},
			map[string]interface{}{"is_wired": true},
		},
	}
	srv := stubController(t,
		map[string]interface{}{"data": []interface{}{}},
		map[string]interface{}{"data": []interface{}{}},
		clients,
	)
	defer srv.Close()

	p := newTestProbe(t, srv)
	byName := collectByName(t, p)

	if v := byName["unifi.clients.total"]; v != 3 {
		t.Errorf("unifi.clients.total = %v; want 3", v)
	}
	if v := byName["unifi.clients.wifi"]; v != 2 {
		t.Errorf("unifi.clients.wifi = %v; want 2", v)
	}
}

func TestParseConfig_Errors(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"missing credentials": {},
		"missing password":    {"username": "admin"},
		"missing username":    {"password": "secret"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseConfig(cfg)
			if err == nil {
				t.Fatalf("parseConfig(%v): expected error, got nil", cfg)
			}
		})
	}
}

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"username": "admin",
		"password": "secret",
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Endpoint != defaultEndpoint {
		t.Errorf("default endpoint = %q; want %q", cfg.Endpoint, defaultEndpoint)
	}
	if cfg.Site != defaultSite {
		t.Errorf("default site = %q; want %q", cfg.Site, defaultSite)
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("default interval = %v; want %v", cfg.Interval, defaultInterval)
	}
}

// TestParseConfig_DefaultVerifyTLS is a security regression test: when verify_tls
// is absent from the config, TLS verification must be ON (true), not the Go
// zero-value false. A false default would allow MITM on the credential-bearing
// POST /api/login. (#461)
func TestParseConfig_DefaultVerifyTLS(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"username": "admin",
		"password": "secret",
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if !cfg.VerifyTLS {
		t.Error("default VerifyTLS = false; want true (TLS must be verified unless operator opts out with verify_tls: false)")
	}
}

func TestParseConfig_VerifyTLSOptOut(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"username":   "admin",
		"password":   "secret",
		"verify_tls": false,
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.VerifyTLS {
		t.Error("VerifyTLS = true when verify_tls: false; want false")
	}
}

func TestEntitySource_BeforeFirstCycle(t *testing.T) {
	src := newEntitySource("https://192.0.2.1:8443")
	_, ok := src.Observe()
	if ok {
		t.Error("Observe() = true before first cycle; want false")
	}
}

func TestEntitySource_AfterMarkReachable(t *testing.T) {
	src := newEntitySource("https://192.0.2.1:8443")
	src.markReachable(true)
	obs, ok := src.Observe()
	if !ok {
		t.Error("Observe() = false after markReachable; want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("len(obs.Entities) = %d; want 1", len(obs.Entities))
	}
	ent := obs.Entities[0]
	if ent.Type != entityTypeServiceInstance {
		t.Errorf("entity type = %q; want %q", ent.Type, entityTypeServiceInstance)
	}
	if v, ok := ent.ID[idKeyServiceInstanceID]; !ok || v != "unifi://https://192.0.2.1:8443" {
		t.Errorf("entity id = %v; want {%q: \"unifi://https://192.0.2.1:8443\"}", ent.ID, idKeyServiceInstanceID)
	}
	if v := ent.Attributes["unifi.reachable"]; v != true {
		t.Errorf("unifi.reachable = %v; want true", v)
	}
}
