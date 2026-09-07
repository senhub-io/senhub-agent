package data_store

import (
	"fmt"

	"senhub-agent.go/internal/agent/services/configuration"
)

// operatorTagsForProbe is what an operator asked to have stamped on every
// record of one probe instance: its governance attributes, then its
// custom_tags, which win on a collision. Nil when the instance declares
// neither, so the common path allocates nothing. A governance block that
// does not parse contributes nothing here; `config check` reports it.
func operatorTagsForProbe(p configuration.ProbeConfig) (map[string]string, error) {
	gov, err := p.ParseGovernance()
	if err != nil {
		return nil, fmt.Errorf("probe %q: governance: %w", p.Name, err)
	}
	attrs := gov.Attributes()
	if len(attrs) == 0 && len(p.CustomTags) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(attrs)+len(p.CustomTags))
	for k, v := range attrs {
		out[k] = fmt.Sprint(v)
	}
	for k, v := range p.CustomTags {
		out[k] = v
	}
	return out, nil
}
