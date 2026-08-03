package common

import (
	"context"
	"encoding/json"
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

// cloudInfo is the cloud nameplate resolved from the metadata service. Every
// field is best-effort: "" means unresolved and is omitted downstream.
type cloudInfo struct {
	provider         string // cloud.provider — aws/gcp/azure
	region           string // cloud.region
	availabilityZone string // cloud.availability_zone
	instanceType     string // host.type — provider instance/machine type
	accountID        string // cloud.account.id — AWS account / GCP project number / Azure subscription
}

// detectCloud queries the cloud metadata service to resolve the cloud
// nameplate. It tries AWS (IMDSv2), then GCP, then Azure; the first that
// answers wins. Any failure (no cloud, blocked IMDS, timeout) degrades silently
// to the zero cloudInfo — never an error. An explicit CLOUD_PROVIDER env
// override short-circuits the network probe entirely (operator escape hatch and
// the way tests exercise the resolver without a metadata endpoint); the
// companion CLOUD_REGION / CLOUD_AVAILABILITY_ZONE / CLOUD_ACCOUNT_ID /
// HOST_TYPE overrides fill the rest of the nameplate.
func detectCloud(timeout time.Duration) cloudInfo {
	if p := strings.TrimSpace(os.Getenv("CLOUD_PROVIDER")); p != "" {
		return cloudInfo{
			provider:         p,
			region:           strings.TrimSpace(os.Getenv("CLOUD_REGION")),
			availabilityZone: strings.TrimSpace(os.Getenv("CLOUD_AVAILABILITY_ZONE")),
			instanceType:     strings.TrimSpace(os.Getenv("HOST_TYPE")),
			accountID:        strings.TrimSpace(os.Getenv("CLOUD_ACCOUNT_ID")),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	client := &http.Client{}

	if ci := detectAWS(ctx, client); ci.provider != "" {
		return ci
	}
	if ci := detectGCP(ctx, client); ci.provider != "" {
		return ci
	}
	if ci := detectAzure(ctx, client); ci.provider != "" {
		return ci
	}
	return cloudInfo{}
}

// imdsBase is overridable in tests to point the per-provider probes at a local
// httptest server instead of the real link-local endpoints.
var imdsBase = map[string]string{
	"aws":   "http://169.254.169.254",
	"gcp":   "http://metadata.google.internal",
	"azure": "http://169.254.169.254",
}

// awsIdentityDocument is the subset of the EC2 instance identity document the
// nameplate needs; one call carries region, AZ, instance type and account id.
type awsIdentityDocument struct {
	Region           string `json:"region"`
	AvailabilityZone string `json:"availabilityZone"`
	InstanceType     string `json:"instanceType"`
	AccountID        string `json:"accountId"`
}

// detectAWS resolves the cloud nameplate via IMDSv2 (token-gated). The instance
// identity document carries region, availabilityZone, instanceType and
// accountId in a single call; when it is unavailable the plain region endpoint
// is the fallback so region resolution never regresses to less than what the
// two-endpoint resolver delivered.
func detectAWS(ctx context.Context, client *http.Client) cloudInfo {
	tokReq, err := http.NewRequestWithContext(ctx, http.MethodPut, imdsBase["aws"]+"/latest/api/token", nil)
	if err != nil {
		return cloudInfo{}
	}
	tokReq.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "60")
	tokResp, err := client.Do(tokReq)
	if err != nil {
		return cloudInfo{}
	}
	token := readBody(tokResp)
	if token == "" {
		return cloudInfo{}
	}

	if ci, ok := awsFromIdentityDocument(ctx, client, token); ok {
		return ci
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		imdsBase["aws"]+"/latest/meta-data/placement/region", nil)
	if err != nil {
		return cloudInfo{}
	}
	req.Header.Set("X-aws-ec2-metadata-token", token)
	resp, err := client.Do(req)
	if err != nil {
		return cloudInfo{}
	}
	return cloudInfo{provider: "aws", region: readBody(resp)}
}

// awsFromIdentityDocument fetches and parses the EC2 instance identity
// document. ok is false when the document cannot be fetched, parsed, or
// carries no region — the caller then falls back to the region endpoint.
func awsFromIdentityDocument(ctx context.Context, client *http.Client, token string) (cloudInfo, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		imdsBase["aws"]+"/latest/dynamic/instance-identity/document", nil)
	if err != nil {
		return cloudInfo{}, false
	}
	req.Header.Set("X-aws-ec2-metadata-token", token)
	resp, err := client.Do(req)
	if err != nil {
		return cloudInfo{}, false
	}
	body := readBody(resp)
	if body == "" {
		return cloudInfo{}, false
	}
	var doc awsIdentityDocument
	if err := json.Unmarshal([]byte(body), &doc); err != nil || doc.Region == "" {
		return cloudInfo{}, false
	}
	return cloudInfo{
		provider:         "aws",
		region:           doc.Region,
		availabilityZone: doc.AvailabilityZone,
		instanceType:     doc.InstanceType,
		accountID:        doc.AccountID,
	}, true
}

// detectGCP resolves the cloud nameplate from the GCP metadata server. The
// zone endpoint returns "projects/<num>/zones/<region>-<az>": the availability
// zone is the last path segment (e.g. "us-central1-a") and the region is that
// minus the trailing "-<az>". Machine type and the numeric project id are
// additional best-effort calls; the provider is set as soon as the zone
// resolves, so their failure cannot regress region resolution.
func detectGCP(ctx context.Context, client *http.Client) cloudInfo {
	zone := gcpMetadata(ctx, client, "/computeMetadata/v1/instance/zone")
	if zone == "" {
		return cloudInfo{}
	}
	az := lastPathSegment(zone)
	region := az
	if i := strings.LastIndex(region, "-"); i >= 0 {
		region = region[:i]
	}
	return cloudInfo{
		provider:         "gcp",
		region:           region,
		availabilityZone: az,
		instanceType:     lastPathSegment(gcpMetadata(ctx, client, "/computeMetadata/v1/instance/machine-type")),
		accountID:        gcpMetadata(ctx, client, "/computeMetadata/v1/project/numeric-project-id"),
	}
}

// gcpMetadata fetches one GCP metadata path with the mandatory
// Metadata-Flavor header. "" on any failure.
func gcpMetadata(ctx context.Context, client *http.Client, path string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imdsBase["gcp"]+path, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	return readBody(resp)
}

// lastPathSegment returns the substring after the last "/". GCP metadata
// returns fully-qualified resource paths ("projects/<num>/zones/<zone>",
// "projects/<num>/machineTypes/<type>") whose nameplate value is the leaf.
func lastPathSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// azureCompute is the subset of the Azure IMDS compute document the nameplate
// needs. zone may legitimately be empty (region without availability zones).
type azureCompute struct {
	Location       string `json:"location"`
	Zone           string `json:"zone"`
	VMSize         string `json:"vmSize"`
	SubscriptionID string `json:"subscriptionId"`
}

// detectAzure resolves the cloud nameplate from the Azure Instance Metadata
// Service (Metadata:true header + api-version required). The compute JSON
// document carries location, zone, vmSize and subscriptionId in a single call;
// when it is unavailable the plain text location endpoint is the fallback so
// region resolution never regresses.
func detectAzure(ctx context.Context, client *http.Client) cloudInfo {
	if ci, ok := azureFromComputeDocument(ctx, client); ok {
		return ci
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		imdsBase["azure"]+"/metadata/instance/compute/location?api-version=2021-02-01&format=text", nil)
	if err != nil {
		return cloudInfo{}
	}
	req.Header.Set("Metadata", "true")
	resp, err := client.Do(req)
	if err != nil {
		return cloudInfo{}
	}
	return cloudInfo{provider: "azure", region: readBody(resp)}
}

// azureFromComputeDocument fetches and parses the Azure IMDS compute JSON
// document. ok is false when the document cannot be fetched, parsed, or
// carries no location — the caller then falls back to the text location call.
func azureFromComputeDocument(ctx context.Context, client *http.Client) (cloudInfo, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		imdsBase["azure"]+"/metadata/instance/compute?api-version=2021-02-01&format=json", nil)
	if err != nil {
		return cloudInfo{}, false
	}
	req.Header.Set("Metadata", "true")
	resp, err := client.Do(req)
	if err != nil {
		return cloudInfo{}, false
	}
	body := readBody(resp)
	if body == "" {
		return cloudInfo{}, false
	}
	var doc azureCompute
	if err := json.Unmarshal([]byte(body), &doc); err != nil || doc.Location == "" {
		return cloudInfo{}, false
	}
	return cloudInfo{
		provider:         "azure",
		region:           doc.Location,
		availabilityZone: doc.Zone,
		instanceType:     doc.VMSize,
		accountID:        doc.SubscriptionID,
	}, true
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
