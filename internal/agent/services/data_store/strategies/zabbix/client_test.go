package zabbix

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func testConfig(server string) Config {
	return Config{
		Server: server, Hostname: "web-01", HostMetadata: "senhub-agent",
		Interval: time.Second, RefreshInterval: time.Second, HeartbeatInterval: 30 * time.Second,
		Timeout: 2 * time.Second, KeyPrefix: "senhub",
	}
}

func TestActiveChecksSendsTheHostAndItsMetadata(t *testing.T) {
	srv := newFakeServer(t)
	srv.setItems("senhub.system.cpu.utilization[host-cpu,0]")
	c, err := newClient(testConfig(srv.addr()))
	if err != nil {
		t.Fatal(err)
	}
	items, _, err := c.activeChecks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "senhub.system.cpu.utilization[host-cpu,0]" || items[0].ItemID != 1000 {
		t.Fatalf("items = %+v", items)
	}
	reqs := srv.requestsOf("active checks")
	// The metadata carries the platform beside what the operator wrote,
	// so the autoregistration action links the template set this host
	// can actually feed. What was written must stay matchable.
	if len(reqs) != 1 || reqs[0]["host"] != "web-01" || reqs[0]["host_metadata"] != metadataWithPlatform("senhub-agent") {
		t.Fatalf("request = %+v", reqs)
	}
	if md, _ := reqs[0]["host_metadata"].(string); !strings.HasPrefix(md, "senhub-agent") {
		t.Fatalf("host_metadata = %q, want what the operator wrote first", md)
	}
}

func TestSendValuesCarriesOneIDPerValueAndReadsTheSummary(t *testing.T) {
	srv := newFakeServer(t)
	c, err := newClient(testConfig(srv.addr()))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1700000000, 123)
	res, err := c.sendValues(context.Background(), []item{
		{Key: "senhub.a[p]", Value: "1"}, {Key: "senhub.b[p]", Value: "2.5"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Processed != 2 || res.Failed != 0 || res.Total != 2 {
		t.Errorf("result = %+v", res)
	}
	req := srv.requestsOf("agent data")[0]
	if req["session"] != c.session || req["host"] != "web-01" {
		t.Errorf("request = %+v", req)
	}
	data := req["data"].([]interface{})
	first, second := data[0].(map[string]interface{}), data[1].(map[string]interface{})
	if first["id"].(float64) != 1 || second["id"].(float64) != 2 {
		t.Errorf("ids = %v %v, want 1 and 2", first["id"], second["id"])
	}
	if first["key"] != "senhub.a[p]" || first["value"] != "1" || first["clock"].(float64) != 1700000000 || first["host"] != "web-01" {
		t.Errorf("first value = %+v", first)
	}
}

func TestSendValuesReportsARefusal(t *testing.T) {
	srv := newFakeServer(t)
	srv.refuse["agent data"] = true
	c, _ := newClient(testConfig(srv.addr()))
	_, err := c.sendValues(context.Background(), []item{{Key: "k", Value: "1"}}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "refused by test") {
		t.Fatalf("err = %v", err)
	}
}

func TestExchangeReadsACompressedReply(t *testing.T) {
	srv := newFakeServer(t)
	srv.compressReplies = true
	srv.setItems("senhub.x[p]")
	c, _ := newClient(testConfig(srv.addr()))
	items, _, err := c.activeChecks(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %v, err = %v", items, err)
	}
}

func TestHeartbeatCarriesTheFrequencyAndExpectsNoReply(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newClient(testConfig(srv.addr()))
	if err := c.heartbeat(context.Background()); err != nil {
		t.Fatalf("a server that closes without answering is the normal case: %v", err)
	}
	req := srv.requestsOf("active check heartbeat")[0]
	if req["heartbeat_freq"].(float64) != 30 {
		t.Errorf("heartbeat_freq = %v", req["heartbeat_freq"])
	}
}

func TestHeartbeatRefusedByAnOldServerIsAnError(t *testing.T) {
	srv := newFakeServer(t)
	srv.refuse["active check heartbeat"] = true
	c, _ := newClient(testConfig(srv.addr()))
	if err := c.heartbeat(context.Background()); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("err = %v", err)
	}
}

func TestActiveChecksTellsAnUnknownHostApart(t *testing.T) {
	srv := newFakeServer(t)
	srv.refuseInfo["active checks"] = "host [web-01] not found"
	c, _ := newClient(testConfig(srv.addr()))
	_, _, err := c.activeChecks(context.Background())
	if !errors.Is(err, errHostUnknown) {
		t.Fatalf("err = %v, want errHostUnknown", err)
	}
}

func TestParseInfo(t *testing.T) {
	got := parseInfo("processed: 7; failed: 2; total: 9; seconds spent: 0.001")
	if got != (pushResult{Processed: 7, Failed: 2, Total: 9}) {
		t.Errorf("got %+v", got)
	}
	if parseInfo("") != (pushResult{}) {
		t.Error("an empty info reads as zero")
	}
}

func TestConnectFailureNamesTheServer(t *testing.T) {
	c, _ := newClient(testConfig("127.0.0.1:1"))
	_, _, err := c.activeChecks(context.Background())
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("err = %v", err)
	}
}
