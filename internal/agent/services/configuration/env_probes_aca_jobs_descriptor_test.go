package configuration

import (
	"testing"
)

var acaDescriptorYAML = `- name: container-apps
  type: azure_container_apps
  params:
    tenant_id: t-1
    client_secret: '${env:AZ_SECRET}'
    discovery:
      resource_groups: [rg-a]
      interval: 120
`

func acaDescriptorSource() ProbeConfig {
	return ProbeConfig{
		Name: "container-apps",
		Type: acaAppsType,
		Params: map[string]interface{}{
			"tenant_id":     "t-1",
			"client_secret": "${env:AZ_SECRET}",
			"discovery": map[string]interface{}{
				"resource_groups": []interface{}{"rg-a"},
				"interval":        120,
			},
		},
	}
}

func TestEnvACAJobsDescriptor_AddedFromSENHUB_PROBES(t *testing.T) {
	data, err := applyEnv(t, LocalConfigurationData{Probes: []ProbeConfig{acaDescriptorSource()}}, "SENHUB_PROBES="+acaDescriptorYAML)
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
	if jobs.Enabled == nil || !*jobs.Enabled {
		t.Errorf("enabled = %v, want true", jobs.Enabled)
	}
	if jobs.Params["tenant_id"] != "t-1" {
		t.Errorf("tenant_id = %v, want t-1", jobs.Params["tenant_id"])
	}
	if jobs.Params["client_secret"] != "${env:AZ_SECRET}" {
		t.Errorf("client_secret = %v, want the raw ${env:AZ_SECRET} reference", jobs.Params["client_secret"])
	}
	disc, _ := jobs.Params["discovery"].(map[string]interface{})
	if disc == nil {
		t.Fatalf("no discovery block: %+v", jobs.Params)
	}
	if disc["interval"] != 120 {
		t.Errorf("interval = %v, want 120", disc["interval"])
	}
	rg, _ := disc["resource_groups"].([]interface{})
	if len(rg) != 1 || rg[0] != "rg-a" {
		t.Errorf("resource_groups = %#v, want [rg-a]", disc["resource_groups"])
	}
}

func TestEnvACAJobsDescriptor_TurnedOffByNone(t *testing.T) {
	env := []string{"SENHUB_PROBES=" + acaDescriptorYAML, "SENHUB_PROBE_JOBS_TYPE=none"}
	data, err := applyEnv(t, LocalConfigurationData{Probes: []ProbeConfig{acaDescriptorSource()}}, env...)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := probeNamed(data, "jobs"); ok {
		t.Errorf("jobs probe present with SENHUB_PROBE_JOBS_TYPE=none: %+v", data.Probes)
	}
	if _, ok := probeNamed(data, "container-apps"); !ok {
		t.Error("the descriptor probe must stay")
	}
}

func TestEnvACAJobsDescriptor_NotWithoutDiscovery(t *testing.T) {
	yamlNoDiscovery := `- name: container-apps
  type: azure_container_apps
  params:
    tenant_id: t-1
`
	src := ProbeConfig{Name: "container-apps", Type: acaAppsType, Params: map[string]interface{}{"		tenant_id": "t-1"}}
	data, err := applyEnv(t, LocalConfigurationData{Probes: []ProbeConfig{src}}, "SENHUB_PROBES="+yamlNoDiscovery)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := probeNamed(data, "jobs"); ok {
		t.Error("a descriptor probe without discovery added a jobs probe")
	}
}

func TestEnvACAJobsDescriptor_OtherFileNeverCounts(t *testing.T) {
	yamlOther := `- name: other
  type: http_check
  params:
    targets: [https://example.test/health]
`
	fileApps := ProbeConfig{Name: "file-apps", Type: acaAppsType, Params: map[string]interface{}{
		"discovery": map[string]interface{}{"resource_groups": []interface{}{"rg-a"}},
	}}
	data, err := applyEnv(t, LocalConfigurationData{Probes: []ProbeConfig{fileApps}}, "SENHUB_PROBES="+yamlOther)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := probeNamed(data, "jobs"); ok {
		t.Error("a probe of another file added a jobs probe")
	}
}

func TestEnvACAJobsDescriptor_EnvAndDescriptorTogetherAddNothing(t *testing.T) {
	env := append([]string{"SENHUB_PROBES=" + acaDescriptorYAML}, acaDiscoveryEnv...)
	data, err := applyEnv(t, LocalConfigurationData{Probes: []ProbeConfig{acaDescriptorSource()}}, env...)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := probeNamed(data, "jobs"); ok {
		t.Error("two discoveries (one env-var, one SENHUB_PROBES) added a jobs probe")
	}
}

func TestEnvACAJobsDescriptor_ConfiguredJobsUnchanged(t *testing.T) {
	file := LocalConfigurationData{Probes: []ProbeConfig{
		acaDescriptorSource(),
		{Name: "jobs", Type: acaJobsType, Params: map[string]interface{}{"app": "x"}},
	}}
	data, err := applyEnv(t, file, "SENHUB_PROBES="+acaDescriptorYAML)
	if err != nil {
		t.Fatal(err)
	}
	jobs, _ := probeNamed(data, "jobs")
	if _, ok := jobs.Params["discovery"]; ok || jobs.Params["app"] != "x" {
		t.Errorf("the configured jobs probe was changed: %+v", jobs.Params)
	}
}

func TestEnvACAJobsDescriptor_OperatorJobsProbeUnderAnotherName(t *testing.T) {
	enabled := true
	data := &LocalConfigurationData{Probes: []ProbeConfig{
		{Name: "container-apps", Type: acaAppsType, Enabled: &enabled, Params: ProbeConfigParams{"discovery": map[string]interface{}{"interval": 300}}},
		{Name: "my-jobs", Type: acaJobsType, Enabled: &enabled, Params: ProbeConfigParams{}},
	}}
	environ := []string{"SENHUB_PROBES=- name: container-apps\n  type: azure_container_apps\n- name: my-jobs\n  type: azure_container_app_jobs\n"}
	withImplicitACAJobs(nil, data, environ, nil)
	if len(data.Probes) != 2 {
		t.Fatalf("probes = %d, want 2: the operator's own jobs probe must suppress the implicit one", len(data.Probes))
	}
}
