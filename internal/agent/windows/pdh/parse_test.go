package pdh

import (
	"reflect"
	"testing"
	"unicode/utf16"
)

// TestBuildCounterPath pins the path grammar the Windows probes depend
// on. It is pure string handling, but it had no test because it lived
// behind //go:build windows with the syscalls (#297) — and a wrong path
// does not fail loudly: PDH returns an opaque error code and the counter
// silently reports nothing.
func TestBuildCounterPath(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		instance string
		want     string
	}{
		{
			name:     "instance inserted after the object",
			path:     `\LogicalDisk\% Free Space`,
			instance: "C:",
			want:     `\LogicalDisk(C:)\% Free Space`,
		},
		{
			name:     "sub-parts of the counter are preserved",
			path:     `\Processor Information\Processor Time\Total`,
			instance: "0,1",
			want:     `\Processor Information(0,1)\Processor Time\Total`,
		},
		{
			name:     "empty instance leaves the path alone",
			path:     `\Memory\Available MBytes`,
			instance: "",
			want:     `\Memory\Available MBytes`,
		},
		{
			name:     "a path with no object segment is returned unchanged",
			path:     `Memory`,
			instance: "C:",
			want:     `Memory`,
		},
		{
			name:     "an instance containing a space survives verbatim",
			path:     `\Network Interface\Bytes Total/sec`,
			instance: "Intel[R] Ethernet Connection",
			want:     `\Network Interface(Intel[R] Ethernet Connection)\Bytes Total/sec`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := BuildCounterPath(c.path, c.instance); got != c.want {
				t.Errorf("BuildCounterPath(%q, %q) = %q, want %q", c.path, c.instance, got, c.want)
			}
		})
	}
}

// utf16Buffer builds the NUL-separated, double-NUL-terminated buffer
// shape PdhEnumObjectItemsW returns, so the test feeds the parser what
// the API actually produces rather than a convenient approximation.
func utf16Buffer(names ...string) []uint16 {
	var buf []uint16
	for _, n := range names {
		buf = append(buf, utf16.Encode([]rune(n))...)
		buf = append(buf, 0)
	}
	return append(buf, 0)
}

func TestParseInstanceList(t *testing.T) {
	cases := []struct {
		name string
		buf  []uint16
		want []string
	}{
		{
			name: "names split on NUL",
			buf:  utf16Buffer("C:", "D:", "HarddiskVolume1"),
			want: []string{"C:", "D:", "HarddiskVolume1"},
		},
		{
			name: "_Total is dropped — it is PDH's roll-up, not an instance",
			buf:  utf16Buffer("C:", "_Total", "D:"),
			want: []string{"C:", "D:"},
		},
		{
			name: "empty entries from an over-sized buffer are dropped",
			buf:  append(utf16Buffer("C:"), 0, 0, 0, 0),
			want: []string{"C:"},
		},
		{
			name: "an empty buffer yields nothing",
			buf:  utf16Buffer(),
			want: nil,
		},
		{
			name: "only _Total yields nothing",
			buf:  utf16Buffer("_Total"),
			want: nil,
		},
		{
			name: "non-ASCII instance names survive the UTF-16 decode",
			buf:  utf16Buffer("Réseau local", "接続"),
			want: []string{"Réseau local", "接続"},
		},
		{
			name: "a buffer missing its terminator still yields its last name",
			buf:  utf16Encode("C:", "D:"),
			want: []string{"C:", "D:"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseInstanceList(c.buf)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("parseInstanceList = %q, want %q", got, c.want)
			}
		})
	}
}

// utf16Encode builds a buffer WITHOUT the trailing terminator, to cover
// the malformed case.
func utf16Encode(names ...string) []uint16 {
	var buf []uint16
	for i, n := range names {
		if i > 0 {
			buf = append(buf, 0)
		}
		buf = append(buf, utf16.Encode([]rune(n))...)
	}
	return buf
}
