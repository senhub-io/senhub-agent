package macindex

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestIndexRegisterLookup(t *testing.T) {
	idx := New(time.Minute)
	idx.Register("aa-bb-cc-dd-ee-ff", "dev-1", "Ethernet1")

	got, ok := idx.Lookup("AA-BB-CC-DD-EE-FF")
	if !ok {
		t.Fatal("Lookup returned ok=false, want true")
	}
	if got.DeviceID != "dev-1" || got.Name != "Ethernet1" {
		t.Fatalf("Lookup = %#v, want {dev-1 Ethernet1}", got)
	}

	if _, ok := idx.Lookup("00-00-00-00-00-00"); ok {
		t.Fatal("Lookup for unknown MAC returned ok=true")
	}
}

func TestIndexTTLExpiry(t *testing.T) {
	idx := New(time.Minute)
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	idx.now = func() time.Time { return now }

	idx.Register("aa-bb-cc-dd-ee-ff", "dev-1", "Ethernet1")
	if _, ok := idx.Lookup("AA-BB-CC-DD-EE-FF"); !ok {
		t.Fatal("Lookup before TTL returned ok=false")
	}

	now = now.Add(time.Minute)
	if _, ok := idx.Lookup("AA-BB-CC-DD-EE-FF"); ok {
		t.Fatal("Lookup after TTL returned ok=true")
	}
}

func TestIndexFlagDefaultFalse(t *testing.T) {
	idx := New(time.Minute)
	if idx.Enabled() {
		t.Fatal("new index enabled flag = true, want false")
	}
}

func TestIndexConcurrency(t *testing.T) {
	idx := New(time.Hour)
	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				mac := fmt.Sprintf("%02X-%02X-%02X-%02X-%02X-%02X",
					byte(base), byte(i), 0, 0, 0, byte(base+i))
				idx.Register(mac, fmt.Sprintf("dev-%d", base%3), "Ethernet1")
				got, ok := idx.Lookup(mac)
				if !ok {
					t.Errorf("Lookup(%s) returned ok=false", mac)
					continue
				}
				if got.DeviceID == "" || got.Name == "" {
					t.Errorf("Lookup(%s) = %#v, want non-empty device and name", mac, got)
				}
			}
		}(g)
	}
	wg.Wait()
}
