package kafka

import "senhub-agent.go/internal/agent/probes"

func init() {
	saslOn := []probes.Condition{{Key: "sasl_mechanism", Values: []string{"PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"}}}
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "kafka", DisplayName: "Apache Kafka", Category: "messaging",
		Summary:  "Brokers, topic partitions, offsets, replicas and consumer-group lag of a Kafka cluster; one instance per cluster.",
		DocsPath: "docs/user-guide/docs/probes/kafka.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "brokers", Kind: probes.KindStringList, Default: []string{"localhost:9092"}, Essential: true, Group: "connection", Description: "Bootstrap brokers as host:port entries", Example: "kafka-1.example.com:9092"},
			{Key: "protocol_version", Kind: probes.KindString, Default: "2.0.0", Group: "connection", Description: "Kafka protocol version negotiated with the brokers"},
			{Key: "tls", Kind: probes.KindBool, Default: false, Group: "tls", Description: "Connect to the brokers over TLS"},
			{Key: "sasl_mechanism", Kind: probes.KindString, Enum: []string{"PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"}, Group: "auth", Description: "SASL mechanism; empty connects without authentication"},
			{Key: "sasl_username", Kind: probes.KindString, EssentialWhen: saslOn, Group: "auth", Description: "SASL user, required with sasl_mechanism"},
			{Key: "sasl_password", Kind: probes.KindString, Secret: true, EssentialWhen: saslOn, Group: "auth", Description: "SASL password, required with sasl_mechanism"},
			{Key: "topic_filter", Kind: probes.KindStringList, Group: "filter", Description: "Glob patterns of the topics to monitor; empty monitors every non-internal topic", Example: "orders-*"},
			{Key: "group_filter", Kind: probes.KindStringList, Group: "filter", Description: "Glob patterns of the consumer groups to monitor; empty monitors every group"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Broker request timeout in seconds"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this cluster instead of the cluster id it reports"},
		},
	})
}
