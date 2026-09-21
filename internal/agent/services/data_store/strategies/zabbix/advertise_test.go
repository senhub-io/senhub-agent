package zabbix

import (
	"context"
	"testing"
)

func advertiseConfig(server, advertise string) Config {
	cfg := testConfig(server)
	cfg.Passive = PassiveConfig{Enabled: true, Port: 10050, Advertise: advertise}
	return cfg
}

// Left to itself Zabbix records the address the packets came from, which
// behind NAT is the translation and not somewhere the server can poll.
func TestTheRegistrationCarriesTheAddressTheOperatorNamed(t *testing.T) {
	cases := []struct {
		advertise string
		field     string
	}{
		{"web-01.example.com", "interface"},
		{"10.20.0.7", "ip"},
	}
	for _, tc := range cases {
		srv := newFakeServer(t)
		c, err := newClient(advertiseConfig(srv.addr(), tc.advertise))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.activeChecks(context.Background()); err != nil {
			t.Fatal(err)
		}
		req := srv.requestsOf("active checks")[0]
		if req[tc.field] != tc.advertise {
			t.Errorf("%q went as %+v, want it under %q", tc.advertise, req, tc.field)
		}
		other := map[string]string{"interface": "ip", "ip": "interface"}[tc.field]
		if _, sent := req[other]; sent {
			t.Errorf("%q was also sent as %q; a name and an address are not the same field", tc.advertise, other)
		}
		if req["port"] == nil {
			t.Error("the port must still travel with it")
		}
	}
}

func TestWithoutAnAddressNothingIsClaimed(t *testing.T) {
	srv := newFakeServer(t)
	c, err := newClient(advertiseConfig(srv.addr(), ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.activeChecks(context.Background()); err != nil {
		t.Fatal(err)
	}
	req := srv.requestsOf("active checks")[0]
	for _, f := range []string{"ip", "interface"} {
		if _, sent := req[f]; sent {
			t.Errorf("%q was sent although the operator named no address", f)
		}
	}
}

func TestAnEmptyAdvertiseIsRefusedRatherThanIgnored(t *testing.T) {
	if _, err := parsePassive(map[string]interface{}{"advertise": "   "}, PassiveConfig{}); err == nil {
		t.Fatal("a blank address was accepted; the operator meant to name one")
	}
}
