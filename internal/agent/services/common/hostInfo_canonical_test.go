package common

import (
	"testing"
	"time"

	"github.com/shirou/gopsutil/v3/host"
)

func withFakeHostname(t *testing.T, raw string) {
	t.Helper()
	sharedHostInfo.mu.Lock()
	oldFetch, oldValue := sharedHostInfo.fetch, sharedHostInfo.value
	sharedHostInfo.fetch = func() (*host.InfoStat, error) {
		return &host.InfoStat{Hostname: raw, HostID: "0123456789abcdef0123456789abcdef", OS: "linux"}, nil
	}
	sharedHostInfo.value = nil
	sharedHostInfo.fetched = time.Time{}
	sharedHostInfo.mu.Unlock()
	SetHostNameOverride("")
	t.Cleanup(func() {
		sharedHostInfo.mu.Lock()
		sharedHostInfo.fetch, sharedHostInfo.value = oldFetch, oldValue
		sharedHostInfo.mu.Unlock()
	})
}

// A host reported as an FQDN and one reported by its short name each get
// one name, identical on metrics, on the OTLP resource, on the host entity
// and from the exported helper the other emitters use (#603).
func TestCanonicalHostName_OneNameOnEverySignal(t *testing.T) {
	for _, raw := range []string{"Web01.Corp.Example.COM.", "WEB01", "web01"} {
		withFakeHostname(t, raw)

		want := canonicalHostname(raw)
		id, err := GetHostIdentity()
		if err != nil {
			t.Fatal(err)
		}
		attrs, err := GetHostResourceAttributes()
		if err != nil {
			t.Fatal(err)
		}
		tg, err := GetHostTags()
		if err != nil {
			t.Fatal(err)
		}
		var tagHost string
		for _, tag := range tg {
			if tag.Key == "host" {
				tagHost = tag.Value
			}
		}
		for source, got := range map[string]string{
			"entity":   id.Name,
			"resource": attrs["host.name"],
			"metrics":  tagHost,
			"helper":   CanonicalHostName(),
		} {
			if got != want || got == "" {
				t.Errorf("raw %q: %s host.name = %q, want %q", raw, source, got, want)
			}
		}
	}
}
