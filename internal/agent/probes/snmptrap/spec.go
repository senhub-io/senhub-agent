package snmptrap

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "snmp_trap", DisplayName: "SNMP Trap Receiver", Category: "network",
		Summary:  "Receives SNMP traps (v2c or v3) on a UDP address and emits them as events.",
		DocsPath: "docs/user-guide/docs/probes/snmp-trap.md", MultiInstance: false,
		Params: []probes.ParamSpec{
			{Key: "bind_address", Kind: probes.KindString, Default: "127.0.0.1:162", Group: "connection", Description: "UDP listen address; port 162 needs root or CAP_NET_BIND_SERVICE"},
			{Key: "version", Kind: probes.KindString, Default: "v2c", Enum: []string{"v2c", "v3"}, Group: "connection"},
			{Key: "community", Kind: probes.KindString, Secret: true, Group: "auth", Description: "v2c community check; empty accepts any"},
			{Key: "mib_paths", Kind: probes.KindStringList, Group: "metrics", Description: "Local MIB files or folders for OID names"},
			{Key: "v3", Kind: probes.KindBlock, Group: "auth", Description: "SNMPv3 users", Fields: []probes.ParamSpec{
				{Key: "users", Kind: probes.KindBlockList, Required: true, Fields: []probes.ParamSpec{
					{Key: "username", Kind: probes.KindString, Required: true},
					{Key: "auth_protocol", Kind: probes.KindString, Enum: []string{"MD5", "SHA", "SHA224", "SHA256", "SHA384", "SHA512"}},
					{Key: "auth_password", Kind: probes.KindString, Secret: true},
					{Key: "priv_protocol", Kind: probes.KindString, Enum: []string{"DES", "AES", "AES192", "AES256"}},
					{Key: "priv_password", Kind: probes.KindString, Secret: true},
				}},
			}},
		},
	})
}
