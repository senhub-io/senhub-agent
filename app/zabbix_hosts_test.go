package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
)

type testTemplate struct {
	TemplateID string `json:"templateid"`
	Host       string `json:"host"`
}

type testHost struct {
	HostID          string         `json:"hostid"`
	Host            string         `json:"host"`
	ParentTemplates []testTemplate `json:"parentTemplates"`
}

type hostUpdateRequest struct {
	HostID    string `json:"hostid"`
	Templates []struct {
		TemplateID string `json:"templateid"`
	} `json:"templates"`
}

type rpcRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	ID     int             `json:"id"`
}

func startFakeZabbix(t *testing.T, hosts []testHost, updateReq *[]hostUpdateRequest) (*httptest.Server, *zabbixAPI) {
	t.Helper()
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var result interface{}
		switch req.Method {
		case "host.get":
			result = hosts
		case "host.update":
			var params hostUpdateRequest
			if err := json.Unmarshal(req.Params, &params); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			*updateReq = append(*updateReq, params)
			mu.Unlock()
			result = map[string]interface{}{}
		default:
			result = map[string]interface{}{}
		}
		resp := map[string]interface{}{
			"jsonrpc": "2.0",
			"result":  result,
			"id":      req.ID,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encoder error: %v", err)
		}
	})
	server := httptest.NewServer(mux)
	api := newZabbixAPI(server.URL, "token")
	return server, api
}

func TestResolveHostsPlatforms(t *testing.T) {
	hosts := []testHost{
		{
			HostID: "1001",
			Host:   "web-01",
			ParentTemplates: []testTemplate{
				{"1", "SenHub CPU Metrics linux"},
				{"2", "SenHub Agent"},
			},
		},
		{
			HostID: "1002",
			Host:   "win-01",
			ParentTemplates: []testTemplate{
				{"3", "SenHub CPU Metrics windows"},
			},
		},
	}
	server, api := startFakeZabbix(t, hosts, nil)
	defer server.Close()

	s := &zabbixSetup{api: api, out: &bytes.Buffer{}}
	err := s.resolveHosts([]string{"web-01", "win-01"})
	if err != nil {
		t.Fatalf("resolveHosts returned error: %v", err)
	}
	if len(s.hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(s.hosts))
	}
	if s.hosts[0].Name != "web-01" || s.hosts[0].Platform != "linux" {
		t.Errorf("web-01 platform = %q, want linux; host = %q", s.hosts[0].Platform, s.hosts[0].Name)
	}
	if s.hosts[1].Name != "win-01" || s.hosts[1].Platform != "windows" {
		t.Errorf("win-01 platform = %q, want windows; host = %q", s.hosts[1].Platform, s.hosts[1].Name)
	}
}

