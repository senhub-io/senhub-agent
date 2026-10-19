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

// pendingLookupChannels were added in 0.6.4 with a mandatory lookup;
// their PRTG form is being decided (ent#715).
var pendingLookupChannels = map[string]bool{
	"http_check\tsenhub.probe.check.target.unresolved": true,
	"tcp_dial\tsenhub.probe.check.target.unresolved":   true,
	"systemd\thost.service.running":                    true,
	"winservices\thost.service.running":                true,
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
			if m.Lookup == "" || allowed[key] || pendingLookupChannels[key] || m.Name == checkResultMetric {
				continue
			}
			t.Errorf("%s.%s references PRTG lookup %q, which customer cores do not have", probe, m.Name, m.Lookup)
		}
	}
}
