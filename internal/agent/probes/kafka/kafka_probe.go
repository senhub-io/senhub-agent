// Package kafka implements a Kafka broker/topic/consumer-group monitoring probe.
//
// The probe uses the sarama Admin API to collect cluster metadata (brokers,
// topics, partitions, replicas) and the Consumer Group API to compute per-group
// lag. It is the FREE-tier counterpart of the OTel Collector's kafkametricsreceiver,
// exposing a compatible metric set for PRTG/Nagios/OTLP/Prometheus outputs.
//
// Connection: sarama.NewClusterAdmin + sarama.NewClient against the configured
// bootstrap broker list. Both are recreated at the start of every Collect cycle
// so transient broker restarts self-heal without manual intervention.
//
// Filtering: topics whose names begin with "__" (internal Kafka topics such as
// __consumer_offsets and __transaction_state) are excluded by default. When
// topic_filter or group_filter are non-empty, only matching names (path.Match
// globs) are collected.
//
// On unreachable brokers the probe emits senhub.kafka.up=0 and returns nil so
// the framework keeps the probe alive and continues collecting at the next
// interval rather than marking it permanently failed.
package kafka

import (
	"context"
	"crypto/tls"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/IBM/sarama"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// adminFactory and clientFactory allow injection of fakes in tests.
type adminFactory func(brokers []string, cfg *sarama.Config) (sarama.ClusterAdmin, error)
type clientFactory func(brokers []string, cfg *sarama.Config) (sarama.Client, error)

// kafkaProbe collects Kafka cluster, topic, partition and consumer-group metrics.
type kafkaProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger

	// seams for unit testing
	newAdmin  adminFactory
	newClient clientFactory
}

// NewKafkaProbe constructs the probe from its raw config map.
func NewKafkaProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.kafka")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	moduleLogger.Debug().
		Strs("brokers", cfg.Brokers).
		Str("protocol_version", cfg.ProtocolVersion).
		Msg("Creating new kafka probe")

	p := &kafkaProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		newAdmin:     sarama.NewClusterAdmin,
		newClient:    sarama.NewClient,
	}
	p.SetProbeType(ProbeType)
	return p, nil
}

func (p *kafkaProbe) ShouldStart() bool { return true }

func (p *kafkaProbe) GetInterval() time.Duration { return p.cfg.Interval }

// OnStart is a no-op: connections are created fresh on every Collect cycle.
func (p *kafkaProbe) OnStart(_ chan struct{}) error { return nil }

// OnShutdown is a no-op: connections are closed within each Collect call.
func (p *kafkaProbe) OnShutdown(_ context.Context) error { return nil }

// Collect runs one collection cycle against the Kafka cluster.
// On connection failure it emits senhub.kafka.up=0 and returns nil.
func (p *kafkaProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()

	sarCfg, err := p.buildSaramaConfig()
	if err != nil {
		p.moduleLogger.Error().Err(err).Msg("kafka: failed to build sarama config")
		return p.upPoint(0, now), nil
	}

	admin, err := p.newAdmin(p.cfg.Brokers, sarCfg)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Strs("brokers", p.cfg.Brokers).Msg("kafka: cannot connect (admin)")
		return p.upPoint(0, now), nil
	}
	defer func() {
		if cerr := admin.Close(); cerr != nil {
			p.moduleLogger.Warn().Err(cerr).Msg("kafka: error closing admin client")
		}
	}()

	client, err := p.newClient(p.cfg.Brokers, sarCfg)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Strs("brokers", p.cfg.Brokers).Msg("kafka: cannot connect (client)")
		return p.upPoint(0, now), nil
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			p.moduleLogger.Warn().Err(cerr).Msg("kafka: error closing client")
		}
	}()

	var points []data_store.DataPoint

	// senhub.kafka.up
	points = append(points, dp("senhub.kafka.up", 1, now,
		tags.Tag{Key: "metric_type", Value: "broker"},
	))

	// kafka.brokers
	brokers := client.Brokers()
	points = append(points, dp("kafka.brokers", float32(len(brokers)), now,
		tags.Tag{Key: "metric_type", Value: "broker"},
	))

	// Topic + partition metrics
	topicMeta, err := admin.ListTopics()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("kafka: ListTopics failed")
	} else {
		topicPoints, topicPartitions := p.collectTopicMetrics(client, topicMeta, now)
		points = append(points, topicPoints...)

		// Consumer group metrics (requires the partition map)
		groupPoints := p.collectGroupMetrics(admin, client, topicPartitions, now)
		points = append(points, groupPoints...)
	}

	enriched := p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName())
	return enriched, nil
}

