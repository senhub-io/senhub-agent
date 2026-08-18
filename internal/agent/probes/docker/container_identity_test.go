package docker

import (
	"strings"
	"testing"
	"time"
)

// #758: the entity is keyed on the full container sha while the metric label
// carried the 12-character CLI form, so a consumer querying by the entity's
// own identity got zero series. Measured by the consumer on production: 64
// characters on the entity rail, 12 on the metric rail.
//
// Truncation is a display convention chosen by the observer and it is
// destructive — the full id cannot be recovered from it. It is the same
// defect family as network.interface (#748) and db (#740): a value that looks
// like the identity, sits next to it, and joins nothing.
func TestContainerIDTagCarriesTheFullSha(t *testing.T) {
	const full = "9f2a1c4e7b3d5a6f8e0c2b4d6a8f1e3c5b7d9f0a2c4e6b8d0f2a4c6e8b0d2f4a"

	p := &dockerProbe{}
	res := statsResult{
		container: containerListItem{
			ID:    full,
			Names: []string{"/api"},
			Image: "nginx:1.27",
			State: "running",
		},
	}

	points := p.buildDatapoints(res, time.Now())
	if len(points) == 0 {
		t.Fatal("no datapoints built")
	}

	var seen string
	for _, tag := range points[0].Tags {
		if tag.Key == "container_id" {
			seen = tag.Value
		}
	}
	if seen == "" {
		t.Fatal("container_id tag not emitted")
	}
	if seen != full {
		t.Errorf("container_id = %q (%d chars), want the full sha (%d chars).\n"+
			"  The entity is keyed on the full id; a truncated label joins nothing.",
			seen, len(seen), len(full))
	}
	if len(seen) != 64 {
		t.Errorf("container_id is %d characters, want 64", len(seen))
	}
	if seen != strings.ToLower(seen) {
		t.Errorf("container_id = %q, want lowercase — the canonical form agreed "+
			"with the consumer", seen)
	}
}

// shortID stays, for the one place a human reads it: the fallback name of an
// unnamed container. It must not creep back into an identity-bearing tag.
func TestShortIDIsDisplayOnly(t *testing.T) {
	const full = "9f2a1c4e7b3d5a6f8e0c2b4d6a8f1e3c5b7d9f0a2c4e6b8d0f2a4c6e8b0d2f4a"

	if got := primaryName(containerListItem{ID: full}); got != full[:12] {
		t.Errorf("unnamed container falls back to %q, want the short form for readability", got)
	}
	if got := primaryName(containerListItem{ID: full, Names: []string{"/api"}}); got != "api" {
		t.Errorf("named container = %q, want api", got)
	}
}
