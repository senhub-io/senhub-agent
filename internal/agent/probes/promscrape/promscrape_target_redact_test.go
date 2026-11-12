package promscrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDisplayTarget_PlainURLUnchanged(t *testing.T) {
	raw := "http://10.0.0.5:9100/metrics"
	if got := displayTarget(raw); got != raw {
		t.Fatalf("plain URL changed: got %q want %q", got, raw)
	}
}

func TestDisplayTarget_UserInfoRedacted(t *testing.T) {
	got := displayTarget("https://user:fake-pass@h.example:9090/metrics")
	if !strings.HasPrefix(got, "https://h.example:9090/metrics [") {
		t.Fatalf("unexpected display form: %q", got)
	}
	if strings.Contains(got, "user") || strings.Contains(got, "fake-pass") {
		t.Fatalf("credentials leaked in display form: %q", got)
	}
}

func TestDisplayTarget_QueryValuesRedacted(t *testing.T) {
	got := displayTarget("https://h.example/federate?match[]=up&token=fake-tok")
	if !strings.HasPrefix(got, "https://h.example/federate?match[]=***&token=*** [") {
		t.Fatalf("unexpected display form: %q", got)
	}
	if strings.Contains(got, "fake-tok") || strings.Contains(got, "match[]=up") {
		t.Fatalf("query secret leaked in display form: %q", got)
	}
}

func TestDisplayTarget_FragmentRedacted(t *testing.T) {
	got := displayTarget("http://h/metrics#token=fake-frag")
	if !strings.HasPrefix(got, "http://h/metrics [") {
		t.Fatalf("fragment URL not rebuilt with hash suffix: %q", got)
	}
	if !strings.Contains(got, " [") {
		t.Fatalf("fragment URL missing hash suffix: %q", got)
	}
	if strings.Contains(got, "fake-frag") || strings.Contains(got, "#") {
		t.Fatalf("fragment leaked in display form: %q", got)
	}
}

func TestDisplayTarget_SchemeLessOpaqueHashed(t *testing.T) {
	got := displayTarget("exporter:9100/metrics?token=fake-x")
	if !strings.HasPrefix(got, "[") {
		t.Fatalf("opaque/scheme-less target must start with hash bracket: %q", got)
	}
	if strings.Contains(got, "fake-x") || strings.Contains(got, "exporter:9100") {
		t.Fatalf("opaque raw target leaked: %q", got)
	}
}

func TestDisplayTarget_QueryValueDifferenceStaysDistinct(t *testing.T) {
	a := displayTarget("https://h.example/metrics?token=fake-one")
	b := displayTarget("https://h.example/metrics?token=fake-two")
	if a == b {
		t.Fatalf("targets differing only in hidden value collapsed: %q", a)
	}
}

func TestDisplayTarget_UserDifferenceStaysDistinct(t *testing.T) {
	a := displayTarget("http://alice@h.example/metrics")
	b := displayTarget("http://bob@h.example/metrics")
	if a == b {
		t.Fatalf("targets differing only in user part collapsed: %q", a)
	}
	if strings.Contains(a, "alice") || strings.Contains(b, "bob") {
		t.Fatalf("user part leaked in display form: %q / %q", a, b)
	}
}

func TestDisplayTarget_UnparsableNeverRaw(t *testing.T) {
	got := displayTarget("http://[::1")
	if !strings.HasPrefix(got, "[") {
		t.Fatalf("unparsable target must start with hash bracket: %q", got)
	}
	if strings.Contains(got, "::1") {
		t.Fatalf("unparsable raw target leaked: %q", got)
	}
}

func TestScrape_TargetSecretNotInTagsOrDisplay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain;version=0.0.4")
		_, _ = w.Write([]byte("# HELP fake_metric fake help\n# TYPE fake_metric gauge\nfake_metric 1\n"))
	}))
	defer srv.Close()

	target := srv.URL + "/metrics?token=fake-tok"
	p := newTestProbe(t, map[string]interface{}{
		"targets": []string{target},
	})

	for _, d := range p.config.displayTargets {
		if strings.Contains(d, "fake-tok") {
			t.Fatalf("parsed display target leaks secret: %q", d)
		}
	}

	points := collectFrom(t, p)
	if len(points) == 0 {
		t.Fatal("expected datapoints from scrape")
	}
	for _, dp := range points {
		for _, tag := range dp.Tags {
			if strings.Contains(tag.Value, "fake-tok") {
				t.Fatalf("tag %q leaks secret in value %q", tag.Key, tag.Value)
			}
		}
	}
}

func TestScrape_RefusedTargetErrorRedactsUserAndQuery(t *testing.T) {
	target := "http://user:fake-pass@127.0.0.1:1/metrics?token=fake-tok"
	p := newTestProbe(t, map[string]interface{}{
		"targets": []string{target},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	res := p.scrape(ctx, target)
	if res.err == nil {
		t.Fatal("expected an error scraping a refused target")
	}
	msg := res.err.Error()
	if strings.Contains(msg, "fake-pass") {
		t.Fatalf("error leaks raw password: %q", msg)
	}
	if strings.Contains(msg, "fake-tok") {
		t.Fatalf("error leaks raw query secret: %q", msg)
	}
	if strings.Contains(msg, "user:") {
		t.Fatalf("error leaks userinfo: %q", msg)
	}
}
