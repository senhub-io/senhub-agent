package kubeletstats

import (
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
)

const summaryFixture = `{
  "node": {
    "nodeName": "worker-1",
    "cpu": {"usageNanoCores": 1500000000},
    "memory": {"workingSetBytes": 1234, "availableBytes": 5678},
    "fs": {"usedBytes": 1000, "capacityBytes": 2000, "availableBytes": 1000},
    "network": {
      "rxBytes": 999999,
      "txBytes": 888888,
      "interfaces": [
        {"name": "eth0", "rxBytes": 1000, "txBytes": 2000},
        {"name": "eth1", "rxBytes": 3000, "txBytes": 4000}
      ]
    }
  },
  "pods": [
    {
      "podRef": {"name": "pod-a", "namespace": "default"},
      "cpu": {"usageNanoCores": 1250000000},
      "memory": {"workingSetBytes": 333},
      "network": {"rxBytes": 10, "txBytes": 20},
      "containers": [
        {"name": "app", "cpu": {"usageNanoCores": 750000000}, "memory": {"workingSetBytes": 111}},
        {"name": "sidecar", "cpu": {"usageNanoCores": 250000000}, "memory": {"workingSetBytes": 222}}
      ]
    },
    {
      "podRef": {"name": "system-pod", "namespace": "kube-system"},
      "cpu": {"usageNanoCores": 500000000},
      "memory": {"workingSetBytes": 999},
      "network": {"rxBytes": 100, "txBytes": 200},
      "containers": [
        {"name": "system", "cpu": {"usageNanoCores": 500000000}, "memory": {"workingSetBytes": 999}}
      ]
    },
    {
      "podRef": {"name": "pod-b", "namespace": "preview"},
      "cpu": {"usageNanoCores": 100000000},
      "memory": {"workingSetBytes": 444},
      "containers": [
        {"name": "worker", "memory": {"workingSetBytes": 444}}
      ]
    }
  ]
}`

const minimalSummaryJSON = `{
  "node": {"nodeName": "worker-1"},
  "pods": [
    {
      "podRef": {"name": "pod-a", "namespace": "default"},
      "containers": [
        {"name": "app", "cpu": {"usageNanoCores": 1000000000}, "memory": {"workingSetBytes": 10}}
      ]
    }
  ]
}`

func newTestProbe(t *testing.T, server *httptest.Server, tokenFile string) *KubeletStatsProbe {
	t.Helper()
	probe := &KubeletStatsProbe{
		BaseProbe: &types.BaseProbe{},
		cfg: probeConfig{
			Endpoint: server.URL,
			Interval: time.Minute,
		},
		endpoint:   server.URL,
		httpClient: server.Client(),
		tokenFile:  tokenFile,
		caFile:     filepath.Join(t.TempDir(), "ca.crt"),
	}
	probe.SetProbeType(ProbeType)
	return probe
}

func collectPoints(t *testing.T, probe *KubeletStatsProbe) []data_store.DataPoint {
	t.Helper()
	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}
	return points
}

func getPointByName(t *testing.T, points []data_store.DataPoint, name string) data_store.DataPoint {
	t.Helper()
	for _, p := range points {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("point %s not found", name)
	return data_store.DataPoint{}
}

func getPoint(t *testing.T, points []data_store.DataPoint, name, tagKey, tagValue string) data_store.DataPoint {
	t.Helper()
	for _, p := range points {
		if p.Name != name {
			continue
		}
		for _, tag := range p.Tags {
			if tag.Key == tagKey && tag.Value == tagValue {
				return p
			}
		}
	}
	t.Fatalf("point %s with %s=%s not found", name, tagKey, tagValue)
	return data_store.DataPoint{}
}

func hasPoint(points []data_store.DataPoint, name, tagKey, tagValue string) bool {
	for _, p := range points {
		if p.Name != name {
			continue
		}
		for _, tag := range p.Tags {
			if tag.Key == tagKey && tag.Value == tagValue {
				return true
			}
		}
	}
	return false
}

func assertFloat(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("value %f, want %f", got, want)
	}
}

