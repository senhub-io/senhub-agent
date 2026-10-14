package podlogs

import (
	"reflect"
	"testing"
)

func TestParsePodLogPath(t *testing.T) {
	uid := "12345678-1234-1234-1234-123456789abc"

	tests := []struct {
		name string
		path string
		want PodRef
		ok   bool
	}{
		{
			name: "normal",
			path: "/var/log/pods/my-ns_my-pod_" + uid + "/my-container/0.log",
			want: PodRef{
				Namespace:    "my-ns",
				Pod:          "my-pod",
				UID:          uid,
				Container:    "my-container",
				RestartCount: 0,
				Rotated:      false,
			},
			ok: true,
		},
		{
			name: "relative",
			path: "my-ns_my-pod_" + uid + "/cont/1.log",
			want: PodRef{
				Namespace:    "my-ns",
				Pod:          "my-pod",
				UID:          uid,
				Container:    "cont",
				RestartCount: 1,
				Rotated:      false,
			},
			ok: true,
		},
		{
			name: "rotated",
			path: "/var/log/pods/ns_pod_" + uid + "/cont/3.log.20240101-000000",
			want: PodRef{
				Namespace:    "ns",
				Pod:          "pod",
				UID:          uid,
				Container:    "cont",
				RestartCount: 3,
				Rotated:      true,
			},
			ok: true,
		},
		{
			name: "gz rejected",
			path: "/var/log/pods/ns_pod_" + uid + "/cont/0.log.gz",
			ok:   false,
		},
		{
			name: "rotated gz rejected",
			path: "/var/log/pods/ns_pod_" + uid + "/cont/0.log.20240101.gz",
			ok:   false,
		},
		{
			name: "too few components",
			path: "ns_pod_" + uid + "/0.log",
			ok:   false,
		},
		{
			name: "namespace with underscore rejected",
			path: "/var/log/pods/bad_ns_pod_" + uid + "/cont/0.log",
			ok:   false,
		},
		{
			name: "pod with underscore rejected",
			path: "/var/log/pods/ns_bad_pod_" + uid + "/cont/0.log",
			ok:   false,
		},
		{
			name: "restart count non-numeric",
			path: "/var/log/pods/ns_pod_" + uid + "/cont/abc.log",
			ok:   false,
		},
		{
			name: "negative restart count",
			path: "/var/log/pods/ns_pod_" + uid + "/cont/-1.log",
			ok:   false,
		},
		{
			name: "missing log suffix",
			path: "/var/log/pods/ns_pod_" + uid + "/cont/0",
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParsePodLogPath(tt.path)
			if ok != tt.ok {
				t.Fatalf("ParsePodLogPath(%q) ok = %v, want %v", tt.path, ok, tt.ok)
			}
			if !ok {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPodRefAttributes(t *testing.T) {
	r := PodRef{
		Namespace:    "ns",
		Pod:          "pod",
		UID:          "uid",
		Container:    "cont",
		RestartCount: 2,
	}

	got := r.Attributes()
	want := map[string]string{
		"k8s.namespace.name":          "ns",
		"k8s.pod.name":                "pod",
		"k8s.pod.uid":                 "uid",
		"k8s.container.name":          "cont",
		"k8s.container.restart_count": "2",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Attributes() = %#v, want %#v", got, want)
	}
}

func TestFilterAllows(t *testing.T) {
	f := Filter{
		IncludeNamespaces: []string{"team-*"},
		ExcludeNamespaces: []string{"team-secret"},
		IncludePods:       []string{"app-*"},
		ExcludePods:       []string{"app-debug"},
	}

	base := PodRef{Namespace: "team-a", Pod: "app-web"}

	tests := []struct {
		name   string
		mutate func(*PodRef)
		want   bool
	}{
		{name: "all allowed", mutate: func(*PodRef) {}, want: true},
		{name: "namespace exclude wins", mutate: func(r *PodRef) { r.Namespace = "team-secret" }, want: false},
		{name: "namespace not included", mutate: func(r *PodRef) { r.Namespace = "other" }, want: false},
		{name: "pod exclude wins", mutate: func(r *PodRef) { r.Pod = "app-debug" }, want: false},
		{name: "pod not included", mutate: func(r *PodRef) { r.Pod = "other" }, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := base
			tt.mutate(&r)
			if got := f.Allows(r); got != tt.want {
				t.Fatalf("Allows(%+v) = %v, want %v", r, got, tt.want)
			}
		})
	}
}

func TestFilterEmptyIncludesAllowAll(t *testing.T) {
	var f Filter
	if !f.Allows(PodRef{Namespace: "ns", Pod: "pod"}) {
		t.Fatal("empty filter should allow all pod refs")
	}
}
