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
	"io"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"

	"senhub-agent.go/internal/agent/probes/dbcommon"
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

	// entity rail (Toise topology)
	entitySrc *kafkaEntitySource

	// seams for unit testing
	newAdmin  adminFactory
	newClient clientFactory
}

// baseTags returns the entity identity tags shared by every datapoint.
func (p *kafkaProbe) baseTags() []tags.Tag {
	id := p.entitySrc.instanceID()
	if id == "" {
		return nil
	}
	return []tags.Tag{{Key: "service.instance.id", Value: id}}
}

// dp constructs a datapoint with the probe's base tags.
func (p *kafkaProbe) dp(name string, value float64, ts time.Time, extraTags ...tags.Tag) data_store.DataPoint {
	base := p.baseTags()
	all := make([]tags.Tag, 0, len(base)+len(extraTags))
	all = append(all, base...)
	all = append(all, extraTags...)
	return data_store.DataPoint{Name: name, Value: value, Timestamp: ts, Tags: all}
}

// checkResultPoint emits the required check-result status metric.
func (p *kafkaProbe) checkResultPoint(result dbcommon.CheckResult, now time.Time) data_store.DataPoint {
	return p.dp(dbcommon.CheckResultMetric, float64(result), now, tags.Tag{Key: "metric_type", Value: "status"})
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

	// Entity rail: use the first configured broker as the primary address.
	primaryBroker := "localhost:9092"
	if len(cfg.Brokers) > 0 {
		primaryBroker = cfg.Brokers[0]
	}

	// Tech-id fetcher: calls the controller broker for metadata which carries
	// the cluster id. This closure is called at most once (pinned on success).
	brokers := cfg.Brokers
	protocolVersion := cfg.ProtocolVersion
	fetcher := func() (string, error) {
		sarCfg := sarama.NewConfig()
		ver, err := sarama.ParseKafkaVersion(protocolVersion)
		if err != nil {
			return "", err
		}
		sarCfg.Version = ver
		client, err := sarama.NewClient(brokers, sarCfg)
		if err != nil {
			return "", err
		}
		defer client.Close() //nolint:errcheck
		controller, err := client.Controller()
		if err != nil {
			return "", err
		}
		resp, err := controller.GetMetadata(&sarama.MetadataRequest{})
		if err != nil {
			return "", err
		}
		if resp.ClusterID != nil {
			return *resp.ClusterID, nil
		}
		return "", nil
	}

	p.entitySrc = newKafkaEntitySource(primaryBroker, cfg.InstanceName, fetcher, nil)

	p.SetEntitySource(p.entitySrc)
	return p, nil
}

func (p *kafkaProbe) ShouldStart() bool { return true }

func (p *kafkaProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *kafkaProbe) OnStart(_ chan struct{}) error {
	return nil
}

func (p *kafkaProbe) OnShutdown(_ context.Context) error {
	return nil
}

// Collect collects without a cancellation signal; the poller calls
// CollectContext.
func (p *kafkaProbe) Collect() ([]data_store.DataPoint, error) {
	return p.CollectContext(context.Background())
}

// CollectContext runs one collection cycle against the Kafka cluster.
// On connection failure it emits senhub.kafka.up=0 and returns nil.
//
// sarama takes no context, so the cycle runs aside: when ctx ends the
// connections are closed, which fails every call still in flight, and the
// cycle returns the context error without waiting for them. A dial that is
// already underway cannot be interrupted and ends at sarama's own dial
// timeout, its result discarded.
func (p *kafkaProbe) CollectContext(ctx context.Context) ([]data_store.DataPoint, error) {
	type outcome struct {
		points []data_store.DataPoint
		err    error
	}
	session := &closerSet{}
	done := make(chan outcome, 1)
	go func() {
		points, err := p.collectCycle(ctx, session)
		done <- outcome{points, err}
	}()

	select {
	case res := <-done:
		return res.points, res.err
	case <-ctx.Done():
		p.closeSession(session)
		return nil, fmt.Errorf("kafka collection interrupted: %w", ctx.Err())
	}
}

// closerSet is the connections of one cycle. Closing it twice, or adding to
// it once closed, is safe: the cycle and its canceller race to close.
type closerSet struct {
	mu     sync.Mutex
	closed bool
	items  []namedCloser
}

type namedCloser struct {
	name string
	io.Closer
}

// add registers c. When the set is already closed it closes c at once
// instead and reports added=false with the outcome of that close.
func (s *closerSet) add(name string, c io.Closer) (added bool, closeErr error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false, c.Close()
	}
	s.items = append(s.items, namedCloser{name: name, Closer: c})
	s.mu.Unlock()
	return true, nil
}

func (s *closerSet) take() []namedCloser {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	items := s.items
	s.items = nil
	return items
}

// track registers a connection with the cycle's session. It reports false,
// with the connection closed, when the session was already closed because
// the cycle was cancelled.
func (p *kafkaProbe) track(s *closerSet, name string, c io.Closer) bool {
	added, closeErr := s.add(name, c)
	if closeErr != nil {
		p.moduleLogger.Warn().Err(closeErr).Str("connection", name).Msg("kafka: error closing connection")
	}
	return added
}

func (p *kafkaProbe) closeSession(s *closerSet) {
	for _, c := range s.take() {
		if err := c.Close(); err != nil {
			p.moduleLogger.Warn().Err(err).Str("connection", c.name).Msg("kafka: error closing connection")
		}
	}
}

