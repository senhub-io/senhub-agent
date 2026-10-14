package kubeletstats

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "kubelet_stats", DisplayName: "Kubelet Stats", Category: "containers",
		Summary:  "Per-node CPU, memory, filesystem and network usage from the local kubelet Summary API. Run as a DaemonSet.",
		DocsPath: "docs/user-guide/docs/probes/kubelet_stats.md", MultiInstance: false, DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Group: "connection", Description: "Base URL of the kubelet. Empty uses port 10250 on $SENHUB_K8S_NODE_IP, else on $SENHUB_K8S_NODE_NAME", Example: "https://127.0.0.1:10250"},
			{Key: "insecure_skip_verify", Kind: probes.KindBool, Default: false, Group: "connection", Description: "Skip TLS certificate verification. Kubelets often serve self-signed certificates; the proper fix is kubelet serving certificate rotation/approval, not enabling this permanently"},
			{Key: "interval", Kind: probes.KindInt, Default: 30, Group: "collection", Description: "Seconds between collections"},
			{Key: "include_namespaces", Kind: probes.KindStringList, Group: "filter", Description: "Namespace glob patterns to include; empty means all"},
			{Key: "exclude_namespaces", Kind: probes.KindStringList, Group: "filter", Description: "Namespace glob patterns to skip"},
		},
	})
}
