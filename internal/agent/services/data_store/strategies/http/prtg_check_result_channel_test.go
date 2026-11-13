package http

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func checkResultConverter(t *testing.T, params map[string]interface{}) *FormatConverter {
	t.Helper()
	baseLogger := createTestLogger()
	moduleLogger := logger.NewModuleLogger(baseLogger, "test")
	cache := NewMetricCache(5*time.Minute, moduleLogger)
	registry := transformers.NewTransformerRegistry(baseLogger)
	probeTags := []tags.Tag{{Key: "probe_name", Value: "Lab-Redis"}, {Key: "probe_type", Value: "redis"}}
	cache.AddDataPointsWithTransformer([]datapoint.DataPoint{
		{Name: checkResultMetric, Value: 0.0, Timestamp: time.Now(), Tags: probeTags},
		{Name: "redis.clients.connected", Value: 3.0, Timestamp: time.Now(), Tags: probeTags},
	}, registry)
	fc := NewFormatConverter(registry, moduleLogger, cache)
	fc.checkResultChannel = NewConfigurationManager(nil, params, moduleLogger).IsPRTGCheckResultChannel
	return fc
}

func checkResultChannelOf(channels []PRTGChannel) *PRTGChannel {
	for i := range channels {
		if channels[i].ValueLookup == checkResultMetric || strings.Contains(channels[i].Channel, "Check Result") {
			return &channels[i]
		}
	}
	return nil
}

// A PRTG core ships no senhub.probe.check.result lookup, and a channel
// naming a lookup the core lacks turns the whole sensor Warning (PE272).
func TestPRTGLeavesCheckResultOutByDefault(t *testing.T) {
	channels := checkResultConverter(t, map[string]interface{}{}).GetMetricsForProbeWithFilter("Lab-Redis", MetricFilter{ShowTags: true})
	if c := checkResultChannelOf(channels); c != nil {
		t.Fatalf("Check Result served without prtg.check_result_channel: %+v", *c)
	}
	if len(channels) != 1 {
		t.Fatalf("got %d channels, want the one other metric", len(channels))
	}
}

func TestPRTGServesCheckResultWithItsLookupWhenEnabled(t *testing.T) {
	params := map[string]interface{}{"prtg": map[string]interface{}{"check_result_channel": true}}
	c := checkResultChannelOf(checkResultConverter(t, params).GetMetricsForProbeWithFilter("Lab-Redis", MetricFilter{ShowTags: true}))
	if c == nil {
		t.Fatal("Check Result missing with prtg.check_result_channel: true")
	}
	if c.ValueLookup != checkResultMetric {
		t.Errorf("ValueLookup = %q, want %q: customers installed the lookup under that name", c.ValueLookup, checkResultMetric)
	}
}

func TestPRTGCheckResultOptionResetsOnReload(t *testing.T) {
	moduleLogger := logger.NewModuleLogger(createTestLogger(), "test")
	cm := NewConfigurationManager(nil, map[string]interface{}{"prtg": map[string]interface{}{"check_result_channel": true}}, moduleLogger)
	if !cm.IsPRTGCheckResultChannel() {
		t.Fatal("option not read")
	}
	cm.params = map[string]interface{}{}
	cm.loadConfiguration()
	if cm.IsPRTGCheckResultChannel() {
		t.Error("option kept after it left the configuration")
	}
}

