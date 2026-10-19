package kubernetes

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"senhub-agent.go/internal/agent/services/logger"
)

func TestPersistentNodeListFailureWarnsOnceAndRecovers(t *testing.T) {
	cs := kubefake.NewSimpleClientset()
	failing := true
	cs.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		if failing {
			return true, nil, errors.New("forbidden")
		}
		return false, nil, nil
	})
	p := buildTestProbe(t, cs, true, false, false)
	var buf bytes.Buffer
	base := zerolog.New(&buf).Level(zerolog.DebugLevel)
	p.moduleLogger = logger.NewModuleLogger(&base, "probe.kubernetes.test")

	for i := 0; i < 3; i++ {
		if _, err := p.Collect(); err != nil {
			t.Fatalf("Collect: %v", err)
		}
	}
	if n := strings.Count(buf.String(), `"level":"warn"`); n != 1 {
		t.Fatalf("expected one warn for three failing cycles, got %d:\n%s", n, buf.String())
	}

	failing = false
	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !strings.Contains(buf.String(), "node collection recovered") {
		t.Fatalf("expected a recovery line:\n%s", buf.String())
	}
}
