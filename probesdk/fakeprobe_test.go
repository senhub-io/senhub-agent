package probesdk_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"senhub-agent.go/probesdk/configparser"
	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/entity"
	"senhub-agent.go/probesdk/exporthook"
	"senhub-agent.go/probesdk/logger"
	"senhub-agent.go/probesdk/logs"
	"senhub-agent.go/probesdk/probes"
	"senhub-agent.go/probesdk/sanitize"
	"senhub-agent.go/probesdk/spec"
	"senhub-agent.go/probesdk/state"
	"senhub-agent.go/probesdk/tags"
	"senhub-agent.go/probesdk/types"
	"senhub-agent.go/probesdk/validators"
)

var (
	hookOnce            sync.Once
	seriesObserved      int64
	selfRecordsProvided int64
)

type sdkContractEntitySource struct {
	id map[string]any
}

func (s *sdkContractEntitySource) Observe() (entity.Observation, bool) {
	attrs := map[string]any{"sdkcontract.attr": "value"}
	if v, ok := s.id["sdkcontract.target.id"]; ok {
		attrs["sdkcontract.target.id.copy"] = v
	}

	sourceID := s.id
	target := entity.Entity{
		Type:       "sdkcontract.target",
		ID:         sourceID,
		Attributes: attrs,
	}
	relation := entity.Relation{
		Type:     "sdkcontract.relation",
		FromType: "sdkcontract.target",
		FromID:   sourceID,
		ToType:   "sdkcontract.target.to",
		ToID:     map[string]any{"sdkcontract.target.to.id": "target-to"},
	}
	return entity.Observation{
		Entities:  []entity.Entity{target},
		Relations: []entity.Relation{relation},
	}, true
}

type fakeProbe struct {
	types.BaseProbe
	params       map[string]interface{}
	moduleLogger *logger.ModuleLogger
}

func newFakeProbe(params map[string]interface{}, base *logger.Logger) (types.Probe, error) {
	if params == nil {
		params = map[string]interface{}{}
	}
	moduleLogger := logger.NewModuleLogger(base, "probe.sdkcontract")

	if u, ok := types.StringParam(params, "url"); ok {
		if !validators.IsURL(u) {
			return nil, fmt.Errorf("invalid url parameter: %s", u)
		}
	}
	if v, ok := params["timeout"]; ok {
		if _, err := configParser.ParseDuration(v); err != nil {
			return nil, fmt.Errorf("parsing timeout parameter: %w", err)
		}
	}

	statePath := state.Path("sdkcontract.state")
	moduleLogger.Debug().Msg("sdkcontract fake state path: " + statePath)

	targetID, ok := types.StringParam(params, "target_id")
	if !ok || targetID == "" {
		targetID = "sdkcontract-target-default"
	}

	p := &fakeProbe{
		params:       params,
		moduleLogger: moduleLogger,
	}
	p.SetEntitySource(&sdkContractEntitySource{
		id: map[string]any{"sdkcontract.target.id": targetID},
	})

	hookOnce.Do(func() {
		exporthook.RegisterSeriesObserver(func(_ string, points []exporthook.SeriesPoint, _ time.Time) {
			atomic.AddInt64(&seriesObserved, int64(len(points)))
		})
		exporthook.RegisterSelfRecordProvider(func(_ time.Time) []exporthook.SelfRecord {
			atomic.AddInt64(&selfRecordsProvided, 1)
			return nil
		})
	})

	return p, nil
}

func init() {
	probes.RegisterProbe("sdkcontract_fake", newFakeProbe)
	spec.Register(spec.Probe{
		Type:        "sdkcontract_fake",
		DisplayName: "SDK contract fake",
		Category:    "test",
		Params: []spec.ParamSpec{
			{Key: "interval", Kind: spec.KindDuration},
			{Key: "url", Kind: spec.KindString},
			{Key: "count", Kind: spec.KindInt},
			{Key: "flag", Kind: spec.KindBool},
			{Key: "names", Kind: spec.KindStringList},
		},
	})
}

func (p *fakeProbe) ShouldStart() bool { return true }

func (p *fakeProbe) GetInterval() time.Duration {
	if d, ok := types.DurationParam(p.params, "interval"); ok {
		return d
	}
	return 50 * time.Millisecond
}

func (p *fakeProbe) Collect() ([]datapoint.DataPoint, error) {
	now := time.Now()

	count, _ := types.IntParam(p.params, "count")
	flag, _ := types.BoolParam(p.params, "flag")
	names, _ := types.StringSliceParam(p.params, "names")
	urlVal, _ := types.StringParam(p.params, "url")

	urlTagKey, _ := tags.UrlToTagKey(urlVal)

	bytesVal, _ := sanitize.Bytes(int64(count) * 1024)

	dp1Tags := []tags.Tag{{Key: "sdkcontract.measurement", Value: "bytes"}}
	if urlTagKey != "" {
		dp1Tags = append(dp1Tags, tags.Tag{Key: urlTagKey, Value: urlVal})
	}
	dp1 := datapoint.DataPoint{
		Name:      "sdkcontract.bytes",
		Timestamp: now,
		Value:     bytesVal,
		Tags:      dp1Tags,
	}

	dp2Tags := []tags.Tag{
		{Key: "sdkcontract.flag", Value: strconv.FormatBool(flag)},
		{Key: "sdkcontract.names", Value: strings.Join(names, ",")},
	}
	dp2 := datapoint.DataPoint{
		Name:      "sdkcontract.count",
		Timestamp: now,
		Value:     float64(count),
		Tags:      dp2Tags,
	}

	logs.Publish(logs.Record{
		Timestamp:         now,
		Severity:          logs.SeverityInfo,
		SeverityText:      "INFO",
		Body:              "sdkcontract fake collect",
		Attributes:        map[string]string{"source": "sdkcontract"},
		ProducerProbeName: p.GetName(),
		ProducerProbeType: p.GetProbeType(),
	})

	return []datapoint.DataPoint{dp1, dp2}, nil
}

func (p *fakeProbe) OnStart(_ chan struct{}) error { return nil }

func (p *fakeProbe) OnShutdown(_ context.Context) error { return nil }
