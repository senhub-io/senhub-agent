package configuration

import (
	"testing"

	"senhub-agent.go/internal/agent/probes/spec"
)

func init() {
	conn := []spec.ParamSpec{
		{Key: "tenant_id", Kind: spec.KindString},
		{Key: "client_id", Kind: spec.KindString},
		{Key: "client_secret", Kind: spec.KindString, Secret: true},
		{Key: "client_secret_file", Kind: spec.KindString},
		{Key: "subscription_id", Kind: spec.KindString},
		{Key: "app", Kind: spec.KindString},
		{Key: "discovery", Kind: spec.KindBlock, Fields: []spec.ParamSpec{
			{Key: "interval", Kind: spec.KindInt},
			{Key: "resource_groups", Kind: spec.KindStringList},
			{Key: "include", Kind: spec.KindStringList},
		}},
	}
	spec.Register(spec.Probe{Type: acaAppsType, DisplayName: "test apps", Params: conn})
	spec.Register(spec.Probe{Type: acaJobsType, DisplayName: "test jobs", Params: conn})
}

var acaDiscoveryEnv = []string{
	"SENHUB_PROBE_ACA_TYPE=azure_container_apps",
	"SENHUB_PROBE_ACA_TENANT_ID=t-1",
	"SENHUB_PROBE_ACA_CLIENT_ID=c-1",
	"SENHUB_PROBE_ACA_CLIENT_SECRET_FILE=/run/secrets/aca",
	"SENHUB_PROBE_ACA_SUBSCRIPTION_ID=s-1",
	"SENHUB_PROBE_ACA_DISCOVERY__RESOURCE_GROUPS=rg-a,rg-b",
	"SENHUB_PROBE_ACA_DISCOVERY__INCLUDE=web-*",
}

func probeNamed(data LocalConfigurationData, name string) (ProbeConfig, bool) {
	for _, p := range data.Probes {
		if p.Name == name {
			return p, true
		}
	}
	return ProbeConfig{}, false
}

func TestEnvACAJobs_AddedWithTheDiscovery(t *testing.T) {
	data, err := applyEnv(t, LocalConfigurationData{}, acaDiscoveryEnv...)
	if err != nil {
		t.Fatal(err)
	}
	jobs, ok := probeNamed(data, "jobs")
	if !ok {
		t.Fatalf("no jobs probe added: %+v", data.Probes)
	}
	if jobs.Type != acaJobsType {
		t.Errorf("type = %q", jobs.Type)
	}
	for k, want := range map[string]string{"tenant_id": "t-1", "client_id": "c-1", "client_secret_file": "/run/secrets/aca", "subscription_id": "s-1"} {
		if jobs.Params[k] != want {
			t.Errorf("%s = %v, want %s", k, jobs.Params[k], want)
		}
	}
	disc, _ := jobs.Params["discovery"].(map[string]interface{})
	if disc == nil {
		t.Fatalf("no discovery block: %+v", jobs.Params)
	}
	if rg, _ := disc["resource_groups"].([]interface{}); len(rg) != 2 {
		if rgs, _ := disc["resource_groups"].([]string); len(rgs) != 2 {
			t.Errorf("resource_groups = %#v, want the discovery's two groups", disc["resource_groups"])
		}
	}
	if _, ok := disc["include"]; ok {
		t.Error("the applications' include filter must not select jobs")
	}
}

func TestEnvACAJobs_TurnedOffByNone(t *testing.T) {
	env := append(append([]string{}, acaDiscoveryEnv...), "SENHUB_PROBE_JOBS_TYPE=none")
	data, err := applyEnv(t, LocalConfigurationData{}, env...)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := probeNamed(data, "jobs"); ok {
		t.Errorf("jobs probe present with SENHUB_PROBE_JOBS_TYPE=none: %+v", data.Probes)
	}
	if _, ok := probeNamed(data, "aca"); !ok {
		t.Error("the applications probe must stay")
	}
}

func TestEnvACAJobs_ExplicitJobsUnchanged(t *testing.T) {
	env := append(append([]string{}, acaDiscoveryEnv...),
		"SENHUB_PROBE_JOBS_TYPE=azure_container_app_jobs", "SENHUB_PROBE_JOBS_APP=nightly",
		"SENHUB_PROBE_JOBS_TENANT_ID=t-2")
	data, err := applyEnv(t, LocalConfigurationData{}, env...)
	if err != nil {
		t.Fatal(err)
	}
	jobs, _ := probeNamed(data, "jobs")
	if jobs.Params["app"] != "nightly" || jobs.Params["tenant_id"] != "t-2" {
		t.Errorf("explicit jobs probe changed: %+v", jobs.Params)
	}
	if _, ok := jobs.Params["discovery"]; ok {
		t.Error("an explicit jobs probe got a discovery block")
	}
}

func TestEnvACAJobs_FileConfigurationUnchanged(t *testing.T) {
	file := LocalConfigurationData{Probes: []ProbeConfig{{Name: "jobs", Type: acaJobsType, Params: map[string]interface{}{"app": "x"}}}}
	data, err := applyEnv(t, file, acaDiscoveryEnv...)
	if err != nil {
		t.Fatal(err)
	}
	jobs, _ := probeNamed(data, "jobs")
	if _, ok := jobs.Params["discovery"]; ok || jobs.Params["app"] != "x" {
		t.Errorf("the configuration's jobs probe was changed: %+v", jobs.Params)
	}

	appsInFile := LocalConfigurationData{Probes: []ProbeConfig{{Name: "aca", Type: acaAppsType, Params: map[string]interface{}{"discovery": map[string]interface{}{}}}}}
	data, err = applyEnv(t, appsInFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := probeNamed(data, "jobs"); ok {
		t.Error("a discovery declared in a file added a jobs probe")
	}
}

func TestEnvACAJobs_NotWithoutDiscovery(t *testing.T) {
	data, err := applyEnv(t, LocalConfigurationData{},
		"SENHUB_PROBE_ACA_TYPE=azure_container_apps", "SENHUB_PROBE_ACA_APP=web", "SENHUB_PROBE_ACA_SUBSCRIPTION_ID=s-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := probeNamed(data, "jobs"); ok {
		t.Error("a single named application added a jobs probe")
	}
}
