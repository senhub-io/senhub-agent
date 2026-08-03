package common

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestDetectK8sNodeName(t *testing.T) {
	if got := detectK8sNodeName(); got != "" {
		t.Errorf("NODE_NAME unset → %q, want empty", got)
	}
	t.Setenv("NODE_NAME", "  worker-3  ")
	if got := detectK8sNodeName(); got != "worker-3" {
		t.Errorf("detectK8sNodeName() = %q, want worker-3", got)
	}
}

func TestDetectCloud_EnvOverride(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", "openstack")
	t.Setenv("CLOUD_REGION", "rbx")
	t.Setenv("CLOUD_AVAILABILITY_ZONE", "rbx-a")
	t.Setenv("CLOUD_ACCOUNT_ID", "acct-42")
	t.Setenv("HOST_TYPE", "b2-7")
	want := cloudInfo{provider: "openstack", region: "rbx", availabilityZone: "rbx-a", instanceType: "b2-7", accountID: "acct-42"}
	if got := detectCloud(time.Millisecond); got != want {
		t.Errorf("env override = %+v, want %+v", got, want)
	}
}

func TestDetectCloud_NoMetadataService(t *testing.T) {
	// Point every provider at a closed local port so the probe fails fast and
	// degrades to the zero cloudInfo rather than erroring or hanging.
	restore := imdsBase
	imdsBase = map[string]string{
		"aws":   "http://127.0.0.1:1",
		"gcp":   "http://127.0.0.1:1",
		"azure": "http://127.0.0.1:1",
	}
	t.Cleanup(func() { imdsBase = restore })

	if got := detectCloud(50 * time.Millisecond); got != (cloudInfo{}) {
		t.Errorf("unreachable IMDS = %+v, want zero cloudInfo", got)
	}
}

// awsOnly points the AWS probe at srv and the other providers at a closed
// port, restoring imdsBase on cleanup.
func awsOnly(t *testing.T, srvURL string) {
	t.Helper()
	restore := imdsBase
	imdsBase = map[string]string{"aws": srvURL, "gcp": "http://127.0.0.1:1", "azure": "http://127.0.0.1:1"}
	t.Cleanup(func() { imdsBase = restore })
}

func TestDetectCloud_AWS_IdentityDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/latest/api/token":
			_, _ = w.Write([]byte("tok-abc"))
		case "/latest/dynamic/instance-identity/document":
			if req.Header.Get("X-aws-ec2-metadata-token") != "tok-abc" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{
				"accountId": "123456789012",
				"availabilityZone": "eu-west-1a",
				"instanceType": "t3.medium",
				"region": "eu-west-1",
				"imageId": "ami-0abcdef1234567890",
				"instanceId": "i-0123456789abcdef0"
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	awsOnly(t, srv.URL)

	want := cloudInfo{provider: "aws", region: "eu-west-1", availabilityZone: "eu-west-1a", instanceType: "t3.medium", accountID: "123456789012"}
	if got := detectCloud(time.Second); got != want {
		t.Errorf("AWS identity document = %+v, want %+v", got, want)
	}
}

