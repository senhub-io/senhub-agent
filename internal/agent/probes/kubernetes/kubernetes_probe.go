// Package kubernetes implements the free kubernetes probe: supervision of
// Kubernetes nodes, pods, containers, and deployments via the Kubernetes
// API server and the metrics-server API.
//
// Authentication is via in-cluster ServiceAccount (when kubeconfig is empty)
// or an explicit kubeconfig file for out-of-cluster operation. Namespace
// filtering (include / exclude) applies to pod and deployment collection.
package kubernetes

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier used in YAML config and
// license JWT claims.
const ProbeType = "kubernetes"

const (
	defaultInterval = 30 * time.Second

	// metricUp is 1 when the last collection reached the API server, 0 otherwise.
	metricUp = "senhub.kubernetes.up"
)

// probeConfig holds the parsed kubernetes probe configuration.
type probeConfig struct {
	Kubeconfig         string
	CollectNodes       bool
	CollectPods        bool
	CollectContainers  bool
	CollectDeployments bool
	IncludeNamespaces  []string
	ExcludeNamespaces  map[string]bool
	Interval           time.Duration
}

// KubernetesProbe collects metrics from a Kubernetes cluster.
type KubernetesProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger
	clientset    kubernetes.Interface
	// clusterEndpoint identifies this cluster in entity IDs.
	clusterEndpoint  string
	entitySrc        *k8sEntitySource
	unregisterEntity func()
}

// NewKubernetesProbe constructs the probe. Config errors surface here.
func NewKubernetesProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.kubernetes")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	probe := &KubernetesProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

func parseConfig(config map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		CollectNodes:       true,
		CollectPods:        true,
		CollectContainers:  true,
		CollectDeployments: true,
		Interval:           defaultInterval,
	}

	if v, ok := config["kubeconfig"].(string); ok {
		cfg.Kubeconfig = v
	}

	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}

	if collect, ok := config["collect"].(map[string]interface{}); ok {
		if v, ok := collect["nodes"].(bool); ok {
			cfg.CollectNodes = v
		}
		if v, ok := collect["pods"].(bool); ok {
			cfg.CollectPods = v
		}
		if v, ok := collect["containers"].(bool); ok {
			cfg.CollectContainers = v
		}
		if v, ok := collect["deployments"].(bool); ok {
			cfg.CollectDeployments = v
		}
	}

	if ns, ok := config["namespaces"].(map[string]interface{}); ok {
		if inc, ok := ns["include"].([]interface{}); ok {
			for _, v := range inc {
				if s, ok := v.(string); ok && s != "" {
					cfg.IncludeNamespaces = append(cfg.IncludeNamespaces, s)
				}
			}
		}
		if exc, ok := ns["exclude"].([]interface{}); ok {
			cfg.ExcludeNamespaces = make(map[string]bool, len(exc))
			for _, v := range exc {
				if s, ok := v.(string); ok && s != "" {
					cfg.ExcludeNamespaces[s] = true
				}
			}
		}
	}
	// Default exclude kube-system when nothing is configured.
	if cfg.ExcludeNamespaces == nil {
		cfg.ExcludeNamespaces = map[string]bool{"kube-system": true}
	}

	return cfg, nil
}

func (p *KubernetesProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *KubernetesProbe) ShouldStart() bool          { return true }
func (p *KubernetesProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *KubernetesProbe) OnStart(_ chan struct{}) error {
	k8sCfg, clusterEndpoint, err := buildClientConfig(p.cfg.Kubeconfig)
	if err != nil {
		return fmt.Errorf("kubernetes probe: building client config: %w", err)
	}
	p.clusterEndpoint = clusterEndpoint

	cs, err := kubernetes.NewForConfig(k8sCfg)
	if err != nil {
		return fmt.Errorf("kubernetes probe: creating clientset: %w", err)
	}
	p.clientset = cs

	p.entitySrc = newK8sEntitySource(p.clusterEndpoint)
	p.unregisterEntity = registerEntitySource(p.entitySrc)

	p.moduleLogger.Info().
		Str("cluster", p.clusterEndpoint).
		Bool("nodes", p.cfg.CollectNodes).
		Bool("pods", p.cfg.CollectPods).
		Bool("containers", p.cfg.CollectContainers).
		Bool("deployments", p.cfg.CollectDeployments).
		Msg("Starting kubernetes probe")
	return nil
}

func (p *KubernetesProbe) OnShutdown(_ context.Context) error {
	if p.unregisterEntity != nil {
		p.unregisterEntity()
	}
	return nil
}

