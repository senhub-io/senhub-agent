package snmppoll

import "senhub-agent.go/internal/agent/probes"

// governanceFields describes the governance block, at the top level and
// inside a discovery rule alike.
var governanceFields = []probes.ParamSpec{
	{Key: "owner", Kind: probes.KindBlock, Description: "Who owns the device", Fields: []probes.ParamSpec{
		{Key: "team", Kind: probes.KindString, Description: "Owning team"},
		{Key: "contact", Kind: probes.KindString, Description: "Contact for the team"},
	}},
	{Key: "criticality", Kind: probes.KindString, Enum: []string{"critical", "high", "medium", "low"}, Description: "Business criticality"},
	{Key: "location", Kind: probes.KindBlock, Description: "Where the device is", Fields: []probes.ParamSpec{
		{Key: "site", Kind: probes.KindString},
		{Key: "datacenter", Kind: probes.KindString},
		{Key: "rack", Kind: probes.KindString},
		{Key: "room", Kind: probes.KindString},
	}},
	{Key: "lifecycle", Kind: probes.KindString, Description: "active, maintenance, decommissioning or retired"},
	{Key: "labels", Kind: probes.KindMap, Description: "Free-form labels emitted as entity attributes"},
}

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "snmp_poll", DisplayName: "SNMP Poll", Category: "network",
		Summary:  "Polls one network device by SNMP (v2c or v3) and, optionally, crawls its neighbours; one instance per device.",
		DocsPath: "docs/user-guide/docs/probes/snmp-poll.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "target", Kind: probes.KindString, Required: true, Group: "connection", Description: "Device address or hostname"},
			{Key: "port", Kind: probes.KindInt, Default: 161, Group: "connection", Description: "SNMP UDP port"},
			{Key: "version", Kind: probes.KindString, Default: "v2c", Enum: []string{"v2c", "v3", "2c", "3", "2"}, Group: "connection", Description: "SNMP version; v1 is refused"},
			{Key: "community", Kind: probes.KindString, Default: "public", Secret: true, Group: "auth", Description: "Community string (v2c)"},
			{Key: "v3", Kind: probes.KindBlock, Group: "auth", Description: "USM credentials, required with version v3", Fields: []probes.ParamSpec{
				{Key: "username", Kind: probes.KindString, Required: true, Description: "USM user"},
				{Key: "auth_protocol", Kind: probes.KindString, Enum: []string{"MD5", "SHA", "SHA224", "SHA256", "SHA384", "SHA512"}, Description: "Authentication protocol; empty for none"},
				{Key: "auth_passphrase", Kind: probes.KindString, Secret: true, Description: "Required with auth_protocol"},
				{Key: "priv_protocol", Kind: probes.KindString, Enum: []string{"DES", "AES", "AES192", "AES256"}, Description: "Privacy protocol; needs auth_protocol"},
				{Key: "priv_passphrase", Kind: probes.KindString, Secret: true, Description: "Required with priv_protocol"},
			}},
			{Key: "retries", Kind: probes.KindInt, Default: 2, Group: "collection", Description: "Retries per request"},
			{Key: "timeout", Kind: probes.KindDuration, Default: "5s", Group: "collection", Description: "Per-request timeout"},
			{Key: "interval", Kind: probes.KindDuration, Default: "60s", Group: "collection", Description: "Metric polling cadence"},
			{Key: "topology_interval", Kind: probes.KindDuration, Default: "10m", Group: "collection", Description: "Entity and topology sweep cadence"},
			{Key: "mibs", Kind: probes.KindStringList, Enum: []string{"mib-2", "if-mib"}, Group: "metrics", Description: "Built-in MIB modules to poll; this or custom_mappings is required"},
			{Key: "mib_paths", Kind: probes.KindStringList, Group: "metrics", Description: "Local MIB files or folders used to name custom mappings"},
			{Key: "custom_mappings", Kind: probes.KindBlockList, Group: "metrics", Description: "OID to metric mappings", Fields: []probes.ParamSpec{
				{Key: "oid", Kind: probes.KindString, Required: true, Description: "OID, leading dot optional"},
				{Key: "metric", Kind: probes.KindString, Description: "Metric name; resolved from mib_paths when omitted"},
				{Key: "type", Kind: probes.KindString, Default: "gauge", Enum: []string{"gauge", "counter"}},
				{Key: "index_label", Kind: probes.KindString, Description: "Walk the OID as a table and tag rows with this label"},
			}},
			{Key: "discovery", Kind: probes.KindBlock, Group: "discovery", Description: "Topology crawl from seed devices", Fields: []probes.ParamSpec{
				{Key: "seeds", Kind: probes.KindStringList, Required: true, Description: "Entry-point device addresses"},
				{Key: "profile", Kind: probes.KindBlock, Required: true, Description: "Credentials for crawled devices (v2c only)", Fields: []probes.ParamSpec{
					{Key: "version", Kind: probes.KindString, Default: "v2c", Enum: []string{"v2c", "2c", "2"}},
					{Key: "community", Kind: probes.KindString, Required: true, Secret: true},
				}},
				{Key: "allowed_cidrs", Kind: probes.KindStringList, Required: true, Description: "The crawl never leaves these ranges"},
				{Key: "max_devices", Kind: probes.KindInt, Default: 200},
				{Key: "max_hops", Kind: probes.KindInt, Default: 4},
				{Key: "interval", Kind: probes.KindDuration, Description: "Crawl cadence; topology_interval by default"},
				{Key: "governance_rules", Kind: probes.KindBlockList, Description: "Per-device governance by match", Fields: []probes.ParamSpec{
					{Key: "match", Kind: probes.KindBlock, Fields: []probes.ParamSpec{
						{Key: "cidr", Kind: probes.KindString},
						{Key: "vendor", Kind: probes.KindString},
						{Key: "sysname", Kind: probes.KindString, Description: "Regular expression"},
					}},
					{Key: "governance", Kind: probes.KindBlock, Fields: governanceFields},
				}},
			}},
			{Key: "governance", Kind: probes.KindBlock, Group: "governance", Description: "Ownership, criticality and location of the device", Fields: governanceFields},
		},
	})
}
