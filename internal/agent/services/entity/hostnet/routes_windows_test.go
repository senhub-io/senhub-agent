//go:build windows

package hostnet

import "testing"

// Reads the real routing table of the Windows host the test runs on.
func TestPlatformRoutes_Windows(t *testing.T) {
	routes, err := platformRoutes()
	if err != nil {
		t.Fatalf("GetIpForwardTable: %v", err)
	}
	var def *hostRoute
	for i := range routes {
		t.Logf("route %s via %s (%s) metric %d", routes[i].Destination, routes[i].NextHop, routes[i].Iface, routes[i].Metric)
		if routes[i].Destination == "0.0.0.0/0" {
			def = &routes[i]
		}
	}
	if def == nil {
		t.Fatal("no default route read")
	}
}
