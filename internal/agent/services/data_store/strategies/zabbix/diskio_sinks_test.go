package zabbix

import (
	"bytes"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/strategies/http/prometheus"
	"senhub-agent.go/internal/agent/services/data_store/strategies/zabbix/template"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// Linux block-device I/O has to reach every sink under the OTel names,
// with the direction as an attribute and the device as the instance.
func TestLinuxDiskIOReachesEverySink(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	def := defs["logicaldisk"]
	cache := func(name string, v float64) otelmapper.CacheMetric {
		return otelmapper.CacheMetric{ProbeName: "logicaldisk", ProbeType: "logicaldisk", MetricName: name, Value: v,
			Tags: map[string]string{"device": "sda"}}
	}
	metrics := []otelmapper.CacheMetric{
		cache("diskio_read_bytes", 4096), cache("diskio_write_bytes", 8192),
		cache("diskio_read_ops", 4), cache("diskio_write_ops", 8), cache("diskio_busy_seconds", 2.5),
	}

	var records []otelmapper.OtelRecord
	want := map[string]string{
		"diskio_read_bytes": "system.disk.io", "diskio_write_bytes": "system.disk.io",
		"diskio_read_ops": "system.disk.operations", "diskio_write_ops": "system.disk.operations",
		"diskio_busy_seconds": "system.disk.io_time",
	}
	for _, m := range metrics {
		recs, err := otelmapper.Resolve(&def, m, otelmapper.DefaultResolveOptions())
		if err != nil || len(recs) != 1 {
			t.Fatalf("%s: %v %+v", m.MetricName, err, recs)
		}
		r := recs[0]
		if r.Name != want[m.MetricName] || r.Type != "counter" {
			t.Errorf("%s resolves to %s (%s)", m.MetricName, r.Name, r.Type)
		}
		if r.Attributes["system.device"] != "sda" {
			t.Errorf("%s: system.device = %q", m.MetricName, r.Attributes["system.device"])
		}
		if strings.HasPrefix(m.MetricName, "diskio_read") && r.Attributes["disk.io.direction"] != "read" {
			t.Errorf("%s: direction = %q", m.MetricName, r.Attributes["disk.io.direction"])
		}
		records = append(records, r)
	}

	var prom bytes.Buffer
	if err := prometheus.SerializeToTextExposition(records, &prom, prometheus.SerializeOptions{}); err != nil {
		t.Fatal(err)
	}
	out := prom.String()
	for _, frag := range []string{
		"# TYPE senhub_system_disk_io_bytes_total counter",
		`disk_io_direction="read"`,
		"# TYPE senhub_system_disk_operations_total counter",
		"# TYPE senhub_system_disk_io_time_seconds_total counter",
	} {
		if !strings.Contains(out, frag) {
			t.Errorf("prometheus output lacks %q:\n%s", frag, out)
		}
	}

	sent := map[string]bool{}
	for _, m := range metrics {
		for _, it := range itemsFor("senhub", &def, m) {
			sent[it.Key] = true
		}
	}
	for _, key := range []string{
		"senhub.system.disk.io[logicaldisk,sda,read]",
		"senhub.system.disk.io[logicaldisk,sda,write]",
		"senhub.system.disk.io_time[logicaldisk,sda]",
	} {
		if !sent[key] {
			t.Errorf("zabbix does not send %s; sent %v", key, sent)
		}
	}

	exp, err := template.Generate(def, template.Options{Platform: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	rules := map[string]bool{}
	for _, r := range exp.ZabbixExport.Templates[0].DiscoveryRules {
		rules[r.Key] = true
	}
	for _, key := range []string{
		"senhub.discovery[logicaldisk,device]",
		"senhub.discovery.variants[logicaldisk,system.disk.io,device]",
		"senhub.discovery.variants[logicaldisk,system.disk.operations,device]",
	} {
		if !rules[key] {
			t.Errorf("linux template lacks the discovery rule %s; has %v", key, rules)
		}
	}
}
