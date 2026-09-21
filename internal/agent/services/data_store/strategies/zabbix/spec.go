package zabbix

import (
	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/data_store/outputspec"
)

func init() {
	outputspec.Register(outputspec.Output{
		Type: "zabbix", DisplayName: "Zabbix (active agent)", Mode: outputspec.ModePush,
		Summary:  "Registers this host on a Zabbix server or proxy and pushes the collected values as an active agent.",
		DocsPath: "docs/user-guide/docs/zabbix.md",
		Params: []spec.ParamSpec{
			{Key: "server", Kind: spec.KindString, Required: true, Group: "connection", Description: "Zabbix server or proxy, host:port; 10051 when the port is omitted. Several addresses separated by commas name a proxy group, whose members redirect the agent to whichever holds this host", Example: "zabbix.example.com:10051"},
			{Key: "hostname", Kind: spec.KindString, Group: "connection", Description: "Name this host registers under; the machine's host name by default"},
			{Key: "host_metadata", Kind: spec.KindString, Default: defaultHostMetadata, Group: "connection", Description: "Sent with every check-list request; the autoregistration action matches on it to pick groups and templates. The agent appends its operating system, so \"senhub-agent\" registers as \"senhub-agent linux\", which is how the per-platform templates are chosen"},
			{Key: "interval", Kind: spec.KindDuration, Default: "60s", Group: "delivery", Description: "Push cadence of the collected values"},
			{Key: "refresh_interval", Kind: spec.KindDuration, Default: "120s", Group: "delivery", Description: "How often the item list is asked again"},
			{Key: "heartbeat_interval", Kind: spec.KindDuration, Default: "60s", Group: "delivery", Description: "Heartbeat cadence; the server declares the host unavailable after twice that"},
			{Key: "timeout", Kind: spec.KindDuration, Default: "10s", Group: "delivery", Description: "Bound on one connection, request and reply"},
			{Key: "key_prefix", Kind: spec.KindString, Default: defaultKeyPrefix, Group: "delivery", Description: "First segment of every item key"},
			{Key: "passive", Kind: spec.KindBlock, Group: "passive", Description: "Listener the server polls like a classic agent: answers agent.ping and the same item keys", Fields: []spec.ParamSpec{
				{Key: "enabled", Kind: spec.KindBool, Default: false, Description: "Answer the server's polls on the passive port"},
				{Key: "bind_address", Kind: spec.KindString, Default: defaultPassiveBind, Description: "Address the listener binds to"},
				{Key: "port", Kind: spec.KindInt, Default: defaultPassivePort, Description: "Port the listener binds to; sent to the server so autoregistration creates the interface on it"},
				{Key: "allow", Kind: spec.KindStringList, Description: "Addresses or CIDR ranges allowed to poll the passive port; every configured server address when empty, since any member of a proxy group may be the one polling"},
			}},
			{Key: "tls", Kind: spec.KindBlock, Group: "tls", Description: "Certificate-based encryption of the connection (Zabbix pre-shared keys are not supported)", Fields: []spec.ParamSpec{
				{Key: "enabled", Kind: spec.KindBool, Default: false, Description: "Encrypt the connection with TLS"},
				{Key: "ca_file", Kind: spec.KindString, Description: "CA certificate that signed the server's certificate"},
				{Key: "cert_file", Kind: spec.KindString, Description: "Client certificate presented to the server"},
				{Key: "key_file", Kind: spec.KindString, Secret: true, Description: "Private key of the client certificate"},
				{Key: "server_name", Kind: spec.KindString, Description: "Name expected in the server's certificate when it differs from the address"},
				{Key: "insecure_skip_verify", Kind: spec.KindBool, Default: false, Description: "Skip the server certificate check"},
			}},
		},
	})
}