// Collect gathers metrics from the Kubernetes API. A partial failure
// (e.g. one resource type unavailable) is logged but does not abort
// the rest of the collection.
//
// senhub.kubernetes.up is emitted on every cycle: 0 before a successful
// API call, 1 after at least one list succeeds. This ensures that an
// unreachable API server or expired credentials produce an observable
// up=0 series rather than vanishing from all sinks.
func (p *KubernetesProbe) Collect() ([]data_store.DataPoint, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Interval/2)
	defer cancel()

	now := time.Now()
	up := float32(0)
	var points []data_store.DataPoint
	upTags := []tags.Tag{
		{Key: "k8s.cluster.name", Value: p.clusterEndpoint},
		{Key: "metric_type", Value: "availability"},
	}

	if p.cfg.CollectNodes {
		pts, err := p.collectNodes(ctx, now)
		if err != nil {
			p.moduleLogger.Warn().Err(err).Msg("kubernetes: node collection failed")
		} else {
			up = 1
		}
		points = append(points, pts...)
	}

	if p.cfg.CollectPods || p.cfg.CollectContainers {
		pts, err := p.collectPods(ctx, now)
		if err != nil {
			p.moduleLogger.Warn().Err(err).Msg("kubernetes: pod collection failed")
		} else {
			up = 1
		}
		points = append(points, pts...)
	}

	if p.cfg.CollectDeployments {
		pts, err := p.collectDeployments(ctx, now)
		if err != nil {
			p.moduleLogger.Warn().Err(err).Msg("kubernetes: deployment collection failed")
		} else {
			up = 1
		}
		points = append(points, pts...)
	}

	points = append(points, data_store.DataPoint{
		Name: metricUp, Value: up, Timestamp: now, Tags: upTags,
	})

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// collectNodes lists all nodes and emits per-node metrics.
func (p *KubernetesProbe) collectNodes(ctx context.Context, now time.Time) ([]data_store.DataPoint, error) {
	nodes, err := p.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	var points []data_store.DataPoint
	for i := range nodes.Items {
		n := &nodes.Items[i]
		nodeName := n.Name
		baseTags := []tags.Tag{
			{Key: "k8s.node.name", Value: nodeName},
			{Key: "k8s.cluster.name", Value: p.clusterEndpoint},
			{Key: "metric_type", Value: "node"},
		}

		ready := float32(0)
		for _, cond := range n.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				ready = 1
				break
			}
		}
		points = append(points, data_store.DataPoint{
			Name: "k8s.node.ready", Value: ready, Timestamp: now, Tags: baseTags,
		})

		if cpu := n.Status.Allocatable.Cpu(); cpu != nil {
			points = append(points, data_store.DataPoint{
				Name:      "k8s.node.cpu.allocatable",
				Value:     float32(cpu.AsApproximateFloat64()),
				Timestamp: now,
				Tags:      baseTags,
			})
		}
		if mem := n.Status.Allocatable.Memory(); mem != nil {
			points = append(points, data_store.DataPoint{
				Name:      "k8s.node.memory.allocatable",
				Value:     float32(mem.Value()),
				Timestamp: now,
				Tags:      baseTags,
			})
		}
		if pods := n.Status.Capacity.Pods(); pods != nil {
			cap, _ := pods.AsInt64()
			points = append(points, data_store.DataPoint{
				Name: "k8s.node.pods.capacity", Value: float32(cap), Timestamp: now, Tags: baseTags,
			})
		}
		if pods := n.Status.Allocatable.Pods(); pods != nil {
			alloc, _ := pods.AsInt64()
			points = append(points, data_store.DataPoint{
				Name: "k8s.node.pods.allocated", Value: float32(alloc), Timestamp: now, Tags: baseTags,
			})
		}
	}
	return points, nil
}

// collectPods lists pods across the filtered namespace set and emits
// pod-level and (when enabled) container-level metrics.
func (p *KubernetesProbe) collectPods(ctx context.Context, now time.Time) ([]data_store.DataPoint, error) {
	namespaces, err := p.resolveNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	var points []data_store.DataPoint
	for _, ns := range namespaces {
		pods, err := p.clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			p.moduleLogger.Warn().Err(err).Str("namespace", ns).Msg("kubernetes: listing pods")
			continue
		}
		for i := range pods.Items {
			pod := &pods.Items[i]
			if p.cfg.CollectPods {
				points = append(points, p.buildPodPoints(pod, now)...)
			}
			if p.cfg.CollectContainers {
				points = append(points, p.buildContainerPoints(pod, now)...)
			}
		}
	}
	return points, nil
}

