package snmppoll

import (
	"math/big"
	"testing"

	"github.com/gosnmp/gosnmp"
)

func TestTrimLeadingDot(t *testing.T) {
	cases := []struct{ in, want string }{
		{".1.3.6.1", "1.3.6.1"},
		{"1.3.6.1", "1.3.6.1"},
		{"  .1.3.6  ", "1.3.6"},
		{"", ""},
	}
	for _, c := range cases {
		if got := trimLeadingDot(c.in); got != c.want {
			t.Errorf("trimLeadingDot(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

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
