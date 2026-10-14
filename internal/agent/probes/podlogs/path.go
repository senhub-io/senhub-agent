package podlogs

import (
	"path"
	"strconv"
	"strings"
)

// PodRef identifies a pod log file parsed from a node log path.
type PodRef struct {
	Namespace    string
	Pod          string
	UID          string
	Container    string
	RestartCount int
	Rotated      bool
}

// ParsePodLogPath parses the last three components of a pod log path:
// <namespace>_<pod>_<uid>/<container>/<restart>.log[.timestamp].
// Compressed files are rejected because this package never reads them.
func ParsePodLogPath(p string) (PodRef, bool) {
	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '/' })
	if len(parts) < 3 {
		return PodRef{}, false
	}

	dir := parts[len(parts)-3]
	container := parts[len(parts)-2]
	fileName := parts[len(parts)-1]

	base, rotated, ok := parseLogFileName(fileName)
	if !ok {
		return PodRef{}, false
	}

	restart, err := strconv.Atoi(base)
	if err != nil || restart < 0 {
		return PodRef{}, false
	}

	namespace, pod, uid, ok := parsePodDir(dir)
	if !ok || container == "" {
		return PodRef{}, false
	}

	return PodRef{
		Namespace:    namespace,
		Pod:          pod,
		UID:          uid,
		Container:    container,
		RestartCount: restart,
		Rotated:      rotated,
	}, true
}

func parseLogFileName(name string) (base string, rotated bool, ok bool) {
	if strings.HasSuffix(name, ".gz") {
		return "", false, false
	}
	if strings.HasSuffix(name, ".log") {
		return strings.TrimSuffix(name, ".log"), false, true
	}
	if idx := strings.Index(name, ".log."); idx >= 0 {
		if idx+len(".log.") == len(name) {
			return "", false, false
		}
		return name[:idx], true, true
	}
	return "", false, false
}

func parsePodDir(dir string) (namespace, pod, uid string, ok bool) {
	if strings.Count(dir, "_") != 2 {
		return "", "", "", false
	}

	last := strings.LastIndex(dir, "_")
	if last < 0 {
		return "", "", "", false
	}
	uid = dir[last+1:]
	rest := dir[:last]

	second := strings.LastIndex(rest, "_")
	if second < 0 {
		return "", "", "", false
	}
	namespace = rest[:second]
	pod = rest[second+1:]

	if namespace == "" || pod == "" || uid == "" {
		return "", "", "", false
	}
	return namespace, pod, uid, true
}

// Attributes returns the OTel Kubernetes semantic convention attributes for r.
func (r PodRef) Attributes() map[string]string {
	return map[string]string{
		"k8s.namespace.name":          r.Namespace,
		"k8s.pod.name":                r.Pod,
		"k8s.pod.uid":                 r.UID,
		"k8s.container.name":          r.Container,
		"k8s.container.restart_count": strconv.Itoa(r.RestartCount),
	}
}

// Filter selects PodRefs by namespace and pod glob patterns using path.Match
// semantics. Exclude patterns win over include patterns; empty include means all.
type Filter struct {
	IncludeNamespaces []string
	ExcludeNamespaces []string
	IncludePods       []string
	ExcludePods       []string
}

// Allows reports whether r passes the filter.
func (f Filter) Allows(r PodRef) bool {
	if matchAny(f.ExcludeNamespaces, r.Namespace) {
		return false
	}
	if len(f.IncludeNamespaces) > 0 && !matchAny(f.IncludeNamespaces, r.Namespace) {
		return false
	}
	if matchAny(f.ExcludePods, r.Pod) {
		return false
	}
	if len(f.IncludePods) > 0 && !matchAny(f.IncludePods, r.Pod) {
		return false
	}
	return true
}

func matchAny(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if ok, err := path.Match(pattern, value); err == nil && ok {
			return true
		}
	}
	return false
}
