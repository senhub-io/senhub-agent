package common

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// defaultCloudTimeout bounds the whole IMDS resolution. The metadata service is
// link-local (169.254.169.254) so a reachable provider answers in single-digit
// milliseconds; a non-cloud host has nothing listening and must fail fast.
const defaultCloudTimeout = 800 * time.Millisecond

// detectK8sNodeName returns the orchestrator node name when the agent runs as a
// Kubernetes pod with the node name injected via the downward API
// (spec.nodeName → NODE_NAME). "" when not on Kubernetes.
func detectK8sNodeName() string {
	return strings.TrimSpace(os.Getenv("NODE_NAME"))
}

// detectContainerRuntime returns the container runtime when the agent's own
// host is itself a container, derived from filesystem markers and cgroup
// hierarchy. "" when running directly on the host (no container).
func detectContainerRuntime() string {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "docker"
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		// The file podman drops; CRI-O/containerd under CRI also use it.
		return "podman"
	}
	if rt := runtimeFromCgroup("/proc/self/cgroup"); rt != "" {
		return rt
	}
	if rt := runtimeFromCgroup("/proc/1/cgroup"); rt != "" {
		return rt
	}
	return ""
}

// runtimeFromCgroup classifies the container runtime from a cgroup file's
// controller paths. The substrings are the cgroup path segments each runtime
// stamps. "" when the file is unreadable or carries no container marker.
func runtimeFromCgroup(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	c := string(b)
	switch {
	case strings.Contains(c, "containerd"):
		return "containerd"
	case strings.Contains(c, "docker"):
		return "docker"
	case strings.Contains(c, "crio") || strings.Contains(c, "crio-"):
		return "crio"
	case strings.Contains(c, "libpod") || strings.Contains(c, "podman"):
		return "podman"
	case strings.Contains(c, "/lxc/") || strings.Contains(c, "lxc.payload"):
		return "lxc"
	}
	return ""
}

// detectCloud queries the cloud metadata service to resolve cloud.provider and
// cloud.region. It tries AWS (IMDSv2), then GCP, then Azure; the first that
// answers wins. Any failure (no cloud, blocked IMDS, timeout) degrades silently
// to ("", "") — never an error. An explicit CLOUD_PROVIDER / CLOUD_REGION env
// override short-circuits the network probe entirely (operator escape hatch and
// the way tests exercise the resolver without a metadata endpoint).
func detectCloud(timeout time.Duration) (provider, region string) {
	if p := strings.TrimSpace(os.Getenv("CLOUD_PROVIDER")); p != "" {
		return p, strings.TrimSpace(os.Getenv("CLOUD_REGION"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	client := &http.Client{}

	if p, r := detectAWS(ctx, client); p != "" {
		return p, r
	}
	if p, r := detectGCP(ctx, client); p != "" {
		return p, r
	}
	if p, r := detectAzure(ctx, client); p != "" {
		return p, r
	}
	return "", ""
}

// imdsBase is overridable in tests to point the per-provider probes at a local
// httptest server instead of the real link-local endpoints.
var imdsBase = map[string]string{
	"aws":   "http://169.254.169.254",
	"gcp":   "http://metadata.google.internal",
	"azure": "http://169.254.169.254",
}

// detectAWS resolves region via IMDSv2 (token-gated). The availability-zone
// endpoint returns e.g. "eu-west-1a"; the region is that minus the trailing AZ
// letter.
func detectAWS(ctx context.Context, client *http.Client) (provider, region string) {
	tokReq, err := http.NewRequestWithContext(ctx, http.MethodPut, imdsBase["aws"]+"/latest/api/token", nil)
	if err != nil {
		return "", ""
	}
	tokReq.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "60")
	tokResp, err := client.Do(tokReq)
	if err != nil {
		return "", ""
	}
	token := readBody(tokResp)
	if token == "" {
		return "", ""
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		imdsBase["aws"]+"/latest/meta-data/placement/region", nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("X-aws-ec2-metadata-token", token)
	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	return "aws", readBody(resp)
}

// detectGCP resolves region from the GCP metadata server. The zone endpoint
// returns "projects/<num>/zones/<region>-<az>"; the region is the last path
// segment minus the trailing "-<az>".
func detectGCP(ctx context.Context, client *http.Client) (provider, region string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		imdsBase["gcp"]+"/computeMetadata/v1/instance/zone", nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	zone := readBody(resp)
	if zone == "" {
		return "", ""
	}
	if i := strings.LastIndex(zone, "/"); i >= 0 {
		zone = zone[i+1:]
	}
	if i := strings.LastIndex(zone, "-"); i >= 0 {
		zone = zone[:i]
	}
	return "gcp", zone
}

// detectAzure resolves region (location) from the Azure Instance Metadata
// Service, which requires the Metadata:true header and an api-version.
func detectAzure(ctx context.Context, client *http.Client) (provider, region string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		imdsBase["azure"]+"/metadata/instance/compute/location?api-version=2021-02-01&format=text", nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("Metadata", "true")
	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	return "azure", readBody(resp)
}

func readBody(resp *http.Response) string {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
