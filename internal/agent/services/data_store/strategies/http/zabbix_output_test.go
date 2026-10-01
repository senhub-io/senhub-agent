package http

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
)

// zabbixStub answers every request with reply, framed the way a Zabbix
// server frames it.
func zabbixStub(t *testing.T, reply map[string]interface{}) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	body, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				head := make([]byte, 13)
				if _, err := io.ReadFull(conn, head); err != nil {
					return
				}
				n := binary.LittleEndian.Uint32(head[5:9])
				if _, err := io.CopyN(io.Discard, conn, int64(n)); err != nil {
					return
				}
				out := append([]byte("ZBXD\x01"), make([]byte, 8)...)
				binary.LittleEndian.PutUint32(out[5:9], uint32(len(body)))
				_, _ = conn.Write(append(out, body...))
			}()
		}
	}()
	return ln.Addr().String()
}

func stepNamed(resp map[string]interface{}, prefix string) map[string]interface{} {
	for _, s := range resp["steps"].([]interface{}) {
		m := s.(map[string]interface{})
		if strings.HasPrefix(m["name"].(string), prefix) {
			return m
		}
	}
	return nil
}

func TestOutputTest_ZabbixProtocolSteps(t *testing.T) {
	router, _ := newOutputsTestRouter(t)
	base := "/api/" + testAdminKey
	run := func(addr string) map[string]interface{} {
		code, resp := doJSON(t, router, "POST", base+"/config/outputs/test", map[string]interface{}{
			"type": "zabbix", "params": map[string]interface{}{"server": addr, "hostname": "web-01"}, "timeout": 3,
		})
		if code != 200 {
			t.Fatalf("status %d: %v", code, resp)
		}
		return resp
	}

	known := run(zabbixStub(t, map[string]interface{}{"response": "success", "data": []interface{}{}}))
	if known["valid"] != true {
		t.Fatalf("known host: %v", known)
	}
	if p := stepNamed(known, "protocol"); p == nil || p["passed"] != true || !strings.Contains(p["detail"].(string), "autoregistration") {
		t.Errorf("protocol step: %v", p)
	}
	if h := stepNamed(known, "host web-01"); h == nil || h["passed"] != true || h["warning"] != nil {
		t.Errorf("host step: %v", h)
	}

	unknown := run(zabbixStub(t, map[string]interface{}{"response": "failed", "info": "host [web-01] not found"}))
	if unknown["valid"] != true {
		t.Fatalf("an unknown host must not fail the test: %v", unknown)
	}
	if h := stepNamed(unknown, "host web-01"); h == nil || h["passed"] != true || h["warning"] == nil {
		t.Errorf("an unknown host must pass with a warning: %v", h)
	}
}
