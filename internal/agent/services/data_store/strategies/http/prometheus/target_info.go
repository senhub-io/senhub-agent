package prometheus

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// target_info is the standard OTel mechanism for carrying resource attributes
// into Prometheus: one series per resource, value 1, joined onto the metrics by
// (job, instance).
//
// It exists here because the scrape endpoint has no resource concept, so an
// entity in the topology graph could not be pivoted to the series scraped
// directly from this agent — there was no host.id and no service.instance.id
// anywhere in the output (#745). The round trip that holds on the OTLP rail did
// not hold here.
//
// The alternative — promoting identity keys to per-datapoint labels — is
// explicitly what the entity/telemetry contract forbids, and it would add that
// cardinality to every operator scraping the agent, most of whom have no use
// for it. target_info costs one series in total.
//
//	target_info{host_id="...",host_name="...",service_instance_id="..."} 1
//
// Joined in PromQL the usual way:
//
//	senhub_system_cpu_utilization_ratio * on(job, instance) group_left(host_id) target_info

const targetInfoName = "target_info"

// writeTargetInfo emits the target_info series from the resource attributes.
// A no-op when there are none, so an endpoint with nothing to say stays silent
// rather than exposing an empty series that a join would match on nothing.
func writeTargetInfo(w io.Writer, resource map[string]string) error {
	labels := make([]string, 0, len(resource))
	for k, v := range resource {
		if v == "" {
			// An attribute the agent could not resolve is omitted, not
			// exported empty: `host_id=""` joins to nothing and reads as an
			// identity rather than as an absence.
			continue
		}
		labels = append(labels, fmt.Sprintf(`%s="%s"`, sanitizeLabelName(k), escapeLabelValue(v)))
	}
	if len(labels) == 0 {
		return nil
	}
	sort.Strings(labels)

	if _, err := fmt.Fprintf(w, "# HELP %s Resource attributes of the agent exposing these metrics\n", targetInfoName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s gauge\n", targetInfoName); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s{%s} 1\n\n", targetInfoName, strings.Join(labels, ","))
	return err
}

// sanitizeLabelName converts an OTel attribute key to a Prometheus label name:
// dots and dashes become underscores, which is the same transformation the
// OTel Prometheus exporters apply (host.id -> host_id).
func sanitizeLabelName(k string) string {
	out := make([]rune, 0, len(k))
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// escapeLabelValue applies the Prometheus text-format escaping: backslash,
// double quote and newline. Anything else is passed through, including UTF-8,
// which the format allows in label values.
func escapeLabelValue(v string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
	)
	return r.Replace(v)
}
