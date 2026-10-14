package sensor

import (
	"testing"

	"senhub-agent.go/internal/agent/services/exporthook"
	"senhub-agent.go/internal/agent/services/license"
)

// TestPublishLicense_FeatureFollowsTheLicence checks the export-path
// extensions see what the licence allows: an enterprise wildcard allows
// a feature, a Pro list without it does not, no licence allows nothing.
func TestPublishLicense_FeatureFollowsTheLicence(t *testing.T) {
	defer exporthook.SetAuthorizer(nil)
	v, err := license.GetDefaultValidator(7)
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	const feature = "series_metering"

	publishLicense(v, &license.License{Tier: license.TierEnterprise, AuthorizedProbes: []string{"*"}})
	if !exporthook.Authorized(feature) {
		t.Error("an enterprise licence must allow the feature")
	}
	publishLicense(v, &license.License{Tier: license.TierPro, AuthorizedProbes: []string{"veeam"}})
	if exporthook.Authorized(feature) {
		t.Error("a Pro licence that does not list the feature must not allow it")
	}
	publishLicense(v, nil)
	if exporthook.Authorized(feature) {
		t.Error("no licence must allow nothing")
	}
	publishLicense(nil, &license.License{AuthorizedProbes: []string{"*"}})
	if exporthook.Authorized(feature) {
		t.Error("without a validator (safe mode) nothing is allowed")
	}
}
