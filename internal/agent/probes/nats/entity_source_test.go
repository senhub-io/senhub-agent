package nats

import "testing"

func TestBuildServiceInstanceID(t *testing.T) {
	cases := []struct {
		endpoint string
		want     string
	}{
		{"http://localhost:8222", "nats://localhost:8222"},
		{"http://10.0.0.1:8222", "nats://10.0.0.1:8222"},
		{"http://nats.example.com:8222", "nats://nats.example.com:8222"},
		{"http://nats.example.com", "nats://nats.example.com"},
	}
	for _, tc := range cases {
		got := buildServiceInstanceID(tc.endpoint)
		if got != tc.want {
			t.Errorf("buildServiceInstanceID(%q): want %q got %q", tc.endpoint, tc.want, got)
		}
	}
}

func TestNATSEntitySource_BeforeAlive(t *testing.T) {
	src := newNATSEntitySource("http://localhost:8222")
	obs, ok := src.Observe()
	if ok {
		t.Error("Observe should return ok=false before first successful scrape")
	}
	if len(obs.Entities) != 0 {
		t.Error("Observation should be empty before first successful scrape")
	}
}

func TestNATSEntitySource_AfterAlive(t *testing.T) {
	src := newNATSEntitySource("http://localhost:8222")
	src.markAlive()

	obs, ok := src.Observe()
	if !ok {
		t.Error("Observe should return ok=true after markAlive")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.Type != "service.instance" {
		t.Errorf("entity type: want %q got %q", "service.instance", e.Type)
	}
	wantID := "nats://localhost:8222"
	if v, ok := e.ID["service.instance.id"]; !ok || v != wantID {
		t.Errorf("entity id service.instance.id: want %q got %v", wantID, v)
	}
}
