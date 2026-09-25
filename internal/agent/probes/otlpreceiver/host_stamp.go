package otlpreceiver

import (
	"net"
	"strings"
	"sync"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
)

// The receiver states the sender's host when, and only when, the sender is
// on this machine. For a local sender "relayed by" and "runs on" name the
// same host, so the agent restates a fact it holds on its own host entity;
// for a remote one it would be a guess that asserts an identity, which is
// exactly what must never be done. A remote sender keeps what it sent and
// the export adds telemetry.relay.* as before.
//
// Three paths, each counted apart so a missing host.id reads as expected
// or as a defect depending on where it came from:
//
//	uds           a Unix domain socket: local by construction.
//	tcp_loopback  a loopback TCP peer whose Resource carries no
//	              telemetry.relay.*: a local proxy that is itself an
//	              OpenTelemetry relay is not the origin.
//	remote        everything else, never stamped.
//
// host.* is one set: when the sender set any host.* key, nothing is added.
// Adding host.id beside a host.name that names something else (a pod, a
// container) would give one record two contradicting identities.
const (
	originUDS         = "uds"
	originTCPLoopback = "tcp_loopback"
	originRemote      = "remote"
)

const (
	attrHostID      = "host.id"
	attrHostName    = "host.name"
	attrServiceName = "service.name"
	relayKeyPrefix  = "telemetry.relay."
	hostKeyPrefix   = "host."
)

// hostStamp is this agent's own host identity, the values of its host
// entity.
type hostStamp struct{ id, name string }

var ownHost = sync.OnceValue(func() hostStamp {
	hi, err := common.GetHostIdentity()
	if err != nil {
		return hostStamp{}
	}
	return hostStamp{id: hi.ID, name: hi.Name}
})

// origin classifies a request by where it came from.
func (p *OTLPReceiverProbe) origin(remoteAddr string) string {
	if p.config.UnixPath != "" {
		return originUDS
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return originTCPLoopback
	}
	return originRemote
}

// stampResource adds this host's identity to one Resource when the path
// allows it and the sender stated no host.* of its own.
func stampResource(res *resourcepb.Resource, origin string, own hostStamp) {
	if res == nil || own.id == "" || origin == originRemote {
		return
	}
	for _, kv := range res.Attributes {
		if strings.HasPrefix(kv.Key, hostKeyPrefix) {
			return
		}
		if origin == originTCPLoopback && strings.HasPrefix(kv.Key, relayKeyPrefix) {
			return
		}
	}
	res.Attributes = append(res.Attributes, stringAttr(attrHostID, own.id))
	if own.name != "" {
		res.Attributes = append(res.Attributes, stringAttr(attrHostName, own.name))
	}
}

func stringAttr(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
}

func resourceString(res *resourcepb.Resource, key string) string {
	if res == nil {
		return ""
	}
	for _, kv := range res.Attributes {
		if kv.Key == key {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

// account stamps one Resource and counts its records for the coverage
// self-metric, by signal, path and service.
func account(signal, origin string, res *resourcepb.Resource, records int, own hostStamp) {
	stampResource(res, origin, own)
	agentstate.RecordOTLPReceiverCoverage(signal, origin, resourceString(res, attrServiceName), records, resourceString(res, attrHostID) != "")
}

func (p *OTLPReceiverProbe) stampMetrics(remoteAddr string, rms []*metricpb.ResourceMetrics) {
	origin, own := p.origin(remoteAddr), ownHost()
	for _, rm := range rms {
		if rm.Resource == nil {
			rm.Resource = &resourcepb.Resource{}
		}
		account(signalMetrics, origin, rm.Resource, countMetricPoints(rm), own)
	}
}

func (p *OTLPReceiverProbe) stampLogs(remoteAddr string, rls []*logspb.ResourceLogs) {
	origin, own := p.origin(remoteAddr), ownHost()
	for _, rl := range rls {
		if rl.Resource == nil {
			rl.Resource = &resourcepb.Resource{}
		}
		n := 0
		for _, sl := range rl.ScopeLogs {
			n += len(sl.LogRecords)
		}
		account(signalLogs, origin, rl.Resource, n, own)
	}
}

func (p *OTLPReceiverProbe) stampSpans(remoteAddr string, rss []*tracepb.ResourceSpans) {
	origin, own := p.origin(remoteAddr), ownHost()
	for _, rs := range rss {
		if rs.Resource == nil {
			rs.Resource = &resourcepb.Resource{}
		}
		n := 0
		for _, ss := range rs.ScopeSpans {
			n += len(ss.Spans)
		}
		account(signalTraces, origin, rs.Resource, n, own)
	}
}

func countMetricPoints(rm *metricpb.ResourceMetrics) int {
	n := 0
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch d := m.Data.(type) {
			case *metricpb.Metric_Gauge:
				n += len(d.Gauge.DataPoints)
			case *metricpb.Metric_Sum:
				n += len(d.Sum.DataPoints)
			case *metricpb.Metric_Histogram:
				n += len(d.Histogram.DataPoints)
			case *metricpb.Metric_ExponentialHistogram:
				n += len(d.ExponentialHistogram.DataPoints)
			case *metricpb.Metric_Summary:
				n += len(d.Summary.DataPoints)
			}
		}
	}
	return n
}
