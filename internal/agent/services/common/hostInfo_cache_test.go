package common

import (
	"errors"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v3/host"
)

func TestHostInfoCache(t *testing.T) {
	now := time.Unix(1000, 0)
	calls := 0
	var failNext bool
	c := &hostInfoCache{
		ttl: hostInfoTTL,
		now: func() time.Time { return now },
		fetch: func() (*host.InfoStat, error) {
			calls++
			if failNext {
				return nil, errors.New("boom")
			}
			return &host.InfoStat{Hostname: "h", Procs: uint64(calls)}, nil
		},
	}

	for i := 0; i < 5; i++ {
		if _, err := c.get(); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("calls within TTL = %d, want 1", calls)
	}

	now = now.Add(hostInfoTTL + time.Second)
	info, err := c.get()
	if err != nil || calls != 2 || info.Procs != 2 {
		t.Fatalf("after TTL: calls=%d err=%v info=%+v", calls, err, info)
	}

	now = now.Add(hostInfoTTL + time.Second)
	failNext = true
	if _, err := c.get(); err == nil {
		t.Fatal("expected error")
	}
	failNext = false
	if _, err := c.get(); err != nil || calls != 4 {
		t.Fatalf("error must not be cached: calls=%d err=%v", calls, err)
	}
}