func (p *kafkaProbe) collectCycle(ctx context.Context, session *closerSet) ([]data_store.DataPoint, error) {
	now := time.Now()
	interrupted := func() error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("kafka collection interrupted: %w", err)
		}
		return nil
	}

	sarCfg, err := p.buildSaramaConfig()
	if err != nil {
		p.moduleLogger.Error().Err(err).Msg("kafka: failed to build sarama config")
		p.entitySrc.notifyFailure()
		return p.failurePoints(0, now, err), nil
	}

	admin, err := p.newAdmin(p.cfg.Brokers, sarCfg)
	if err != nil {
		if ierr := interrupted(); ierr != nil {
			return nil, ierr
		}
		p.moduleLogger.Warn().Err(err).Strs("brokers", p.cfg.Brokers).Msg("kafka: cannot connect (admin)")
		p.entitySrc.notifyFailure()
		return p.failurePoints(0, now, err), nil
	}
	defer p.closeSession(session)
	if !p.track(session, "admin", admin) {
		return nil, fmt.Errorf("kafka collection interrupted: %w", context.Cause(ctx))
	}

	client, err := p.newClient(p.cfg.Brokers, sarCfg)
	if err != nil {
		if ierr := interrupted(); ierr != nil {
			return nil, ierr
		}
		p.moduleLogger.Warn().Err(err).Strs("brokers", p.cfg.Brokers).Msg("kafka: cannot connect (client)")
		p.entitySrc.notifyFailure()
		return p.failurePoints(0, now, err), nil
	}
	if !p.track(session, "client", client) {
		return nil, fmt.Errorf("kafka collection interrupted: %w", context.Cause(ctx))
	}

	if ierr := interrupted(); ierr != nil {
		return nil, ierr
	}
	// Broker is reachable; notify the entity source so it can pin the tech id.
	p.entitySrc.notifySuccess("")

	var points []data_store.DataPoint

	// senhub.kafka.up
	points = append(points, p.dp("senhub.kafka.up", 1, now,
		tags.Tag{Key: "metric_type", Value: "broker"},
	))

	// kafka.brokers
	brokers := client.Brokers()
	points = append(points, p.dp("kafka.brokers", float64(len(brokers)), now,
		tags.Tag{Key: "metric_type", Value: "broker"},
	))

	// per-collection check result: 0 (ok)
	points = append(points, p.checkResultPoint(dbcommon.CheckOK, now))

	// Topic + partition metrics
	topicMeta, err := admin.ListTopics()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("kafka: ListTopics failed")
	} else {
		topicPoints, topicPartitions := p.collectTopicMetrics(client, topicMeta, now)
		points = append(points, topicPoints...)

		// Consumer group metrics (requires the partition map)
		groupPoints := p.collectGroupMetricsWithNames(admin, client, topicPartitions, now)
		points = append(points, groupPoints...)
	}

	if ierr := interrupted(); ierr != nil {
		return nil, ierr
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
		points = append(points, p.dp("kafka.topic.partitions", float64(numPartitions), now,
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
				points = append(points, p.dp("kafka.partition.current_offset", float64(newestOffset), now,
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
				points = append(points, p.dp("kafka.partition.oldest_offset", float64(oldestOffset), now,
					tags.Tag{Key: "metric_type", Value: "partition"},
					tags.Tag{Key: "topic", Value: topic},
					tags.Tag{Key: "partition", Value: partStr},
				))
			}

			// Replicas from topic detail
			replicas := detail.ReplicaAssignment[partition]
			points = append(points,
				p.dp("kafka.partition.replicas", float64(len(replicas)), now,
					tags.Tag{Key: "metric_type", Value: "partition"},
					tags.Tag{Key: "topic", Value: topic},
					tags.Tag{Key: "partition", Value: partStr},
				),
			)

			// In-sync replicas (ISR) — under-replicated partitions have ISR < replicas.
			isr, err := client.InSyncReplicas(topic, partition)
			if err != nil {
				p.moduleLogger.Warn().Err(err).
					Str("topic", topic).Int32("partition", partition).
					Msg("kafka: InSyncReplicas failed")
			} else {
				points = append(points,
					p.dp("kafka.partition.replicas_in_sync", float64(len(isr)), now,
						tags.Tag{Key: "metric_type", Value: "partition"},
						tags.Tag{Key: "topic", Value: topic},
						tags.Tag{Key: "partition", Value: partStr},
					),
				)
			}
		}
	}

	return points, topicPartitions
}

// collectGroupMetricsWithNames returns per-group, per-group/topic, and
// per-group/topic/partition datapoints.
func (p *kafkaProbe) collectGroupMetricsWithNames(
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
			points = append(points, p.dp("kafka.consumer_group.members", float64(members), now,
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
		lagSum := make(map[string]float64)

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
				groupOffset := float64(block.Offset)

				points = append(points, p.dp("kafka.consumer_group.offset", groupOffset, now,
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

				lag := float64(newestOffset) - groupOffset
				if lag < 0 {
					lag = 0
				}

				points = append(points, p.dp("kafka.consumer_group.lag", lag, now,
					tags.Tag{Key: "metric_type", Value: "consumer_group"},
					tags.Tag{Key: "group", Value: group},
					tags.Tag{Key: "topic", Value: topic},
					tags.Tag{Key: "partition", Value: partStr},
				))

				lagSum[topic] += lag
			}

			// Emit per-group/topic lag_sum
			points = append(points, p.dp("kafka.consumer_group.lag_sum", lagSum[topic], now,
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
func (p *kafkaProbe) upPoint(val float64, now time.Time) []data_store.DataPoint {
	return []data_store.DataPoint{
		p.dp("senhub.kafka.up", val, now, tags.Tag{Key: "metric_type", Value: "broker"}),
	}
}

func (p *kafkaProbe) failurePoints(val float64, now time.Time, err error) []data_store.DataPoint {
	pts := append(p.upPoint(val, now), p.checkResultPoint(dbcommon.ClassifyError(err), now))
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
