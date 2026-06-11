package snmpcore

import (
	"math/big"
	"testing"

	"github.com/gosnmp/gosnmp"
)

// TestIsPrintable pins the ONE reconciled semantics (#291): trailing
// NULs tolerated, UTF-8 text accepted, tab/LF/CR accepted, bare control
// bytes and invalid UTF-8 rejected.
func TestIsPrintable(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		{"plain ascii", []byte("eth0"), true},
		{"trailing NULs tolerated (device padding)", []byte("host1\x00\x00"), true},
		{"leading NUL rejected", []byte{0x00, 'a'}, false},
		{"utf-8 text accepted", []byte("température"), true},
		{"tab lf cr accepted", []byte("line1\n\tline2\r"), true},
		{"vertical tab rejected", []byte("a\x0bb"), false},
		{"form feed rejected", []byte("a\x0cb"), false},
		{"invalid utf-8 rejected", []byte{0x00, 0xff, 0x10}, false},
		{"empty is printable", []byte{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPrintable(tc.in); got != tc.want {
				t.Errorf("IsPrintable(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestOctetText migrated from snmppoll (lldp_test.go): topology-rail
// rendering — text with trailing NULs stripped, bare lowercase hex for
// binary.
func TestOctetText(t *testing.T) {
	if got := OctetText([]byte("host1\x00")); got != "host1" {
		t.Errorf("OctetText(host1 NUL) = %q, want host1", got)
	}
	if got := OctetText([]byte{0x00, 0xff, 0x10}); got != "00ff10" {
		t.Errorf("OctetText(binary) = %q, want 00ff10", got)
	}
	if got := OctetText(nil); got != "" {
		t.Errorf("OctetText(nil) = %q, want empty", got)
	}
}

// TestFormatPDUValue migrated from snmptrap (traps_test.go): log-rail
// rendering — printable bytes as text, binary as 0x-prefixed hex, the
// numeric/bool/big.Int dispatch.
func TestFormatPDUValue(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		want string
	}{
		{"printable bytes", []byte("eth0"), "eth0"},
		{"binary bytes hex", []byte{0x00, 0xff, 0x10}, "0x00ff10"},
		{"trailing NUL now text (reconciled semantics)", []byte("host\x00"), "host"},
		{"string", "1.3.6.1.2.1.1", "1.3.6.1.2.1.1"},
		{"int", 42, "42"},
		{"uint32", uint32(7), "7"},
		{"big int", big.NewInt(9000000000), "9000000000"},
		{"bool", true, "true"},
		{"nil", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatPDUValue(gosnmp.SnmpPDU{Value: tc.in}); got != tc.want {
				t.Errorf("FormatPDUValue(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	for _, s := range []string{"2", "2c", "v2c", "V2C", " v2c "} {
		if v, err := ParseVersion(s); err != nil || v != gosnmp.Version2c {
			t.Errorf("ParseVersion(%q) = %v, %v; want Version2c", s, v, err)
		}
	}
	if v, err := ParseVersion("v3"); err != nil || v != gosnmp.Version3 {
		t.Errorf("ParseVersion(v3) = %v, %v", v, err)
	}
	if v, err := ParseVersion("v1"); err != nil || v != gosnmp.Version1 {
		t.Errorf("ParseVersion(v1) = %v, %v", v, err)
	}
	if _, err := ParseVersion("v9"); err == nil {
		t.Error("ParseVersion(v9) must error")
	}
}

func TestVersionString(t *testing.T) {
	if got := VersionString(gosnmp.Version2c); got != "v2c" {
		t.Errorf("VersionString(2c) = %q", got)
	}
	if got := VersionString(gosnmp.Version3); got != "v3" {
		t.Errorf("VersionString(3) = %q", got)
	}
}

func TestUSMTables(t *testing.T) {
	if AuthProtocol("SHA256") != gosnmp.SHA256 || AuthProtocol("") != gosnmp.NoAuth || AuthProtocol("bogus") != gosnmp.NoAuth {
		t.Error("AuthProtocol table broken")
	}
	if PrivProtocol("AES256") != gosnmp.AES256 || PrivProtocol("") != gosnmp.NoPriv {
		t.Error("PrivProtocol table broken")
	}
	if MsgFlags(gosnmp.SHA, gosnmp.AES) != gosnmp.AuthPriv {
		t.Error("MsgFlags authPriv broken")
	}
	if MsgFlags(gosnmp.SHA, gosnmp.NoPriv) != gosnmp.AuthNoPriv {
		t.Error("MsgFlags authNoPriv broken")
	}
	if MsgFlags(gosnmp.NoAuth, gosnmp.NoPriv) != gosnmp.NoAuthNoPriv {
		t.Error("MsgFlags noAuthNoPriv broken")
	}
}

func TestCoercions(t *testing.T) {
	if string(AsBytes("abc")) != "abc" || AsBytes(42) != nil {
		t.Error("AsBytes broken")
	}
	if n, ok := AsInt(uint64(9)); !ok || n != 9 {
		t.Error("AsInt broken")
	}
	if _, ok := AsInt("x"); ok {
		t.Error("AsInt must reject strings")
	}
	if TrimLeadingDot(".1.3.6") != "1.3.6" || TrimLeadingDot("1.3.6") != "1.3.6" {
		t.Error("TrimLeadingDot broken")
	}
	if TrimLeadingDot("  .1.3.6  ") != "1.3.6" || TrimLeadingDot("") != "" {
		t.Error("TrimLeadingDot whitespace handling broken (migrated from snmppoll client_test)")
	}
}