// collectTopicMetrics returns per-topic and per-topic/partition datapoints.
// It also returns the map of topic→partitions for subsequent group-lag calculation.
func (p *kafkaProbe) collectTopicMetrics(
	client sarama.Client,
	topicMeta map[string]sarama.TopicDetail,
	now time.Time,
) ([]data_store.DataPoint, map[string][]int32) {
	var points []data_store.DataPoint
	topicPartitions := make(map[string][]int32)

	for topic, detail := range topicMeta {
		if isInternalTopic(topic) {
			continue
		}
		if !p.matchesTopic(topic) {
			continue
		}

		numPartitions := detail.NumPartitions
		points = append(points, dp("kafka.topic.partitions", float32(numPartitions), now,
			tags.Tag{Key: "metric_type", Value: "topic"},
			tags.Tag{Key: "topic", Value: topic},
		))

		partitions := make([]int32, 0, numPartitions)
		for i := int32(0); i < numPartitions; i++ {
			partitions = append(partitions, i)
		}
		topicPartitions[topic] = partitions

		for _, partition := range partitions {
			partStr := strconv.Itoa(int(partition))

			newestOffset, err := client.GetOffset(topic, partition, sarama.OffsetNewest)
			if err != nil {
				p.moduleLogger.Warn().Err(err).
					Str("topic", topic).Int32("partition", partition).
					Msg("kafka: GetOffset(newest) failed")
			} else {
				points = append(points, dp("kafka.partition.current_offset", float32(newestOffset), now,
					tags.Tag{Key: "metric_type", Value: "partition"},
					tags.Tag{Key: "topic", Value: topic},
					tags.Tag{Key: "partition", Value: partStr},
				))
			}

			oldestOffset, err := client.GetOffset(topic, partition, sarama.OffsetOldest)
			if err != nil {
				p.moduleLogger.Warn().Err(err).
					Str("topic", topic).Int32("partition", partition).
					Msg("kafka: GetOffset(oldest) failed")
			} else {
				points = append(points, dp("kafka.partition.oldest_offset", float32(oldestOffset), now,
					tags.Tag{Key: "metric_type", Value: "partition"},
					tags.Tag{Key: "topic", Value: topic},
					tags.Tag{Key: "partition", Value: partStr},
				))
			}

			// Replicas from topic detail
			replicas := detail.ReplicaAssignment[partition]
			points = append(points,
				dp("kafka.partition.replicas", float32(len(replicas)), now,
					tags.Tag{Key: "metric_type", Value: "partition"},
					tags.Tag{Key: "topic", Value: topic},
					tags.Tag{Key: "partition", Value: partStr},
				),
			)
		}
	}

	return points, topicPartitions
}

// collectGroupMetrics returns per-group, per-group/topic, and
// per-group/topic/partition datapoints.
func (p *kafkaProbe) collectGroupMetrics(
	admin sarama.ClusterAdmin,
	client sarama.Client,
	topicPartitions map[string][]int32,
	now time.Time,
) []data_store.DataPoint {
	var points []data_store.DataPoint

	groups, err := admin.ListConsumerGroups()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("kafka: ListConsumerGroups failed")
		return points
	}

	for group := range groups {
		if !p.matchesGroup(group) {
			continue
		}

		// Group membership
		groupDesc, err := admin.DescribeConsumerGroups([]string{group})
		if err != nil {
			p.moduleLogger.Warn().Err(err).Str("group", group).Msg("kafka: DescribeConsumerGroups failed")
		} else if len(groupDesc) > 0 {
			members := len(groupDesc[0].Members)
			points = append(points, dp("kafka.consumer_group.members", float32(members), now,
				tags.Tag{Key: "metric_type", Value: "consumer_group"},
				tags.Tag{Key: "group", Value: group},
			))
		}

		if len(topicPartitions) == 0 {
			continue
		}

		// Group offsets per topic/partition
		groupOffsets, err := admin.ListConsumerGroupOffsets(group, topicPartitions)
		if err != nil {
			p.moduleLogger.Warn().Err(err).Str("group", group).Msg("kafka: ListConsumerGroupOffsets failed")
			continue
		}

		// lag_sum accumulator: topic → sum
		lagSum := make(map[string]float32)

		for topic, partOffsets := range groupOffsets.Blocks {
			if isInternalTopic(topic) {
				continue
			}
			if !p.matchesTopic(topic) {
				continue
			}

			for partition, block := range partOffsets {
				if block.Err != sarama.ErrNoError {
					p.moduleLogger.Warn().
						Str("group", group).Str("topic", topic).Int32("partition", partition).
						Msgf("kafka: group offset block error: %v", block.Err)
					continue
				}

				partStr := strconv.Itoa(int(partition))
				groupOffset := float32(block.Offset)

				points = append(points, dp("kafka.consumer_group.offset", groupOffset, now,
					tags.Tag{Key: "metric_type", Value: "consumer_group"},
					tags.Tag{Key: "group", Value: group},
					tags.Tag{Key: "topic", Value: topic},
					tags.Tag{Key: "partition", Value: partStr},
				))

				// Compute lag = newest_offset - group_offset (min 0)
				newestOffset, err := client.GetOffset(topic, partition, sarama.OffsetNewest)
				if err != nil {
					p.moduleLogger.Warn().Err(err).
						Str("group", group).Str("topic", topic).Int32("partition", partition).
						Msg("kafka: GetOffset(newest) for lag failed")
					continue
				}

				lag := float32(newestOffset) - groupOffset
				if lag < 0 {
					lag = 0
				}

				points = append(points, dp("kafka.consumer_group.lag", lag, now,
					tags.Tag{Key: "metric_type", Value: "consumer_group"},
					tags.Tag{Key: "group", Value: group},
					tags.Tag{Key: "topic", Value: topic},
					tags.Tag{Key: "partition", Value: partStr},
				))

				lagSum[topic] += lag
			}

			// Emit per-group/topic lag_sum
			points = append(points, dp("kafka.consumer_group.lag_sum", lagSum[topic], now,
				tags.Tag{Key: "metric_type", Value: "consumer_group"},
				tags.Tag{Key: "group", Value: group},
				tags.Tag{Key: "topic", Value: topic},
			))
		}
	}

	return points
}

