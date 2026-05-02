package prometheus

import (
	"math"
	"testing"
)

func floatApprox(a, b float64) bool {
	if a == b {
		return true
	}
	return math.Abs(a-b) < 1e-9
}

func TestOTelNameToPromName(t *testing.T) {
	tests := []struct {
		name       string
		otelName   string
		unit       string
		metricType string
		want       string
	}{
		// OTel native system.* metrics
		{"cpu.time counter", "system.cpu.time", "s", "counter", "senhub_system_cpu_time_seconds_total"},
		{"cpu.utilization gauge", "system.cpu.utilization", "1", "gauge", "senhub_system_cpu_utilization_ratio"},
		{"memory.usage updowncounter", "system.memory.usage", "By", "updowncounter", "senhub_system_memory_usage_bytes"},
		{"memory.limit updowncounter", "system.memory.limit", "By", "updowncounter", "senhub_system_memory_limit_bytes"},
		{"network.io counter", "system.network.io", "By", "counter", "senhub_system_network_io_bytes_total"},
		{"network.packet.count counter", "system.network.packet.count", "{packet}", "counter", "senhub_system_network_packet_count_total"},
		{"filesystem.utilization gauge", "system.filesystem.utilization", "1", "gauge", "senhub_system_filesystem_utilization_ratio"},

		// OTEP 0119 load averages
		{"load_1m", "system.linux.cpu.load_1m", "{thread}", "gauge", "senhub_system_linux_cpu_load_1m"},

		// Hardware
		{"hw.status updowncounter", "hw.status", "1", "updowncounter", "senhub_hw_status"},
		{"hw.physical_disk.size", "hw.physical_disk.size", "By", "updowncounter", "senhub_hw_physical_disk_size_bytes"},

		// senhub.* extensions
		{"senhub veeam job duration", "senhub.veeam.job.seconds_since_last_run", "s", "gauge", "senhub_veeam_job_seconds_since_last_run_seconds"},
		{"senhub probe http", "senhub.probe.http.duration_seconds", "s", "gauge", "senhub_probe_http_duration_seconds"},
		{"senhub probe icmp", "senhub.probe.icmp.packet_loss_ratio", "1", "gauge", "senhub_probe_icmp_packet_loss_ratio"},

		// Rate units
		{"bit/s gauge", "senhub.netscaler.interface.throughput", "bit/s", "gauge", "senhub_netscaler_interface_throughput_bits_per_second"},
		{"By/s gauge", "senhub.netscaler.lbvserver.throughput", "By/s", "gauge", "senhub_netscaler_lbvserver_throughput_bytes_per_second"},
		{"1/s gauge", "senhub.netscaler.ssl.transactions.rate", "1/s", "gauge", "senhub_netscaler_ssl_transactions_rate_per_second"},

		// Annotated counters
		{"error counter", "system.network.errors", "{error}", "counter", "senhub_system_network_errors_total"},

		// No duplicate suffix
		{"already seconds", "senhub.probe.http.duration_seconds", "s", "gauge", "senhub_probe_http_duration_seconds"},
		{"already total counter", "senhub.veeam.jobs.runs_total", "{run}", "counter", "senhub_veeam_jobs_runs_total"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OTelNameToPromName(tt.otelName, tt.unit, tt.metricType)
			if got != tt.want {
				t.Errorf("OTelNameToPromName(%q, %q, %q) = %q, want %q",
					tt.otelName, tt.unit, tt.metricType, got, tt.want)
			}
		})
	}
}

func TestOTelAttributeToPromLabel(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"cpu.mode", "cpu_mode"},
		{"system.memory.state", "system_memory_state"},
		{"network.io.direction", "network_io_direction"},
		{"hw.id", "hw_id"},
		{"hw.state", "hw_state"},
		{"senhub.network.wifi.ssid", "senhub_network_wifi_ssid"},
		{"probe_name", "probe_name"},
		{"url.full", "url_full"},
	}
	for _, tt := range tests {
		if got := OTelAttributeToPromLabel(tt.in); got != tt.want {
			t.Errorf("OTelAttributeToPromLabel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestConvertValue(t *testing.T) {
	tests := []struct {
		name          string
		raw           float64
		sourceUnit    string
		otelUnit      string
		valueScale    float64
		want          float64
	}{
		{"percent to ratio", 50.0, "%", "1", 0, 0.5},
		{"percent case", 22.4, "percent", "1", 0, 0.224},
		{"MB to By", 512.0, "MB", "By", 0, 512.0 * 1048576.0},
		{"KB to By", 256.0, "KB", "By", 0, 256.0 * 1024.0},
		{"ms to s", 1500.0, "ms", "s", 0, 1.5},
		{"us to s", 1.5e6, "μs", "s", 0, 1.5},
		{"Mbps to bit/s", 100.0, "Mbits/s", "bit/s", 0, 1.0e8},
		{"hours to seconds", 2.0, "h", "s", 0, 7200.0},
		{"explicit scale overrides", 50.0, "%", "1", 1000.0, 50000.0},
		{"no conversion match", 42.0, "", "", 0, 42.0},
		{"no conversion same unit", 42.0, "By", "By", 0, 42.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertValue(tt.raw, tt.sourceUnit, tt.otelUnit, tt.valueScale)
			if !floatApprox(got, tt.want) {
				t.Errorf("ConvertValue(%v, %q, %q, %v) = %v, want %v",
					tt.raw, tt.sourceUnit, tt.otelUnit, tt.valueScale, got, tt.want)
			}
		})
	}
}

func TestPromType(t *testing.T) {
	tests := []struct {
		otel, want string
	}{
		{"counter", "counter"},
		{"gauge", "gauge"},
		{"updowncounter", "gauge"},
		{"histogram", "histogram"},
		{"Counter", "counter"},
		{"", "gauge"},
	}
	for _, tt := range tests {
		if got := PromType(tt.otel); got != tt.want {
			t.Errorf("PromType(%q) = %q, want %q", tt.otel, got, tt.want)
		}
	}
}

func TestFormatLabels(t *testing.T) {
	values := map[string]string{
		"probe_name": "my-probe",
		"hw_state":   "ok",
		"cpu_mode":   "user",
	}
	got := FormatLabels([]string{"cpu_mode", "hw_state", "probe_name"}, values)
	want := `{cpu_mode="user",hw_state="ok",probe_name="my-probe"}`
	if got != want {
		t.Errorf("FormatLabels = %q, want %q", got, want)
	}

	// Empty labels
	if got := FormatLabels(nil, nil); got != "" {
		t.Errorf("FormatLabels(nil, nil) = %q, want empty", got)
	}
}

func TestLabelValueString_Escaping(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"simple", "simple"},
		{`with "quote"`, `with \"quote\"`},
		{`back\slash`, `back\\slash`},
		{"line1\nline2", `line1\nline2`},
	}
	for _, tt := range tests {
		if got := LabelValueString(tt.in); got != tt.want {
			t.Errorf("LabelValueString(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