func TestResolveHostsUnknown(t *testing.T) {
	server, api := startFakeZabbix(t, nil, nil)
	defer server.Close()

	s := &zabbixSetup{api: api, out: &bytes.Buffer{}}
	err := s.resolveHosts([]string{"unknown"})
	if err == nil {
		t.Fatal("expected error for unknown host, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte(`no host named "unknown"`)) {
		t.Errorf("error does not contain expected text: %v", err)
	}
}

func TestResolveHostsNoPlatform(t *testing.T) {
	hosts := []testHost{
		{
			HostID: "1001",
			Host:   "bad-host",
			ParentTemplates: []testTemplate{
				{"1", "SenHub Agent"},
			},
		},
	}
	server, api := startFakeZabbix(t, hosts, nil)
	defer server.Close()

	s := &zabbixSetup{api: api, out: &bytes.Buffer{}}
	err := s.resolveHosts([]string{"bad-host"})
	if err == nil {
		t.Fatal("expected error for host with no platform template, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte(`cannot tell the platform`)) {
		t.Errorf("error does not contain expected text: %v", err)
	}
}

func TestLinkExistingHostsAddsMissing(t *testing.T) {
	hosts := []testHost{
		{
			HostID: "1001",
			Host:   "web-01",
			ParentTemplates: []testTemplate{
				{"1", "SenHub CPU Metrics linux"},
				{"2", "SenHub Agent"},
			},
		},
		{
			HostID: "1002",
			Host:   "win-01",
			ParentTemplates: []testTemplate{
				{"3", "SenHub CPU Metrics windows"},
			},
		},
	}
	var updates []hostUpdateRequest
	server, api := startFakeZabbix(t, hosts, &updates)
	defer server.Close()

	s := &zabbixSetup{api: api, out: &bytes.Buffer{}, templateByName: map[string]string{"SenHub PostgreSQL linux": "9"}}
	err := s.resolveHosts([]string{"web-01", "win-01"})
	if err != nil {
		t.Fatalf("resolveHosts error: %v", err)
	}
	err = s.linkExistingHosts("linux", []string{"SenHub PostgreSQL linux"})
	if err != nil {
		t.Fatalf("linkExistingHosts error: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("expected 1 host.update, got %d", len(updates))
	}
	upd := updates[0]
	if upd.HostID != "1001" {
		t.Errorf("host.update hostid = %q, want 1001", upd.HostID)
	}
	var ids []string
	for _, tpl := range upd.Templates {
		ids = append(ids, tpl.TemplateID)
	}
	sort.Strings(ids)
	expected := []string{"1", "2", "9"}
	if len(ids) != len(expected) {
		t.Fatalf("expected %d template ids, got %d: %v", len(expected), len(ids), ids)
	}
	for i := range expected {
		if ids[i] != expected[i] {
			t.Errorf("template id mismatch at %d: got %s, want %s", i, ids[i], expected[i])
		}
	}
}

func TestLinkExistingHostsNoMissing(t *testing.T) {
	hosts := []testHost{
		{
			HostID: "1001",
			Host:   "web-01",
			ParentTemplates: []testTemplate{
				{"1", "SenHub CPU Metrics linux"},
				{"2", "SenHub Agent"},
				{"9", "SenHub PostgreSQL linux"},
			},
		},
	}
	var updates []hostUpdateRequest
	server, api := startFakeZabbix(t, hosts, &updates)
	defer server.Close()

	s := &zabbixSetup{api: api, out: &bytes.Buffer{}, templateByName: map[string]string{"SenHub PostgreSQL linux": "9"}}
	err := s.resolveHosts([]string{"web-01"})
	if err != nil {
		t.Fatalf("resolveHosts error: %v", err)
	}
	err = s.linkExistingHosts("linux", []string{"SenHub PostgreSQL linux"})
	if err != nil {
		t.Fatalf("linkExistingHosts error: %v", err)
	}
	if len(updates) != 0 {
		t.Errorf("expected no host.update, got %d", len(updates))
	}
}

func TestLinkExistingHostsDryRun(t *testing.T) {
	hosts := []testHost{
		{
			HostID: "1001",
			Host:   "web-01",
			ParentTemplates: []testTemplate{
				{"1", "SenHub CPU Metrics linux"},
				{"2", "SenHub Agent"},
			},
		},
	}
	var updates []hostUpdateRequest
	server, api := startFakeZabbix(t, hosts, &updates)
	defer server.Close()

	var buf bytes.Buffer
	s := &zabbixSetup{
		api:            api,
		out:            &buf,
		dryRun:         true,
		templateByName: map[string]string{"SenHub PostgreSQL linux": "9"},
	}
	err := s.resolveHosts([]string{"web-01"})
	if err != nil {
		t.Fatalf("resolveHosts error: %v", err)
	}
	err = s.linkExistingHosts("linux", []string{"SenHub PostgreSQL linux"})
	if err != nil {
		t.Fatalf("linkExistingHosts error: %v", err)
	}
	if len(updates) != 0 {
		t.Errorf("expected no host.update in dryRun, got %d", len(updates))
	}
	if !bytes.Contains(buf.Bytes(), []byte("would link")) {
		t.Errorf("expected output to contain 'would link', got: %s", buf.String())
	}
}
