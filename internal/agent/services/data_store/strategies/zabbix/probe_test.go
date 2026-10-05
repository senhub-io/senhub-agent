package zabbix

import (
	"context"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/strategies/zabbix/psk"
)

func plainDial(ctx context.Context, network, address string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

func probeParams(extra map[string]interface{}) map[string]interface{} {
	p := map[string]interface{}{"server": "127.0.0.1:10051", "hostname": "web-01"}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

func TestCheckProtocolKnownHost(t *testing.T) {
	srv := newFakeServer(t)
	srv.setItems("senhub.a[p]")
	res, err := CheckProtocol(context.Background(), probeParams(nil), srv.addr(), plainDial)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Answered || res.Host != HostKnown || res.Encryption != "" || res.Hostname != "web-01" {
		t.Fatalf("result = %+v", res)
	}
	if reqs := srv.requestsOf("active checks"); len(reqs) != 1 || reqs[0]["host"] != "web-01" {
		t.Fatalf("requests = %+v", reqs)
	}
}

func TestCheckProtocolUnknownHostIsAnsweredNotFailed(t *testing.T) {
	srv := newFakeServer(t)
	srv.refuseInfo["active checks"] = "host [web-01] not found"
	res, err := CheckProtocol(context.Background(), probeParams(nil), srv.addr(), plainDial)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Answered || res.Host != HostUnknown || res.ProtocolErr != nil || res.HostInfo == "" {
		t.Fatalf("result = %+v", res)
	}
}

func TestCheckProtocolRefusedHost(t *testing.T) {
	srv := newFakeServer(t)
	srv.refuseInfo["active checks"] = "host [web-01] not monitored"
	res, err := CheckProtocol(context.Background(), probeParams(nil), srv.addr(), plainDial)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Answered || res.Host != HostRefused {
		t.Fatalf("result = %+v", res)
	}
}

// Any open port passed the old test; a peer that is not a Zabbix server
// must now fail at the protocol step.
func TestCheckProtocolRejectsANonZabbixPeer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
			conn.Close()
		}
	}()
	res, err := CheckProtocol(context.Background(), probeParams(nil), ln.Addr().String(), plainDial)
	if err != nil {
		t.Fatal(err)
	}
	if res.Answered || res.ProtocolErr == nil || res.HandshakeErr != nil {
		t.Fatalf("result = %+v", res)
	}
}

func pskParams(t *testing.T, key []byte) map[string]interface{} {
	t.Helper()
	path := filepath.Join(t.TempDir(), "psk.key")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)), 0o600); err != nil {
		t.Fatal(err)
	}
	return probeParams(map[string]interface{}{
		"tls": map[string]interface{}{"enabled": true, "psk_identity": "senhub", "psk_file": path},
	})
}

// pskServer accepts one handshake with key and then answers an active
// checks request with an empty success.
func pskServer(t *testing.T, key []byte) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				conn, err := psk.Server(raw, psk.Config{Identity: "senhub", Key: key})
				if err != nil {
					raw.Close()
					return
				}
				defer conn.Close()
				if _, err := readFrame(conn); err != nil {
					return
				}
				_ = writeFrame(conn, []byte(`{"response":"success","data":[]}`))
			}()
		}
	}()
	return ln.Addr().String()
}

func TestCheckProtocolPSKAcceptedAndRefused(t *testing.T) {
	good := []byte("0123456789abcdef0123456789abcdef")
	bad := []byte("fedcba9876543210fedcba9876543210")
	addr := pskServer(t, good)

	res, err := CheckProtocol(context.Background(), pskParams(t, good), addr, plainDial)
	if err != nil {
		t.Fatal(err)
	}
	if res.Encryption != "PSK" || res.HandshakeErr != nil || !res.Answered || res.Host != HostKnown {
		t.Fatalf("right key: %+v", res)
	}

	res, err = CheckProtocol(context.Background(), pskParams(t, bad), addr, plainDial)
	if err != nil {
		t.Fatal(err)
	}
	if res.Encryption != "PSK" || res.HandshakeErr == nil || res.Answered {
		t.Fatalf("wrong key: %+v", res)
	}
}
