package kubernetes

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "kubernetes", DisplayName: "Kubernetes", Category: "containers",
		Summary:  "Nodes, pods, containers, workloads, storage, quotas, autoscalers and events of the cluster the agent belongs to or a kubeconfig points at.",
		DocsPath: "docs/user-guide/docs/probes/kubernetes.md", MultiInstance: true, DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "kubeconfig", Kind: probes.KindString, Group: "connection", Description: "Path of a kubeconfig file; empty uses the in-cluster service account. One instance per cluster: the identity comes from the cluster, so two instances pointing at two clusters do not collide", Example: "/home/agent/.kube/config"},
			{Key: "interval", Kind: probes.KindInt, Default: 30, Group: "collection", Description: "Seconds between collections"},
			{Key: "namespaces", Kind: probes.KindBlock, Group: "filter", Description: "Namespace selection", Fields: []probes.ParamSpec{
				{Key: "include", Kind: probes.KindStringList, Description: "Namespaces to watch; empty means all"},
				{Key: "exclude", Kind: probes.KindStringList, Default: []string{"kube-system"}, Description: "Namespaces to skip"},
			}},
			{Key: "collect", Kind: probes.KindBlock, Group: "detail", Description: "Resource kinds to collect", Fields: []probes.ParamSpec{
				{Key: "nodes", Kind: probes.KindBool, Default: true, Description: "Node readiness, capacity and pressure conditions"},
				{Key: "pods", Kind: probes.KindBool, Default: true, Description: "Pod phase, readiness, restarts and resource requests"},
				{Key: "containers", Kind: probes.KindBool, Default: true, Description: "Per-container state, waiting reason and resources"},
				{Key: "deployments", Kind: probes.KindBool, Default: true, Description: "Deployment replica health"},
				{Key: "statefulsets", Kind: probes.KindBool, Default: true, Description: "StatefulSet replica health"},
				{Key: "daemonsets", Kind: probes.KindBool, Default: true, Description: "DaemonSet scheduling health"},
				{Key: "replicasets", Kind: probes.KindBool, Default: false, Advanced: true, Description: "ReplicaSet replicas; off because each Deployment revision keeps one"},
				{Key: "jobs", Kind: probes.KindBool, Default: true, Description: "Job active, succeeded and failed counts"},
				{Key: "cronjobs", Kind: probes.KindBool, Default: true, Description: "CronJob active jobs and suspended state"},
				{Key: "storage", Kind: probes.KindBool, Default: true, Description: "PersistentVolumes and claims"},
				{Key: "quotas", Kind: probes.KindBool, Default: true, Description: "ResourceQuota limits and use"},
				{Key: "autoscalers", Kind: probes.KindBool, Default: true, Description: "HorizontalPodAutoscaler replica counts"},
				{Key: "events", Kind: probes.KindBool, Default: true, Description: "Cluster events, published on the log rail"},
			}},
		},
	})
}