func TestCollectNodeAndPodMetrics(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, summaryFixture)
	}))
	defer server.Close()

	probe := newTestProbe(t, server, tokenFile)
	points := collectPoints(t, probe)

	podsSeen := getPointByName(t, points, metricPodsSeen)
	assertFloat(t, podsSeen.Value, 3)

	nodeCPU := getPoint(t, points, "k8s.node.cpu.usage", "k8s.node.name", "worker-1")
	assertFloat(t, nodeCPU.Value, 1.5)

	nodeMemWorking := getPoint(t, points, "k8s.node.memory.working_set", "k8s.node.name", "worker-1")
	assertFloat(t, nodeMemWorking.Value, 1234)

	nodeMemAvail := getPoint(t, points, "k8s.node.memory.available", "k8s.node.name", "worker-1")
	assertFloat(t, nodeMemAvail.Value, 5678)

	nodeFSUsed := getPoint(t, points, "k8s.node.filesystem.usage", "k8s.node.name", "worker-1")
	assertFloat(t, nodeFSUsed.Value, 1000)
	nodeFSCapacity := getPoint(t, points, "k8s.node.filesystem.capacity", "k8s.node.name", "worker-1")
	assertFloat(t, nodeFSCapacity.Value, 2000)
	nodeFSAvail := getPoint(t, points, "k8s.node.filesystem.available", "k8s.node.name", "worker-1")
	assertFloat(t, nodeFSAvail.Value, 1000)

	nodeNetRX := getPoint(t, points, "k8s.node.network.io", "direction", "receive")
	// The default interface, not the sum: pod traffic would count twice.
	assertFloat(t, nodeNetRX.Value, 999999)
	nodeNetTX := getPoint(t, points, "k8s.node.network.io", "direction", "transmit")
	assertFloat(t, nodeNetTX.Value, 888888)

	podCPU := getPoint(t, points, "k8s.pod.cpu.usage", "k8s.pod.name", "pod-a")
	assertFloat(t, podCPU.Value, 1.25)
	podMem := getPoint(t, points, "k8s.pod.memory.working_set", "k8s.pod.name", "pod-a")
	assertFloat(t, podMem.Value, 333)
	podNetRX := getPoint(t, points, "k8s.pod.network.io", "direction", "receive")
	assertFloat(t, podNetRX.Value, 10)
	podNetTX := getPoint(t, points, "k8s.pod.network.io", "direction", "transmit")
	assertFloat(t, podNetTX.Value, 20)

	appCPU := getPoint(t, points, "container.cpu.usage", "k8s.container.name", "app")
	assertFloat(t, appCPU.Value, 0.75)
	sidecarCPU := getPoint(t, points, "container.cpu.usage", "k8s.container.name", "sidecar")
	assertFloat(t, sidecarCPU.Value, 0.25)
	workerMem := getPoint(t, points, "container.memory.working_set", "k8s.container.name", "worker")
	assertFloat(t, workerMem.Value, 444)

	if hasPoint(points, "container.cpu.usage", "k8s.container.name", "worker") {
		t.Fatal("worker CPU usage should be absent when usageNanoCores is missing")
	}
	if hasPoint(points, "k8s.pod.network.io", "k8s.pod.name", "pod-b") {
		t.Fatal("pod-b network metrics should be absent when network is missing")
	}
	if !hasPoint(points, "k8s.pod.cpu.usage", "k8s.pod.name", "system-pod") {
		t.Fatal("kube-system pod must be collected by default (auto-discovery: every pod)")
	}
}

func TestNamespaceFilter(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, summaryFixture)
	}))
	defer server.Close()

	probe := newTestProbe(t, server, tokenFile)
	probe.cfg.IncludeNamespaces = []string{"def*"}

	points := collectPoints(t, probe)

	podsSeen := getPointByName(t, points, metricPodsSeen)
	assertFloat(t, podsSeen.Value, 1)

	if hasPoint(points, "k8s.pod.cpu.usage", "k8s.pod.name", "pod-a") == false {
		t.Fatal("pod-a should be included by include_namespaces")
	}
	if hasPoint(points, "k8s.pod.cpu.usage", "k8s.pod.name", "pod-b") {
		t.Fatal("pod-b should be excluded by include_namespaces")
	}
}

func TestScrapeUnauthorized(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	probe := newTestProbe(t, server, tokenFile)
	points := collectPoints(t, probe)

	if len(points) != 1 {
		t.Fatalf("expected exactly 1 scrape error datapoint, got %d", len(points))
	}

	scrapeError := getPoint(t, points, metricScrapeErrors, "reason", "auth")
	assertFloat(t, scrapeError.Value, 1)

	if hasPoint(points, metricPodsSeen, "", "") {
		t.Fatal("pods_seen should not be emitted on a failed scrape")
	}
}

func TestTokenReread(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("token-1"), 0600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, minimalSummaryJSON)
	}))
	defer server.Close()

	probe := newTestProbe(t, server, tokenFile)

	firstPoints := collectPoints(t, probe)
	if len(firstPoints) != 1 {
		t.Fatalf("expected exactly 1 scrape error datapoint on stale token, got %d", len(firstPoints))
	}
	_ = getPoint(t, firstPoints, metricScrapeErrors, "reason", "auth")

	if err := os.WriteFile(tokenFile, []byte("token-2"), 0600); err != nil {
		t.Fatal(err)
	}

	secondPoints := collectPoints(t, probe)
	// The counter is cumulative: still 1 after a successful scrape.
	if p := getPoint(t, secondPoints, metricScrapeErrors, "reason", "auth"); p.Value != 1 {
		t.Fatalf("auth errors = %v after the token was re-read, want 1", p.Value)
	}
	_ = getPointByName(t, secondPoints, metricPodsSeen)
}
