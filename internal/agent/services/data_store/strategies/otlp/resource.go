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
//   - service.instance.id = first 8 chars of agent key (set by
//     NewOTLPSyncStrategy when the operator did not supply one)
//   - service.version  = build version, when known via cliArgs.Version
//   - any custom keys from cfg.Resource.Extra are passed through
//     verbatim — callers are responsible for using valid OTel names
func buildResource(cfg ResourceConfig, version string) *resource.Resource {
	attrs := make([]attribute.KeyValue, 0, 4+len(cfg.Extra))

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
	for k, v := range cfg.Extra {
		attrs = append(attrs, attribute.String(k, v))
	}

	// NewSchemaless skips the OTel schema URL field (a dual-purpose
	// pointer that adds churn without behavioral value for our use
	// case). The actual semconv version is implied by the attribute
	// key names we use.
	return resource.NewSchemaless(attrs...)
}