// No channel gets a mandatory PRTG lookup beyond those 0.6.3 had: an
// existing customer core lacks the file, and its sensors turn Warning on
// upgrade. A new status channel ships a plain value and a PRTG limit, or
// stays behind an option, as Check Result does.
func TestNoNewChannelRequiresAPRTGLookup(t *testing.T) {
	f, err := os.Open("testdata/prtg_lookup_channels_063.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	allowed := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := sc.Text(); line != "" && !strings.HasPrefix(line, "#") {
			allowed[line] = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	for probe, def := range defs {
		for _, m := range def.Metrics {
			key := probe + "\t" + m.Name
			if m.Lookup == "" || allowed[key] || optionalChannels[m.Name] || m.Name == serviceRunningMetric {
				continue
			}
			t.Errorf("%s.%s references PRTG lookup %q, which customer cores do not have", probe, m.Name, m.Lookup)
		}
	}
}

func statusConverter(t *testing.T, params map[string]interface{}) *FormatConverter {
	t.Helper()
	baseLogger := createTestLogger()
	moduleLogger := logger.NewModuleLogger(baseLogger, "test")
	cache := NewMetricCache(5*time.Minute, moduleLogger)
	registry := transformers.NewTransformerRegistry(baseLogger)
	now := time.Now()
	cache.AddDataPointsWithTransformer([]datapoint.DataPoint{
		{Name: unresolvedTargetMetric, Value: 1.0, Timestamp: now, Tags: []tags.Tag{{Key: "probe_name", Value: "Lab-TCP"}, {Key: "probe_type", Value: "tcp_dial"}}},
		{Name: "senhub.probe.check.duration", Value: 0.01, Timestamp: now, Tags: []tags.Tag{{Key: "probe_name", Value: "Lab-TCP"}, {Key: "probe_type", Value: "tcp_dial"}}},
	}, registry)
	fc := NewFormatConverter(registry, moduleLogger, cache)
	fc.checkResultChannel = NewConfigurationManager(nil, params, moduleLogger).IsPRTGCheckResultChannel
	return fc
}

func TestPRTGLeavesUnresolvedTargetOutByDefault(t *testing.T) {
	channels := statusConverter(t, map[string]interface{}{}).GetMetricsForProbeWithFilter("Lab-TCP", MetricFilter{ShowTags: true})
	for _, c := range channels {
		if c.ValueLookup == unresolvedTargetMetric || strings.Contains(strings.ToLower(c.Channel), "unresolved") {
			t.Fatalf("unresolved-target channel served without prtg.check_result_channel: %+v", c)
		}
	}
}

func TestPRTGServesUnresolvedTargetWithItsLookupWhenEnabled(t *testing.T) {
	params := map[string]interface{}{"prtg": map[string]interface{}{"check_result_channel": true}}
	channels := statusConverter(t, params).GetMetricsForProbeWithFilter("Lab-TCP", MetricFilter{ShowTags: true})
	for _, c := range channels {
		if c.ValueLookup == unresolvedTargetMetric {
			return
		}
	}
	t.Fatalf("no channel with lookup %q in %+v", unresolvedTargetMetric, channels)
}

func serviceConverter(t *testing.T, params map[string]interface{}) *FormatConverter {
	t.Helper()
	baseLogger := createTestLogger()
	moduleLogger := logger.NewModuleLogger(baseLogger, "test")
	cache := NewMetricCache(5*time.Minute, moduleLogger)
	registry := transformers.NewTransformerRegistry(baseLogger)
	now := time.Now()
	cache.AddDataPointsWithTransformer([]datapoint.DataPoint{
		{Name: serviceRunningMetric, Value: 0.0, Timestamp: now, Tags: []tags.Tag{{Key: "probe_name", Value: "Lab-Units"}, {Key: "probe_type", Value: "systemd"}, {Key: "systemd.unit", Value: "nginx.service"}, {Key: "host.service.name", Value: "nginx"}}},
		{Name: serviceRunningMetric, Value: 0.0, Timestamp: now, Tags: []tags.Tag{{Key: "probe_name", Value: "Lab-Services"}, {Key: "probe_type", Value: "winservices"}, {Key: "windows.service.name", Value: "Spooler"}, {Key: "host.service.name", Value: "Spooler"}}},
	}, registry)
	fc := NewFormatConverter(registry, moduleLogger, cache)
	fc.checkResultChannel = NewConfigurationManager(nil, params, moduleLogger).IsPRTGCheckResultChannel
	return fc
}

// Up to 0.6.3 a "Service X Running" channel was a plain number, and PRTG
// fixes a channel's type when it creates it: a lookup sent later on that
// channel turns the sensor Warning (PE272) even with the lookup file
// installed. So the channel never names a lookup, with or without
// prtg.check_result_channel, and a stopped service turns the sensor
// Error through a limit below 1.
func TestPRTGServiceChannelNeverCarriesALookup(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params map[string]interface{}
	}{
		{"default", map[string]interface{}{}},
		{"check_result_channel", map[string]interface{}{"prtg": map[string]interface{}{"check_result_channel": true}}},
	} {
		for _, probe := range []string{"Lab-Units", "Lab-Services"} {
			t.Run(tc.name+"/"+probe, func(t *testing.T) {
				channels := serviceConverter(t, tc.params).GetMetricsForProbeWithFilter(probe, MetricFilter{ShowTags: true})
				if len(channels) != 1 {
					t.Fatalf("got %d channels, want the service channel: %+v", len(channels), channels)
				}
				c := channels[0]
				if !strings.HasPrefix(c.Channel, "Service ") || !strings.HasSuffix(c.Channel, " Running") {
					t.Fatalf("channel = %q, want Service <name> Running", c.Channel)
				}
				if c.ValueLookup != "" {
					t.Errorf("ValueLookup = %q, want none", c.ValueLookup)
				}
				if c.Float == nil || *c.Float != 1 {
					t.Errorf("Float = %v, want 1 as up to 0.6.3", c.Float)
				}
				if c.Value != 0 {
					t.Errorf("Value = %v, want 0 for a stopped service", c.Value)
				}
				if c.LimitMode != 1 || c.LimitMinError <= 0 || c.LimitMinError >= 1 {
					t.Errorf("limit = mode %d min error %v, want mode 1 with an error below 1", c.LimitMode, c.LimitMinError)
				}
			})
		}
	}
}