func (p *KubernetesProbe) buildPodPoints(pod *corev1.Pod, now time.Time) []data_store.DataPoint {
	baseTags := []tags.Tag{
		{Key: "k8s.pod.name", Value: pod.Name},
		{Key: "k8s.namespace.name", Value: pod.Namespace},
		{Key: "k8s.node.name", Value: pod.Spec.NodeName},
		{Key: "metric_type", Value: "pod"},
	}

	running := float32(0)
	if pod.Status.Phase == corev1.PodRunning {
		running = 1
	}

	ready := float32(0)
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			ready = 1
			break
		}
	}

	var totalRestarts int32
	for _, cs := range pod.Status.ContainerStatuses {
		totalRestarts += cs.RestartCount
	}

	return []data_store.DataPoint{
		{Name: "k8s.pod.phase", Value: running, Timestamp: now, Tags: baseTags},
		{Name: "k8s.pod.ready", Value: ready, Timestamp: now, Tags: baseTags},
		{Name: "k8s.pod.restarts", Value: float32(totalRestarts), Timestamp: now, Tags: baseTags},
	}
}

func (p *KubernetesProbe) buildContainerPoints(pod *corev1.Pod, now time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint
	for _, cs := range pod.Status.ContainerStatuses {
		baseTags := []tags.Tag{
			{Key: "k8s.container.name", Value: cs.Name},
			{Key: "k8s.pod.name", Value: pod.Name},
			{Key: "k8s.namespace.name", Value: pod.Namespace},
			{Key: "metric_type", Value: "container"},
		}

		ready := float32(0)
		if cs.Ready {
			ready = 1
		}

		points = append(points,
			data_store.DataPoint{Name: "k8s.container.ready", Value: ready, Timestamp: now, Tags: baseTags},
			data_store.DataPoint{Name: "k8s.container.restarts", Value: float32(cs.RestartCount), Timestamp: now, Tags: baseTags},
		)
	}
	return points
}

// collectDeployments lists deployments across the filtered namespace set.
func (p *KubernetesProbe) collectDeployments(ctx context.Context, now time.Time) ([]data_store.DataPoint, error) {
	namespaces, err := p.resolveNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	var points []data_store.DataPoint
	for _, ns := range namespaces {
		deps, err := p.clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			p.moduleLogger.Warn().Err(err).Str("namespace", ns).Msg("kubernetes: listing deployments")
			continue
		}
		for i := range deps.Items {
			dep := &deps.Items[i]
			baseTags := []tags.Tag{
				{Key: "k8s.deployment.name", Value: dep.Name},
				{Key: "k8s.namespace.name", Value: dep.Namespace},
				{Key: "metric_type", Value: "deployment"},
			}

			desired := dep.Spec.Replicas
			var desiredCount int32
			if desired != nil {
				desiredCount = *desired
			}
			available := dep.Status.AvailableReplicas

			depReady := float32(0)
			if available >= desiredCount {
				depReady = 1
			}

			points = append(points,
				data_store.DataPoint{Name: "k8s.deployment.available", Value: float32(available), Timestamp: now, Tags: baseTags},
				data_store.DataPoint{Name: "k8s.deployment.desired", Value: float32(desiredCount), Timestamp: now, Tags: baseTags},
				data_store.DataPoint{Name: "k8s.deployment.ready", Value: depReady, Timestamp: now, Tags: baseTags},
			)
		}
	}
	return points, nil
}

// resolveNamespaces returns the effective list of namespaces to query.
// When IncludeNamespaces is non-empty, only those are returned (minus
// the exclusion list). When empty, all cluster namespaces are listed and
// the exclusion list is applied.
func (p *KubernetesProbe) resolveNamespaces(ctx context.Context) ([]string, error) {
	if len(p.cfg.IncludeNamespaces) > 0 {
		out := make([]string, 0, len(p.cfg.IncludeNamespaces))
		for _, ns := range p.cfg.IncludeNamespaces {
			if !p.cfg.ExcludeNamespaces[ns] {
				out = append(out, ns)
			}
		}
		return out, nil
	}

	nsList, err := p.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing namespaces: %w", err)
	}
	out := make([]string, 0, len(nsList.Items))
	for _, ns := range nsList.Items {
		if !p.cfg.ExcludeNamespaces[ns.Name] {
			out = append(out, ns.Name)
		}
	}
	return out, nil
}

// buildClientConfig returns a *rest.Config and a human-readable cluster
// endpoint string for logging and entity IDs.
func buildClientConfig(kubeconfig string) (*rest.Config, string, error) {
	if kubeconfig == "" {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			return nil, "", fmt.Errorf("in-cluster config: %w", err)
		}
		endpoint := clusterEndpointFromHost(cfg.Host)
		return cfg, endpoint, nil
	}

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, "", fmt.Errorf("kubeconfig %q: %w", kubeconfig, err)
	}
	endpoint := clusterEndpointFromHost(cfg.Host)
	return cfg, endpoint, nil
}

// clusterEndpointFromHost strips the scheme from the API server URL and
// returns the host[:port] as a stable identifier.
func clusterEndpointFromHost(host string) string {
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	if host == "" {
		return "localhost"
	}
	return host
}
