package kubernetes

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// The cluster's stable identity.
//
// The entity was keyed `kubernetes://<api-server-address>` — a URL, which is
// a property of the path taken to reach the cluster, not of the cluster. Our
// own rule for service.instance forbids it in those words: never
// scheme://host:port, URL+port, or IP.
//
// The consequences are not theoretical. An HA endpoint failing over, a DNS
// change, or a kubeconfig repointed at a different load balancer all re-key
// the entity: the cluster dies in the graph and a new one appears, taking its
// history with it. In the other direction, two clusters reachable at the same
// address — a dev and a prod behind the same local proxy — collapse onto one
// node.
//
// Kubernetes reports a stable identifier: the UID of the kube-system
// namespace, created once at cluster bootstrap and never reissued. It is what
// k8sclusterreceiver publishes as k8s.cluster.uid, so a backend joining our
// entity to their metrics finds the same value.
const kubeSystemNamespace = "kube-system"

// clusterUID reads the kube-system namespace UID.
//
// It is fetched once at start rather than per cycle: the value cannot change
// for the life of a cluster, and a probe that re-derived it every cycle would
// turn a transient API error into an identity change — the very churn this is
// meant to prevent.
func clusterUID(ctx context.Context, cs kubernetes.Interface) (string, error) {
	ns, err := cs.CoreV1().Namespaces().Get(ctx, kubeSystemNamespace, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("reading %s namespace UID: %w", kubeSystemNamespace, err)
	}
	uid := string(ns.UID)
	if uid == "" {
		return "", fmt.Errorf("%s namespace reports an empty UID", kubeSystemNamespace)
	}
	return uid, nil
}

// resolveClusterIdentity fetches the UID with a short timeout of its own.
//
// A failure here is not fatal to the probe: metrics are still worth
// collecting, and the caller falls back to the address-derived identity with a
// warning rather than emitting nothing. But the fallback is explicitly a
// degraded mode — an operator whose RBAC denies reading kube-system should see
// why their cluster entity is unstable, instead of discovering it months later
// as unexplained churn in the graph.
func resolveClusterIdentity(cs kubernetes.Interface, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return clusterUID(ctx, cs)
}
