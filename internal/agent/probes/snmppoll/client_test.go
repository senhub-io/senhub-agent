package snmppoll

import (
	"math/big"
	"testing"

	"github.com/gosnmp/gosnmp"
)

func TestPduToVarBind(t *testing.T) {
	cases := []struct {
		name      string
		pdu       gosnmp.SnmpPDU
		wantNum   bool
		wantValue float64
	}{
		{"counter64", gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.2.2.1.10.1", Type: gosnmp.Counter64, Value: uint64(123456789)}, true, 123456789},
		{"counter32", gosnmp.SnmpPDU{Name: ".1", Type: gosnmp.Counter32, Value: uint(99)}, true, 99},
		{"gauge32", gosnmp.SnmpPDU{Name: ".1", Type: gosnmp.Gauge32, Value: uint(42)}, true, 42},
		{"integer", gosnmp.SnmpPDU{Name: ".1", Type: gosnmp.Integer, Value: 7}, true, 7},
		{"timeticks", gosnmp.SnmpPDU{Name: ".1", Type: gosnmp.TimeTicks, Value: uint32(9000)}, true, 9000},
		{"octetstring-skipped", gosnmp.SnmpPDU{Name: ".1", Type: gosnmp.OctetString, Value: []byte("eth0")}, false, 0},
		{"nosuchobject", gosnmp.SnmpPDU{Name: ".1", Type: gosnmp.NoSuchObject}, false, 0},
		{"nosuchinstance", gosnmp.SnmpPDU{Name: ".1", Type: gosnmp.NoSuchInstance}, false, 0},
		{"endofmib", gosnmp.SnmpPDU{Name: ".1", Type: gosnmp.EndOfMibView}, false, 0},
		{"null", gosnmp.SnmpPDU{Name: ".1", Type: gosnmp.Null}, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vb := pduToVarBind(c.pdu)
			if vb.IsNumeric != c.wantNum {
				t.Fatalf("IsNumeric = %v, want %v", vb.IsNumeric, c.wantNum)
			}
			if c.wantNum && vb.Value != c.wantValue {
				t.Errorf("Value = %v, want %v", vb.Value, c.wantValue)
			}
			if vb.OID == "" || vb.OID[0] == '.' {
				t.Errorf("OID %q should be trimmed of leading dot", vb.OID)
			}
		})
	}
}

func TestBigIntToFloat(t *testing.T) {
	if got := bigIntToFloat(nil); got != 0 {
		t.Errorf("bigIntToFloat(nil) = %v, want 0", got)
	}
	if got := bigIntToFloat(big.NewInt(1024)); got != 1024 {
		t.Errorf("bigIntToFloat(1024) = %v, want 1024", got)
	}
}

// TestNewGosnmpClient_V3USM pins the USM wiring: the handle carries the
// user security model with the derived authPriv level, and no community.
func TestNewGosnmpClient_V3USM(t *testing.T) {
	c := newGosnmpClient(&config{
		Target:  "192.0.2.10",
		Port:    161,
		Version: gosnmp.Version3,
		V3: &v3Config{
			Username:     "monitoring",
			AuthProtocol: "SHA256",
			AuthPassword: "auth-secret",
			PrivProtocol: "AES256",
			PrivPassword: "priv-secret",
		},
	})
	h := c.handle
	if h.Version != gosnmp.Version3 || h.SecurityModel != gosnmp.UserSecurityModel {
		t.Fatalf("version/security model not set: %v / %v", h.Version, h.SecurityModel)
	}
	if h.MsgFlags != gosnmp.AuthPriv {
		t.Errorf("MsgFlags = %v, want AuthPriv", h.MsgFlags)
	}
	if h.Community != "" {
		t.Errorf("community must not be set under v3, got %q", h.Community)
	}
	usm, ok := h.SecurityParameters.(*gosnmp.UsmSecurityParameters)
	if !ok || usm.UserName != "monitoring" || usm.AuthenticationProtocol != gosnmp.SHA256 || usm.PrivacyProtocol != gosnmp.AES256 {
		t.Errorf("USM parameters wrong: %+v", usm)
	}
}

func TestNewGosnmpClient_V2cKeepsCommunity(t *testing.T) {
	c := newGosnmpClient(&config{Target: "192.0.2.10", Port: 161, Version: gosnmp.Version2c, Community: "ro"})
	if c.handle.Community != "ro" || c.handle.Version != gosnmp.Version2c {
		t.Errorf("v2c handle wrong: community=%q version=%v", c.handle.Community, c.handle.Version)
	}
}
