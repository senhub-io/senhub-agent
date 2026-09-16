package zabbix

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

func passiveConfig(t *testing.T) Config {
	t.Helper()
	cfg := testConfig("127.0.0.1:10051")
	cfg.Passive = PassiveConfig{Enabled: true, BindAddress: "127.0.0.1", Port: 0, Allow: []string{"127.0.0.0/8"}}
	return cfg
}

func startPassive(t *testing.T, cfg Config, lookup itemLookup) *passiveListener {
	t.Helper()
	pl, err := newPassiveListener(cfg, lookup, logger.NewModuleLogger(testLogger(), "test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := pl.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pl.stop)
	return pl
}

func pollPlain(t *testing.T, addr, key string) string {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte(key + "\n")); err != nil {
		t.Fatal(err)
	}
	reply, err := readFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	return string(reply)
}

func TestPassiveAnswersAgentPingInThePlainDialect(t *testing.T) {
	pl := startPassive(t, passiveConfig(t), nil)
	if got := pollPlain(t, pl.addr(), "agent.ping"); got != "1" {
		t.Errorf("agent.ping = %q", got)
	}
	if got := pollPlain(t, pl.addr(), "agent.hostname"); got != "web-01" {
		t.Errorf("agent.hostname = %q", got)
	}
	if got := pollPlain(t, pl.addr(), "agent.version"); got == "" {
		t.Error("agent.version must not be empty")
	}
}

func TestPassiveReportsAnUnknownKeyAsNotSupported(t *testing.T) {
	pl := startPassive(t, passiveConfig(t), nil)
	got := pollPlain(t, pl.addr(), "vfs.fs.size[/,used]")
	if !strings.HasPrefix(got, "ZBX_NOTSUPPORTED\x00") {
		t.Errorf("reply = %q", got)
	}
}

func TestPassiveServesTheAgentsOwnKeys(t *testing.T) {
	lookup := func(key string) (string, bool) {
		if key == "senhub.system.memory.utilization[memory]" {
			return "0.42", true
		}
		return "", false
	}
	pl := startPassive(t, passiveConfig(t), lookup)
	if got := pollPlain(t, pl.addr(), "senhub.system.memory.utilization[memory]"); got != "0.42" {
		t.Errorf("value = %q", got)
	}
}

func TestPassiveAnswersTheJSONDialectOfNewerServers(t *testing.T) {
	pl := startPassive(t, passiveConfig(t), nil)
	conn, err := net.DialTimeout("tcp", pl.addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	req := []byte(`{"request":"passive checks","data":[{"key":"agent.ping","timeout":3},{"key":"nope","timeout":3}]}`)
	if err := writeFrame(conn, req); err != nil {
		t.Fatal(err)
	}
	raw, err := readFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	var reply passiveReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		t.Fatalf("reply %q: %v", raw, err)
	}
	if reply.Variant != 2 || reply.Version == "" || len(reply.Data) != 2 {
		t.Fatalf("reply = %+v", reply)
	}
	if reply.Data[0].Value != "1" || reply.Data[1].Error != unsupportedKey {
		t.Errorf("data = %+v", reply.Data)
	}
}

func TestPassiveRefusesAnAddressOutsideTheAllowList(t *testing.T) {
	cfg := passiveConfig(t)
	cfg.Passive.Allow = []string{"192.0.2.0/24"}
	pl := startPassive(t, cfg, nil)
	conn, err := net.DialTimeout("tcp", pl.addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write([]byte("agent.ping\n"))
	buf := make([]byte, 16)
	n, _ := conn.Read(buf)
	if n != 0 {
		t.Errorf("a refused poller got %q, want the connection closed", buf[:n])
	}
}

func TestAllowedNetsDefaultToTheServerAddress(t *testing.T) {
	cfg := testConfig("127.0.0.1:10051")
	nets, err := allowedNets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) != 1 || !nets[0].Contains(net.ParseIP("127.0.0.1")) || nets[0].Contains(net.ParseIP("127.0.0.2")) {
		t.Errorf("nets = %v", nets)
	}
}

func TestActiveChecksCarryThePassivePortForAutoregistration(t *testing.T) {
	srv := newFakeServer(t)
	cfg := testConfig(srv.addr())
	cfg.Passive = PassiveConfig{Enabled: true, Port: 10250}
	c, _ := newClient(cfg)
	if _, err := c.activeChecks(context.Background()); err != nil {
		t.Fatal(err)
	}
	req := srv.requestsOf("active checks")[0]
	if req["port"] == nil || req["port"].(float64) != 10250 {
		t.Errorf("port = %v, want 10250", req["port"])
	}
}

func TestWriteFrameThenReadFrameCarriesAnEmptyValue(t *testing.T) {
	var buf bytes.Buffer
	_ = writeFrame(&buf, []byte(""))
	got, err := readFrame(&buf)
	if err != nil || len(got) != 0 {
		t.Errorf("got %q, %v", got, err)
	}
}
