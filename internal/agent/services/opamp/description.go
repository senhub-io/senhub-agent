package opamp

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/open-telemetry/opamp-go/client/types"
	"github.com/open-telemetry/opamp-go/protobufs"

	"senhub-agent.go/internal/agent/services/license"
)

// ServiceName and ConfigSourceLocal are values of the agent description.
const (
	ServiceName       = "senhub-agent"
	ConfigSourceLocal = "local"
)

type descriptionInput struct {
	InstanceID  string
	Version     string
	Host        map[string]string
	Edition     string
	LicenseTier string
}

func buildDescription(in descriptionInput) *protobufs.AgentDescription {
	desc := &protobufs.AgentDescription{
		IdentifyingAttributes: []*protobufs.KeyValue{
			stringValue("service.name", ServiceName),
			stringValue("service.instance.id", in.InstanceID),
			stringValue("service.version", in.Version),
		},
	}

	attrs := make([]*protobufs.KeyValue, 0, 8)
	if in.Host != nil {
		for _, key := range []string{"host.id", "host.name", "os.type", "os.description", "host.arch"} {
			if v := in.Host[key]; v != "" {
				attrs = append(attrs, stringValue(key, v))
			}
		}
	}
	if in.Edition != "" {
		attrs = append(attrs, stringValue("senhub.agent.edition", in.Edition))
	}
	if in.LicenseTier != "" {
		attrs = append(attrs, stringValue("senhub.license.tier", in.LicenseTier))
	}
	attrs = append(attrs, stringValue("senhub.agent.config.source", ConfigSourceLocal))
	desc.NonIdentifyingAttributes = attrs

	return desc
}

func stringValue(key, value string) *protobufs.KeyValue {
	return &protobufs.KeyValue{
		Key: key,
		Value: &protobufs.AnyValue{
			Value: &protobufs.AnyValue_StringValue{StringValue: value},
		},
	}
}

func instanceUID(instanceID string) (types.InstanceUid, error) {
	if instanceID == "" {
		return types.InstanceUid{}, fmt.Errorf("instance id is empty")
	}
	uid, err := uuid.Parse(instanceID)
	if err != nil {
		return types.InstanceUid{}, fmt.Errorf("instance id %q: %w", instanceID, err)
	}
	return types.InstanceUid(uid[:]), nil
}

// Edition names the build: "enterprise" when it carries a probe of the
// paid catalogue, "oss" otherwise.
func Edition(isRegistered func(probeType string) bool) string {
	if isRegistered == nil {
		return ""
	}
	for _, probeType := range license.KnownPaidProbes() {
		if isRegistered(probeType) {
			return "enterprise"
		}
	}
	return "oss"
}

func licenseTier(token, agentKey string, newValidator func() (license.Validator, error)) string {
	if token == "" {
		return "free"
	}
	if newValidator == nil {
		return ""
	}

	validator, err := newValidator()
	if err != nil {
		return ""
	}

	lic, err := validator.ValidateLicense(token)
	if err != nil || lic == nil {
		return "free"
	}
	if agentKey != "" && !license.VerifyBinding(token, agentKey, lic) {
		return "free"
	}
	if lic.IsExpired && !validator.IsInGracePeriod(lic) {
		return "free"
	}
	return string(lic.Tier)
}
