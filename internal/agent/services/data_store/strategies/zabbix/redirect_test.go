package zabbix

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func testConfigServers(addrs ...string) Config {
	cfg := testConfig(addrs[0])
	cfg.Servers = addrs
	return cfg
}

func TestAProxyGroupRedirectIsFollowedAndThenUsedDirectly(t *testing.T) {
	holder := newFakeServer(t)
	holder.setItems("senhub.system.cpu.utilization[cpu,0]")
	entry := newFakeServer(t)
	entry.setRedirect(holder.addr(), 1)

	c, err := newClient(testConfig(entry.addr()))
	if err != nil {
		t.Fatal(err)
	}
	items, err := c.activeChecks(context.Background())
	if err != nil {
		t.Fatalf("the redirect was not followed: %v", err)
	}
	if len(items) != 1 || items[0].Key != "senhub.system.cpu.utilization[cpu,0]" {
		t.Fatalf("items = %+v", items)
	}

	if _, err := c.activeChecks(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := entry.requestCount(); n != 1 {
		t.Fatalf("the entry point was asked %d times; once the group has spoken every later request goes straight to the holder", n)
	}
	if n := holder.requestCount(); n != 2 {
		t.Fatalf("the holder was asked %d times, want 2", n)
	}
}

func TestValuesAreRedirectedLikeTheCheckList(t *testing.T) {
	holder := newFakeServer(t)
	entry := newFakeServer(t)
	entry.setRedirect(holder.addr(), 1)

	c, err := newClient(testConfig(entry.addr()))
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.sendValues(context.Background(), []item{{Key: "senhub.k[p]", Value: "1"}}, time.Now())
	if err != nil {
		t.Fatalf("the values were not redirected: %v", err)
	}
	if res.Processed != 1 {
		t.Fatalf("processed = %d, want 1", res.Processed)
	}
	if len(holder.requestsOf("agent data")) != 1 {
		t.Fatal("the holder never received the batch")
	}
}

func TestAStaleRedirectRevisionIsIgnored(t *testing.T) {
	newHolder := newFakeServer(t)
	newHolder.setItems("senhub.k[p]")
	oldHolder := newFakeServer(t)
	oldHolder.setItems("stale.key[p]")
	entry := newFakeServer(t)

	c, err := newClient(testConfig(entry.addr()))
	if err != nil {
		t.Fatal(err)
	}
	entry.setRedirect(newHolder.addr(), 7)
	if _, err := c.activeChecks(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A reply that overtook a newer one must not move us backwards.
	if c.follow(redirection{Address: oldHolder.addr(), Revision: 3}) {
		t.Fatal("an older revision was followed")
	}
	if got := c.target(); got != newHolder.addr() {
		t.Fatalf("target = %s, want the holder named by the newer revision", got)
	}
}

func TestWhenTheHoldingMemberGoesDownTheAgentReturnsToTheConfiguredAddresses(t *testing.T) {
	holder := newFakeServer(t)
	entry := newFakeServer(t)
	entry.setRedirect(holder.addr(), 1)

	c, err := newClient(testConfig(entry.addr()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.activeChecks(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.target() != holder.addr() {
		t.Fatalf("target = %s, want the holder", c.target())
	}

	// The member holding the host goes down. The group reassigns it, but
	// the agent only learns that by asking a configured address again.
	holder.close()
	entry.clearRedirect()
	entry.setItems("senhub.k[p]")
	if _, err := c.activeChecks(context.Background()); err == nil {
		t.Fatal("the dead member answered")
	}
	if got := c.target(); got != entry.addr() {
		t.Fatalf("target = %s, want the configured address back", got)
	}
	items, err := c.activeChecks(context.Background())
	if err != nil {
		t.Fatalf("the agent did not recover: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
}

func TestSeveralConfiguredAddressesAreTriedInTurn(t *testing.T) {
	dead := newFakeServer(t)
	deadAddr := dead.addr()
	dead.close()
	alive := newFakeServer(t)
	alive.setItems("senhub.k[p]")

	c, err := newClient(testConfigServers(deadAddr, alive.addr()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.activeChecks(context.Background()); err == nil {
		t.Fatal("a closed listener answered")
	}
	items, err := c.activeChecks(context.Background())
	if err != nil {
		t.Fatalf("the second address was not tried: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
}

func TestParseServersReadsAListAndFillsInThePort(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"zbx.example.com", []string{"zbx.example.com:10051"}},
		{"zbx.example.com:10061", []string{"zbx.example.com:10061"}},
		{"a:10051, b:10052 ,c", []string{"a:10051", "b:10052", "c:10051"}},
		{"a:10051,a:10051", []string{"a:10051"}},
		{"[::1]:10051", []string{"[::1]:10051"}},
	}
	for _, tc := range cases {
		got, err := parseServers(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q -> %v, want %v", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{"", "   ", " , , "} {
		if _, err := parseServers(bad); err == nil {
			t.Fatalf("%q was accepted", bad)
		}
	}
}

func TestThePassiveAllowListCoversEveryConfiguredAddress(t *testing.T) {
	cfg := testConfigServers("127.0.0.1:10051", "127.0.0.2:10051")
	cfg.Passive = PassiveConfig{Enabled: true, Port: 10050}
	nets, err := allowedNets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"127.0.0.1", "127.0.0.2"} {
		found := false
		for _, n := range nets {
			if n.IP.String() == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s may not poll the passive port; every member of a proxy group must be allowed", want)
		}
	}
}
