package ibmi

import (
	"fmt"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/tags"
)

// serviceAgentCollector surfaces the state of IBM Electronic Service
// Agent (ESA), the background process that sends hardware/software
// problem reports and inventory data to IBM support. ESA is almost
// always enabled on production LPARs under IBM warranty — tracking
// its activation status and the age of the last inventory collection
// is a classic "is support posture healthy?" signal.
//
// **Opt-in collector.** QSYS2.ELECTRONIC_SERVICE_AGENT_INFO requires
// `*ALLOBJ` special authority (verified on PUB400 on 2026-04-17 —
// the PGMR profile returns SQL0443 / ERRNO 3401). Activate via
// `enabled_collectors: [..., service_agent]` in the probe config on
// deployments where the probe profile has been granted *ALLOBJ.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-electronic-service-agent-info
//
// Column names follow the IBM 7.5 documentation. The Parse path is
// intentionally tolerant of columns being absent or NULL — different
// PTF levels occasionally rename or add fields, and we'd rather
// silently skip a missing column than crash the whole collector.
type serviceAgentCollector struct{}

func newServiceAgentCollector() *serviceAgentCollector { return &serviceAgentCollector{} }

func (*serviceAgentCollector) Name() string  { return "service_agent" }
func (*serviceAgentCollector) IsEvent() bool { return false }

func (*serviceAgentCollector) SQL() string {
	return "SELECT ACTIVATION_STATUS, LAST_INVENTORY_COLLECTION_TIMESTAMP, LAST_HARDWARE_PROBLEM_SEND, LAST_SOFTWARE_PROBLEM_SEND FROM QSYS2.ELECTRONIC_SERVICE_AGENT_INFO"
}

func (*serviceAgentCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	// ELECTRONIC_SERVICE_AGENT_INFO is a single-row view. Zero rows
	// is anomalous (ESA never configured) but not an error — we
	// emit the activated=0 signal so dashboards can alert on it.
	if len(res.Rows) == 0 {
		return []datapoint.DataPoint{{
			Name:      "ibmi.service_agent.activated",
			Timestamp: ts,
			Value:     0,
			Tags:      []tags.Tag{hostTag},
		}}, nil
	}

	row := res.Rows[0]
	status := trimmedCell(row, idx, "ACTIVATION_STATUS")

	var activated float32
	if strings.EqualFold(status, "ENABLED") || strings.EqualFold(status, "ACTIVATED") {
		activated = 1
	}

	points := []datapoint.DataPoint{
		{
			Name:      "ibmi.service_agent.activated",
			Timestamp: ts,
			Value:     activated,
			Tags:      []tags.Tag{hostTag, {Key: "status", Value: status}},
		},
	}

	// Emit an "age in seconds" gauge for each of the timestamp
	// columns. Missing/NULL timestamps produce no datapoint —
	// dashboards can alert on the presence of a fresh sample.
	ageFromColumn := func(column, metricName string) {
		raw, present, _ := requireCell(row, idx, column)
		if !present || raw == "" {
			return
		}
		parsed, err := time.ParseInLocation(ibmiTimestampLayout, raw, time.Local)
		if err != nil {
			parsed, err = time.ParseInLocation(ibmiTimestampShortLayout, raw, time.Local)
			if err != nil {
				return
			}
		}
		age := ts.Sub(parsed).Seconds()
		if age < 0 {
			age = 0
		}
		points = append(points, datapoint.DataPoint{
			Name:      metricName,
			Timestamp: ts,
			Value:     float32(age),
			Tags:      []tags.Tag{hostTag},
		})
	}
	ageFromColumn("LAST_INVENTORY_COLLECTION_TIMESTAMP", "ibmi.service_agent.last_inventory_age_seconds")
	ageFromColumn("LAST_HARDWARE_PROBLEM_SEND", "ibmi.service_agent.last_hw_problem_send_age_seconds")
	ageFromColumn("LAST_SOFTWARE_PROBLEM_SEND", "ibmi.service_agent.last_sw_problem_send_age_seconds")

	if len(points) == 0 {
		return nil, fmt.Errorf("ELECTRONIC_SERVICE_AGENT_INFO produced no usable datapoints")
	}
	return points, nil
}
