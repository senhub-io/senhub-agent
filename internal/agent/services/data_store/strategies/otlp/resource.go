package otlp

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

// OTel resource attribute keys. Hard-coded as strings rather than
// importing a specific semconv version because:
//  1. semconv versions churn quickly across OTel releases — pinning one
//     creates a maintenance liability for keys that haven't actually
//     changed semantics in years
//  2. these keys are part of the OTel spec, not the Go SDK
//
// If a future spec rev renames any of these, we'll update the
// constants explicitly.
const (
	resourceKeyServiceName     = "service.name"
	resourceKeyServiceInstance = "service.instance.id"
	resourceKeyServiceVersion  = "service.version"
	resourceKeyEnvironment     = "deployment.environment"
)

// buildResource constructs the OTel Resource attached to every emitted
// record. The resource carries identity for the agent process; it is
// scope-stable for the lifetime of the strategy instance.
//
// Defaults:
//   - service.name     = "senhub-agent" unless operator overrode it
//   - service.instance.id = the full agent key (set by
//     NewOTLPSyncStrategy when the operator did not supply one)
//   - service.version  = build version, when known via cliArgs.Version
//   - hostAttrs (host.* / os.* from gopsutil) so the agent's own metrics and
//     logs carry the SAME host.id as the host entity — the join that lets a
//     backend correlate the host node in the infra graph with its telemetry.
//     Lowest precedence: an operator global_tag or resource: Extra of the same
//     key wins.
//   - agent-level global_tags are attached as resource attributes:
//     they describe the agent/host as a whole, so they belong on the
//     Resource (one per process) rather than multiplying as per-metric
//     attributes (issue #202)
//   - any custom keys from cfg.Resource.Extra are passed through
//     verbatim — callers are responsible for using valid OTel names.
//     Explicit resource: config wins over a same-named global_tag.
func buildResource(cfg ResourceConfig, version string, hostAttrs, globalTags map[string]string) *resource.Resource {
	attrs := make([]attribute.KeyValue, 0, 4+len(hostAttrs)+len(globalTags)+len(cfg.Extra))

	if cfg.ServiceName != "" {
		attrs = append(attrs, attribute.String(resourceKeyServiceName, cfg.ServiceName))
	}
	if cfg.ServiceInstance != "" {
		attrs = append(attrs, attribute.String(resourceKeyServiceInstance, cfg.ServiceInstance))
	}
	if version != "" {
		attrs = append(attrs, attribute.String(resourceKeyServiceVersion, version))
	}
	if cfg.Environment != "" {
		attrs = append(attrs, attribute.String(resourceKeyEnvironment, cfg.Environment))
	}
	// host.*/os.* first (lowest precedence), then global_tags, then Extra so an
	// explicit operator value of the same key wins (NewSchemaless keeps last).
	for k, v := range hostAttrs {
		attrs = append(attrs, attribute.String(k, v))
	}
	for k, v := range globalTags {
		attrs = append(attrs, attribute.String(k, v))
	}
	for k, v := range cfg.Extra {
		attrs = append(attrs, attribute.String(k, v))
	}

	// NewSchemaless skips the OTel schema URL field (a dual-purpose
	// pointer that adds churn without behavioral value for our use
	// case). The actual semconv version is implied by the attribute
	// key names we use.
	return resource.NewSchemaless(attrs...)
}

// volatileOSKeys are resource attributes that change on every OS or kernel
// upgrade. A backend such as VictoriaMetrics turns resource attributes into
// labels of every series, so these values would recreate every series of the
// machine on each upgrade and break every rate() across it.
var volatileOSKeys = []string{"os.version", "os.build_id", "os.description"}

// withoutVolatileOSAttrs returns a copy of hostAttrs without the volatile OS
// keys, so they never end up as labels on every series. It never mutates the
// input and returns nil for a nil input.
func withoutVolatileOSAttrs(hostAttrs map[string]string) map[string]string {
	if hostAttrs == nil {
		return nil
	}
	out := make(map[string]string, len(hostAttrs))
	for k, v := range hostAttrs {
		out[k] = v
	}
	for _, k := range volatileOSKeys {
		delete(out, k)
	}
	return out
}

// volatileOSAttrs returns only the volatile OS keys that are present and
// non-empty, so they can ride on senhub.agent.build_info instead of every
// series. It returns nil when none are available.
func volatileOSAttrs(hostAttrs map[string]string) map[string]string {
	var out map[string]string
	for _, k := range volatileOSKeys {
		if v, ok := hostAttrs[k]; ok && v != "" {
			if out == nil {
				out = map[string]string{}
			}
			out[k] = v
		}
	}
	return out
}
