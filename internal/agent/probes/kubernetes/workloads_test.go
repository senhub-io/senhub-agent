package kubernetes

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func int32p(v int32) *int32 { return &v }
func boolp(v bool) *bool    { return &v }

func testProbe(objects ...runtime.Object) *KubernetesProbe {
	return &KubernetesProbe{
		cfg: probeConfig{
			IncludeNamespaces: []string{"default"},
			ExcludeNamespaces: map[string]bool{},
		},
		clusterEndpoint: "test-cluster",
		clientset:       kubefake.NewSimpleClientset(objects...),
	}
}

// The gap between what a workload asks for and what it has is the number an
// operator watches during a rollout, and it is the one a single count cannot
// answer. Each kind is checked on that pair.
func TestCollectStatefulSets_DesiredVersusReady(t *testing.T) {
	p := testProbe(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"},
		Spec:       appsv1.StatefulSetSpec{Replicas: int32p(3)},
		Status:     appsv1.StatefulSetStatus{ReadyReplicas: 1, CurrentReplicas: 2, UpdatedReplicas: 2},
	})

	pts, err := p.collectStatefulSets(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("collectStatefulSets: %v", err)
	}

	for name, want := range map[string]float64{
		"k8s.statefulset.desired": 3,
		"k8s.statefulset.ready":   1,
		"k8s.statefulset.current": 2,
		"k8s.statefulset.updated": 2,
	} {
		dp, ok := findDP(pts, name)
		if !ok {
			t.Errorf("%s not emitted", name)
			continue
		}
		if dp.Value != want {
			t.Errorf("%s = %v, want %v", name, dp.Value, want)
		}
		if got := tagValue(dp, "k8s.workload.kind"); got != "statefulset" {
			t.Errorf("%s: k8s.workload.kind = %q, want statefulset", name, got)
		}
		if got := tagValue(dp, "k8s.workload.name"); got != "pg" {
			t.Errorf("%s: k8s.workload.name = %q, want pg", name, got)
		}
	}
}

func TestCollectDaemonSets_MisscheduledIsReported(t *testing.T) {
	p := testProbe(&appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "logs", Namespace: "default"},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: 5, CurrentNumberScheduled: 4,
			NumberReady: 3, NumberMisscheduled: 1,
		},
	})

	pts, err := p.collectDaemonSets(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("collectDaemonSets: %v", err)
	}
	for name, want := range map[string]float64{
		"k8s.daemonset.desired_scheduled": 5,
		"k8s.daemonset.ready":             3,
		"k8s.daemonset.misscheduled":      1,
	} {
		dp, ok := findDP(pts, name)
		if !ok {
			t.Errorf("%s not emitted", name)
			continue
		}
		if dp.Value != want {
			t.Errorf("%s = %v, want %v", name, dp.Value, want)
		}
	}
}

// A Job whose pods keep failing stays present and looks scheduled; nothing in
// the pod metrics says the Job as a whole is not converging.
func TestCollectJobs_FailedCountIsEmitted(t *testing.T) {
	p := testProbe(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "migrate", Namespace: "default"},
		Spec:       batchv1.JobSpec{Completions: int32p(1)},
		Status:     batchv1.JobStatus{Active: 0, Succeeded: 0, Failed: 4},
	})

	pts, err := p.collectJobs(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("collectJobs: %v", err)
	}
	dp, ok := findDP(pts, "k8s.job.failed")
	if !ok {
		t.Fatal("k8s.job.failed not emitted")
	}
	if dp.Value != 4 {
		t.Errorf("k8s.job.failed = %v, want 4", dp.Value)
	}
}

// A suspended CronJob is the quiet failure: nothing runs, nothing errors, and
// the absence of Jobs looks identical to a schedule that has not fired yet.
func TestCollectCronJobs_SuspendedIsVisible(t *testing.T) {
	p := testProbe(&batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: "default"},
		Spec:       batchv1.CronJobSpec{Suspend: boolp(true)},
	})

	pts, err := p.collectCronJobs(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("collectCronJobs: %v", err)
	}
	dp, ok := findDP(pts, "k8s.cronjob.suspended")
	if !ok {
		t.Fatal("k8s.cronjob.suspended not emitted")
	}
	if dp.Value != 1 {
		t.Errorf("k8s.cronjob.suspended = %v, want 1", dp.Value)
	}
}

// A node under disk pressure is still Ready right up to the moment it is not.
func TestNodeConditionPoints_PressureIsOneWhenPresent(t *testing.T) {
	n := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			{Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue},
			{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse},
		}},
	}

	pts := nodeConditionPoints(n, time.Now(), nil)

	dp, ok := findDP(pts, "k8s.node.condition.disk_pressure")
	if !ok {
		t.Fatal("disk_pressure not emitted")
	}
	if dp.Value != 1 {
		t.Errorf("disk_pressure = %v, want 1 (1 means the pressure IS present)", dp.Value)
	}
	if dp, ok := findDP(pts, "k8s.node.condition.memory_pressure"); !ok || dp.Value != 0 {
		t.Errorf("memory_pressure should be 0 when the condition is False")
	}
	// PIDPressure is absent from the object; a steady 0 is emitted so a
	// dashboard cannot confuse "not reported" with "no pressure".
	if dp, ok := findDP(pts, "k8s.node.condition.pid_pressure"); !ok || dp.Value != 0 {
		t.Errorf("pid_pressure should default to 0 when unreported")
	}
	// NetworkUnavailable is the exception: many CNIs never set it, so a
	// steady 0 would be inventing a fact.
	if _, ok := findDP(pts, "k8s.node.condition.network_unavailable"); ok {
		t.Error("network_unavailable must not be invented when the CNI never reports it")
	}
}

// Without requests and limits there is no way to say whether a cluster is
// over-committed — the question asked immediately after an outage.
func TestPodResourcePoints_RequestsSummedLimitsOptional(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{Containers: []corev1.Container{
			{
				Name: "a",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("250m"),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
				},
			},
			{
				Name: "b",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("250m"),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
				},
			},
		}},
	}

	pts := podResourcePoints(pod, time.Now(), nil)

	dp, ok := findDP(pts, "k8s.pod.cpu.request")
	if !ok {
		t.Fatal("k8s.pod.cpu.request not emitted")
	}
	if dp.Value < 0.49 || dp.Value > 0.51 {
		t.Errorf("cpu.request = %v, want the sum of both containers (0.5)", dp.Value)
	}
	if dp, ok := findDP(pts, "k8s.pod.memory.request"); !ok || dp.Value != 128*1024*1024 {
		t.Errorf("memory.request should sum both containers (128Mi), got %v", dp.Value)
	}
	// No limit is set: a pod with no limit is unbounded, which is a different
	// fact from a limit of zero. Emitting 0 would read as "capped at nothing".
	if _, ok := findDP(pts, "k8s.pod.cpu.limit"); ok {
		t.Error("cpu.limit must not be emitted as 0 when no limit is set")
	}
}
