package snmppoll

import (
	"io"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

const walkRoot = ".1.3.6.1.2.1.2.2.1.2"

var walkRows = []struct {
	oid   string
	value string
}{
	{".1.3.6.1.2.1.2.2.1.2.1", "eth0"},
	{".1.3.6.1.2.1.2.2.1.2.2", "eth1"},
	{".1.3.6.1.2.1.2.2.1.2.3", "lo"},
}

func buildWalkResponse(req *gosnmp.SnmpPacket) *gosnmp.SnmpPacket {
	resp := &gosnmp.SnmpPacket{
		Version:    gosnmp.Version2c,
		Community:  "public",
		PDUType:    gosnmp.GetResponse,
		RequestID:  req.RequestID,
		Error:      gosnmp.NoError,
		ErrorIndex: 0,
	}
	oid := ""
	if len(req.Variables) > 0 {
		oid = req.Variables[0].Name
	}
	if strings.HasPrefix(oid, ".1.3.6.1.2.1.1.5") {
		resp.Error = gosnmp.GenErr
		resp.ErrorIndex = 1
		resp.Variables = []gosnmp.SnmpPDU{{Name: oid, Type: gosnmp.Null, Value: nil}}
		return resp
	}
	var vars []gosnmp.SnmpPDU
	start := 0
	for i, row := range walkRows {
		if row.oid == oid {
			start = i + 1
		}
	}
	if oid != walkRoot && start == 0 {
		start = len(walkRows)
	}
	for _, row := range walkRows[start:] {
		vars = append(vars, gosnmp.SnmpPDU{Name: row.oid, Type: gosnmp.OctetString, Value: []byte(row.value)})
	}
	if len(vars) == 0 {
		resp.Variables = []gosnmp.SnmpPDU{{Name: ".1.3.6.1.2.1.2.2.1.3.1", Type: gosnmp.Integer, Value: 1}}
		return resp
	}
	resp.Variables = vars
	return resp
}

func startWalkResponder(t *testing.T) uint16 {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	decoder := &gosnmp.GoSNMP{
		Version:   gosnmp.Version2c,
		Community: "public",
		Logger:    gosnmp.NewLogger(log.New(io.Discard, "", 0)),
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 2048)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			pkt, err := decoder.SnmpDecodePacket(buf[:n])
			if err != nil {
				continue
			}
			out, err := buildWalkResponse(pkt).MarshalMsg()
			if err != nil {
				continue
			}
			if _, err := conn.WriteTo(out, addr); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		conn.Close()
		<-done
	})
	return uint16(conn.LocalAddr().(*net.UDPAddr).Port)
}

func newTestWalkClient(t *testing.T, port uint16) *gosnmpClient {
	t.Helper()
	cl := newGosnmpClient(&config{
		Target:    "127.0.0.1",
		Port:      port,
		Version:   gosnmp.Version2c,
		Community: "public",
		Timeout:   2 * time.Second,
		Retries:   0,
	})
	if err := cl.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cl.Close() })
	return cl
}

func TestWalkRaw_ErrorStatusIsAnError(t *testing.T) {
	cl := newTestWalkClient(t, startWalkResponder(t))
	_, err := cl.WalkRaw("1.3.6.1.2.1.1.5")
	if err == nil {
		t.Fatal("expected non-nil error for genErr error-status, got nil")
	}
}

func TestBulkWalk_ErrorStatusIsAnError(t *testing.T) {
	cl := newTestWalkClient(t, startWalkResponder(t))
	_, err := cl.BulkWalk("1.3.6.1.2.1.1.5")
	if err == nil {
		t.Fatal("expected non-nil error for genErr error-status, got nil")
	}
}

func TestWalkRaw_HealthyTableStillWalks(t *testing.T) {
	cl := newTestWalkClient(t, startWalkResponder(t))
	binds, err := cl.WalkRaw("1.3.6.1.2.1.2.2.1.2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(binds) != 3 {
		t.Fatalf("expected 3 varbinds, got %d", len(binds))
	}
	wantOIDs := []string{"1.3.6.1.2.1.2.2.1.2.1", "1.3.6.1.2.1.2.2.1.2.2", "1.3.6.1.2.1.2.2.1.2.3"}
	wantVals := []string{"eth0", "eth1", "lo"}
	for i, b := range binds {
		if b.OID != wantOIDs[i] {
			t.Errorf("bind %d: OID = %q, want %q", i, b.OID, wantOIDs[i])
		}
		val, ok := b.Value.([]byte)
		if !ok {
			t.Errorf("bind %d: value type %T, want []byte", i, b.Value)
			continue
		}
		if string(val) != wantVals[i] {
			t.Errorf("bind %d: value = %q, want %q", i, val, wantVals[i])
		}
	}
}