func TestDetectCloud_AWS_RegionFallback(t *testing.T) {
	// Identity document unavailable (404) → region must still resolve via the
	// plain placement/region endpoint, with the token forwarded.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/latest/api/token":
			_, _ = w.Write([]byte("tok-abc"))
		case "/latest/meta-data/placement/region":
			if req.Header.Get("X-aws-ec2-metadata-token") != "tok-abc" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte("eu-west-1"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	awsOnly(t, srv.URL)

	want := cloudInfo{provider: "aws", region: "eu-west-1"}
	if got := detectCloud(time.Second); got != want {
		t.Errorf("AWS region fallback = %+v, want %+v", got, want)
	}
}

func TestDetectCloud_GCP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Metadata-Flavor") != "Google" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch req.URL.Path {
		case "/computeMetadata/v1/instance/zone":
			_, _ = w.Write([]byte("projects/987654321098/zones/us-central1-a"))
		case "/computeMetadata/v1/instance/machine-type":
			_, _ = w.Write([]byte("projects/987654321098/machineTypes/e2-standard-4"))
		case "/computeMetadata/v1/project/numeric-project-id":
			_, _ = w.Write([]byte("987654321098"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	restore := imdsBase
	imdsBase = map[string]string{"aws": "http://127.0.0.1:1", "gcp": srv.URL, "azure": "http://127.0.0.1:1"}
	t.Cleanup(func() { imdsBase = restore })

	want := cloudInfo{provider: "gcp", region: "us-central1", availabilityZone: "us-central1-a", instanceType: "e2-standard-4", accountID: "987654321098"}
	if got := detectCloud(time.Second); got != want {
		t.Errorf("GCP metadata = %+v, want %+v", got, want)
	}
}

func TestDetectCloud_GCP_ZoneOnly(t *testing.T) {
	// Only the zone endpoint answers → region/AZ resolve, the rest stays empty.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/computeMetadata/v1/instance/zone" {
			_, _ = w.Write([]byte("projects/987654321098/zones/europe-west9-b"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	restore := imdsBase
	imdsBase = map[string]string{"aws": "http://127.0.0.1:1", "gcp": srv.URL, "azure": "http://127.0.0.1:1"}
	t.Cleanup(func() { imdsBase = restore })

	want := cloudInfo{provider: "gcp", region: "europe-west9", availabilityZone: "europe-west9-b"}
	if got := detectCloud(time.Second); got != want {
		t.Errorf("GCP zone-only = %+v, want %+v", got, want)
	}
}

// azureOnly points the Azure probe at srv and the other providers at a closed
// port, restoring imdsBase on cleanup.
func azureOnly(t *testing.T, srvURL string) {
	t.Helper()
	restore := imdsBase
	imdsBase = map[string]string{"aws": "http://127.0.0.1:1", "gcp": "http://127.0.0.1:1", "azure": srvURL}
	t.Cleanup(func() { imdsBase = restore })
}

func TestDetectCloud_Azure_ComputeDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Metadata") != "true" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.URL.Path == "/metadata/instance/compute" && req.URL.Query().Get("format") == "json" {
			_, _ = w.Write([]byte(`{
				"location": "westeurope",
				"zone": "2",
				"vmSize": "Standard_D2s_v3",
				"subscriptionId": "8d10da13-8125-4ba9-a717-bf7490507b3d",
				"name": "recette-vm"
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	azureOnly(t, srv.URL)

	want := cloudInfo{provider: "azure", region: "westeurope", availabilityZone: "2", instanceType: "Standard_D2s_v3", accountID: "8d10da13-8125-4ba9-a717-bf7490507b3d"}
	if got := detectCloud(time.Second); got != want {
		t.Errorf("Azure compute document = %+v, want %+v", got, want)
	}
}

func TestDetectCloud_Azure_LocationFallback(t *testing.T) {
	// Compute JSON document unavailable (404) → region must still resolve via
	// the plain text location endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Metadata") != "true" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.URL.Path == "/metadata/instance/compute/location" {
			_, _ = w.Write([]byte("francecentral"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	azureOnly(t, srv.URL)

	want := cloudInfo{provider: "azure", region: "francecentral"}
	if got := detectCloud(time.Second); got != want {
		t.Errorf("Azure location fallback = %+v, want %+v", got, want)
	}
}

func TestRuntimeFromCgroup(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		content, want string
	}{
		{"12:devices:/docker/abc123", "docker"},
		{"0::/system.slice/containerd.service", "containerd"},
		{"0::/lxc/mycontainer", "lxc"},
		{"0::/machine.slice/libpod-abc.scope", "podman"},
		{"0::/system.slice/sshd.service", ""},
	}
	for i, c := range cases {
		path := dir + "/cgroup" + string(rune('0'+i))
		if err := os.WriteFile(path, []byte(c.content), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := runtimeFromCgroup(path); got != c.want {
			t.Errorf("runtimeFromCgroup(%q) = %q, want %q", c.content, got, c.want)
		}
	}
	if got := runtimeFromCgroup(dir + "/missing"); got != "" {
		t.Errorf("missing cgroup file → %q, want empty", got)
	}
}
