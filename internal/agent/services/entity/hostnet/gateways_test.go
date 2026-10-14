package hostnet

import (
	"reflect"
	"testing"
)

func TestDefaultGatewaysFrom(t *testing.T) {
	tests := []struct {
		name   string
		routes []hostRoute
		want   []string
	}{
		{
			name: "metric then address, duplicate collapsed",
			routes: []hostRoute{
				{Destination: "0.0.0.0/0", NextHop: "10.0.0.1", Metric: 20, Iface: "eth0"},
				{Destination: "0.0.0.0/0", NextHop: "10.0.0.2", Metric: 10, Iface: "eth0"},
				{Destination: "0.0.0.0/0", NextHop: "10.0.0.1", Metric: 30, Iface: "eth1"},
			},
			want: []string{"10.0.0.2", "10.0.0.1"},
		},
		{
			name: "filters container bridges, non-default, IPv6-looking and host-local next hops",
			routes: []hostRoute{
				{Destination: "0.0.0.0/0", NextHop: "10.0.0.1", Metric: 20, Iface: "eth0"},
				{Destination: "0.0.0.0/0", NextHop: "172.17.0.1", Metric: 5, Iface: "docker0"},
				{Destination: "0.0.0.0/0", NextHop: "172.18.0.1", Metric: 5, Iface: "br-abc123"},
				{Destination: "10.0.0.0/24", NextHop: "10.0.0.254", Metric: 5, Iface: "eth0"},
				{Destination: "0.0.0.0/0", NextHop: "2001:db8::1", Metric: 5, Iface: "eth0"},
				{Destination: "0.0.0.0/0", NextHop: "127.0.0.1", Metric: 5, Iface: "lo"},
				{Destination: "0.0.0.0/0", NextHop: "0.0.0.0", Metric: 5, Iface: "eth0"},
			},
			want: []string{"10.0.0.1"},
		},
		{
			name:   "empty routes",
			routes: nil,
			want:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := defaultGatewaysFrom(tt.routes)
			if got == nil {
				got = []string{}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("defaultGatewaysFrom() = %v, want %v", got, tt.want)
			}
		})
	}
}
