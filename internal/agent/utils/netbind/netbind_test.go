package netbind

import "testing"

func TestIsWildcard(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"0.0.0.0", true},
		{"0.0.0.0:162", true},
		{"::", true},
		{"[::]", true},
		{"[::]:4317", true},
		{":8080", true},
		{"127.0.0.1", false},
		{"127.0.0.1:4317", false},
		{"192.168.1.10:514", false},
		{"localhost:514", false},
		{"localhost", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsWildcard(c.addr); got != c.want {
			t.Errorf("IsWildcard(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}
