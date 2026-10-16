package probes

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/entity"
)

type pollerTestSource struct {
	obs entity.Observation
}

func (s pollerTestSource) Observe() (entity.Observation, bool) {
	return s.obs, true
}

type pollerTestCadenceSource struct{}

func (s pollerTestCadenceSource) Observe() (entity.Observation, bool) {
	return entity.Observation{}, true
}

func (s pollerTestCadenceSource) Cadence() time.Duration {
	return 7 * time.Minute
}

func sourceAsForTest[T any](src entity.Source) (T, bool) {
	var zero T
	for src != nil {
		if t, ok := src.(T); ok {
			return t, true
		}
		u, ok := src.(interface{ Unwrap() entity.Source })
		if !ok {
			break
		}
		src = u.Unwrap()
	}
	return zero, false
}

func TestProbeCadenceSourceExposesPollIntervalThroughUnwrap(t *testing.T) {
	wrapped := probeCadenceSource{src: pollerTestSource{}, interval: 5 * time.Minute}
	attributed := entity.WithAttributesFunc(wrapped, func() map[string]any { return nil })

	cad, ok := sourceAsForTest[entity.CadenceSource](attributed)
	if !ok {
		t.Fatal("CadenceSource not found through WithAttributesFunc and Unwrap chain")
	}
	if got := cad.Cadence(); got != 5*time.Minute {
		t.Fatalf("Cadence() = %s, want %s", got, 5*time.Minute)
	}
}

func TestImplementsCadenceSourceFindsExisting(t *testing.T) {
	if !implementsCadenceSource(pollerTestCadenceSource{}) {
		t.Fatal("implementsCadenceSource returned false for a source that already declares Cadence")
	}
	if implementsCadenceSource(pollerTestSource{}) {
		t.Fatal("implementsCadenceSource returned true for a source without cadence")
	}
}
