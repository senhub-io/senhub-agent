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
	p, r := detectCloud(time.Millisecond)
	if p != "openstack" || r != "rbx" {
		t.Errorf("env override = (%q,%q), want (openstack,rbx)", p, r)
	}
}

func TestDetectCloud_NoMetadataService(t *testing.T) {
	// Point every provider at a closed local port so the probe fails fast and
	// degrades to ("","") rather than erroring or hanging.
	restore := imdsBase
	imdsBase = map[string]string{
		"aws":   "http://127.0.0.1:1",
		"gcp":   "http://127.0.0.1:1",
		"azure": "http://127.0.0.1:1",
	}
	t.Cleanup(func() { imdsBase = restore })

	p, r := detectCloud(50 * time.Millisecond)
	if p != "" || r != "" {
		t.Errorf("unreachable IMDS = (%q,%q), want empty", p, r)
	}
}

func TestDetectCloud_AWS(t *testing.T) {
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

	restore := imdsBase
	imdsBase = map[string]string{"aws": srv.URL, "gcp": "http://127.0.0.1:1", "azure": "http://127.0.0.1:1"}
	t.Cleanup(func() { imdsBase = restore })

	p, r := detectCloud(time.Second)
	if p != "aws" || r != "eu-west-1" {
		t.Errorf("AWS IMDS = (%q,%q), want (aws,eu-west-1)", p, r)
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