// buildSaramaConfig assembles a sarama.Config from the probe configuration.
func (p *kafkaProbe) buildSaramaConfig() (*sarama.Config, error) {
	cfg := sarama.NewConfig()
	cfg.Net.DialTimeout = p.cfg.Timeout
	cfg.Net.ReadTimeout = p.cfg.Timeout
	cfg.Net.WriteTimeout = p.cfg.Timeout

	ver, err := sarama.ParseKafkaVersion(p.cfg.ProtocolVersion)
	if err != nil {
		return nil, fmt.Errorf("kafka: invalid protocol_version %q: %w", p.cfg.ProtocolVersion, err)
	}
	cfg.Version = ver

	if p.cfg.TLS {
		cfg.Net.TLS.Enable = true
		cfg.Net.TLS.Config = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	switch strings.ToUpper(p.cfg.SASLMechanism) {
	case "PLAIN":
		cfg.Net.SASL.Enable = true
		cfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		cfg.Net.SASL.User = p.cfg.SASLUsername
		cfg.Net.SASL.Password = p.cfg.SASLPassword
	case "SCRAM-SHA-256":
		cfg.Net.SASL.Enable = true
		cfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
		cfg.Net.SASL.User = p.cfg.SASLUsername
		cfg.Net.SASL.Password = p.cfg.SASLPassword
		cfg.Net.SASL.SCRAMClientGeneratorFunc = scramSHA256Generator
	case "SCRAM-SHA-512":
		cfg.Net.SASL.Enable = true
		cfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
		cfg.Net.SASL.User = p.cfg.SASLUsername
		cfg.Net.SASL.Password = p.cfg.SASLPassword
		cfg.Net.SASL.SCRAMClientGeneratorFunc = scramSHA512Generator
	}

	return cfg, nil
}

// upPoint returns the minimal point set emitted when the cluster is unreachable.
func (p *kafkaProbe) upPoint(val float32, now time.Time) []data_store.DataPoint {
	pts := []data_store.DataPoint{
		dp("senhub.kafka.up", val, now, tags.Tag{Key: "metric_type", Value: "broker"}),
	}
	return p.BaseProbe.EnrichDataPointsWithProbeName(pts, p.GetName())
}

// isInternalTopic returns true for Kafka-internal topics (name starts with "__").
func isInternalTopic(name string) bool {
	return strings.HasPrefix(name, "__")
}

// matchesTopic returns true when the topic passes the configured filter.
func (p *kafkaProbe) matchesTopic(topic string) bool {
	if len(p.cfg.TopicFilter) == 0 {
		return true
	}
	for _, glob := range p.cfg.TopicFilter {
		if ok, _ := path.Match(glob, topic); ok {
			return true
		}
	}
	return false
}

// matchesGroup returns true when the consumer group passes the configured filter.
func (p *kafkaProbe) matchesGroup(group string) bool {
	if len(p.cfg.GroupFilter) == 0 {
		return true
	}
	for _, glob := range p.cfg.GroupFilter {
		if ok, _ := path.Match(glob, group); ok {
			return true
		}
	}
	return false
}

// dp is a convenience constructor for a DataPoint.
func dp(name string, value float32, ts time.Time, extraTags ...tags.Tag) data_store.DataPoint {
	return data_store.DataPoint{
		Name:      name,
		Value:     value,
		Timestamp: ts,
		Tags:      extraTags,
	}
}
