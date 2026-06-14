package kafka

import (
	"testing"
	"time"

	"github.com/IBM/sarama"

	"senhub-agent.go/internal/agent/cliArgs"
	probeTypes "senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// ---- fakes ---------------------------------------------------------------

type fakeAdmin struct {
	topics map[string]sarama.TopicDetail
	groups map[string]string
	descs  []*sarama.GroupDescription
	// groupOffsets[group][topic][partition] = offset
	groupOffsets map[string]map[string]map[int32]int64

	listTopicsErr  error
	listGroupsErr  error
	descGroupsErr  error
	groupOffsetErr error
}

func (f *fakeAdmin) ListTopics() (map[string]sarama.TopicDetail, error) {
	return f.topics, f.listTopicsErr
}

func (f *fakeAdmin) ListConsumerGroups() (map[string]string, error) {
	return f.groups, f.listGroupsErr
}

func (f *fakeAdmin) DescribeConsumerGroups(groups []string) ([]*sarama.GroupDescription, error) {
	if f.descGroupsErr != nil {
		return nil, f.descGroupsErr
	}
	if f.descs != nil {
		return f.descs, nil
	}
	out := make([]*sarama.GroupDescription, 0, len(groups))
	for _, g := range groups {
		out = append(out, &sarama.GroupDescription{GroupId: g, Members: map[string]*sarama.GroupMemberDescription{}})
	}
	return out, nil
}

func (f *fakeAdmin) ListConsumerGroupOffsets(group string, topicPartitions map[string][]int32) (*sarama.OffsetFetchResponse, error) {
	if f.groupOffsetErr != nil {
		return nil, f.groupOffsetErr
	}
	resp := &sarama.OffsetFetchResponse{
		Blocks: make(map[string]map[int32]*sarama.OffsetFetchResponseBlock),
	}
	gOffsets := f.groupOffsets[group]
	for topic, partitions := range topicPartitions {
		resp.Blocks[topic] = make(map[int32]*sarama.OffsetFetchResponseBlock)
		for _, part := range partitions {
			offset := int64(0)
			if topicMap, ok := gOffsets[topic]; ok {
				offset = topicMap[part]
			}
			resp.Blocks[topic][part] = &sarama.OffsetFetchResponseBlock{
				Offset: offset,
				Err:    sarama.ErrNoError,
			}
		}
	}
	return resp, nil
}

func (f *fakeAdmin) Close() error { return nil }

// Unused ClusterAdmin methods — stubs to satisfy the interface.
func (f *fakeAdmin) CreateTopic(_ string, _ *sarama.TopicDetail, _ bool) error { return nil }
func (f *fakeAdmin) DescribeTopics(_ []string) ([]*sarama.TopicMetadata, error) {
	return nil, nil
}
func (f *fakeAdmin) DeleteTopic(_ string) error { return nil }
func (f *fakeAdmin) CreatePartitions(_ string, _ int32, _ [][]int32, _ bool) error {
	return nil
}
func (f *fakeAdmin) AlterPartitionReassignments(_ string, _ [][]int32) error { return nil }
func (f *fakeAdmin) ListPartitionReassignments(_ string, _ []int32) (map[string]map[int32]*sarama.PartitionReplicaReassignmentsStatus, error) {
	return nil, nil
}
func (f *fakeAdmin) DeleteRecords(_ string, _ map[int32]int64) error { return nil }
func (f *fakeAdmin) DescribeConfig(_ sarama.ConfigResource) ([]sarama.ConfigEntry, error) {
	return nil, nil
}
func (f *fakeAdmin) DescribeConfigs(_ []*sarama.ConfigResource, _ sarama.DescribeConfigsOptions) ([]*sarama.ConfigResourceResult, error) {
	return nil, nil
}
func (f *fakeAdmin) AlterConfig(_ sarama.ConfigResourceType, _ string, _ map[string]*string, _ bool) error {
	return nil
}
func (f *fakeAdmin) IncrementalAlterConfig(_ sarama.ConfigResourceType, _ string, _ map[string]sarama.IncrementalAlterConfigsEntry, _ bool) error {
	return nil
}
func (f *fakeAdmin) CreateACL(_ sarama.Resource, _ sarama.Acl) error { return nil }
func (f *fakeAdmin) CreateACLs(_ []*sarama.ResourceAcls) error       { return nil }
func (f *fakeAdmin) ListAcls(_ sarama.AclFilter) ([]sarama.ResourceAcls, error) {
	return nil, nil
}
func (f *fakeAdmin) DeleteACL(_ sarama.AclFilter, _ bool) ([]sarama.MatchingAcl, error) {
	return nil, nil
}
func (f *fakeAdmin) DeleteGroups(_ []string) (map[string]error, error)   { return nil, nil }
func (f *fakeAdmin) DescribeLogDirs(_ []int32) (map[int32][]sarama.DescribeLogDirsResponseDirMetadata, error) {
	return nil, nil
}
func (f *fakeAdmin) DescribeUserScramCredentials(_ []string) ([]*sarama.DescribeUserScramCredentialsResult, error) {
	return nil, nil
}
func (f *fakeAdmin) DeleteUserScramCredentials(_ []sarama.AlterUserScramCredentialsDelete) ([]*sarama.AlterUserScramCredentialsResult, error) {
	return nil, nil
}
func (f *fakeAdmin) UpsertUserScramCredentials(_ []sarama.AlterUserScramCredentialsUpsert) ([]*sarama.AlterUserScramCredentialsResult, error) {
	return nil, nil
}
func (f *fakeAdmin) DescribeClientQuotas(_ []sarama.QuotaFilterComponent, _ bool) ([]sarama.DescribeClientQuotasEntry, error) {
	return nil, nil
}
func (f *fakeAdmin) AlterClientQuotas(_ []sarama.QuotaEntityComponent, _ sarama.ClientQuotasOp, _ bool) error {
	return nil
}
func (f *fakeAdmin) Controller() (*sarama.Broker, error) { return nil, nil }
func (f *fakeAdmin) RemoveMemberFromConsumerGroup(_ string, _ []string) (*sarama.LeaveGroupResponse, error) {
	return nil, nil
}
func (f *fakeAdmin) ListConsumerGroupOffsetsBatch(_ map[string]map[string][]int32) (map[string]*sarama.OffsetFetchResponseGroup, error) {
	return nil, nil
}
func (f *fakeAdmin) ElectLeaders(_ sarama.ElectionType, _ map[string][]int32) (map[string]map[int32]*sarama.PartitionResult, error) {
	return nil, nil
}
func (f *fakeAdmin) ListOffsets(_ map[string]map[int32]int64, _ *sarama.ListOffsetsOptions) (map[string]map[int32]*sarama.OffsetResult, error) {
	return nil, nil
}
func (f *fakeAdmin) AlterConsumerGroupOffsets(_ string, _ map[string]map[int32]sarama.OffsetAndMetadata, _ *sarama.AlterConsumerGroupOffsetsOptions) (*sarama.OffsetCommitResponse, error) {
	return nil, nil
}
func (f *fakeAdmin) DeleteConsumerGroupOffset(_ string, _ string, _ int32) error { return nil }
func (f *fakeAdmin) DeleteConsumerGroup(_ string) error                           { return nil }
func (f *fakeAdmin) DescribeCluster() ([]*sarama.Broker, int32, error)           { return nil, 0, nil }
func (f *fakeAdmin) Coordinator(_ string) (*sarama.Broker, error)                { return nil, nil }

// fakeClient satisfies sarama.Client for the subset used by the probe.
type fakeClient struct {
	brokers []string
	// offsets[topic][partition][time] = value
	offsets map[string]map[int32]map[int64]int64

	getOffsetErr error
}

func (f *fakeClient) Brokers() []*sarama.Broker {
	out := make([]*sarama.Broker, len(f.brokers))
	for i, addr := range f.brokers {
		b := sarama.NewBroker(addr)
		out[i] = b
	}
	return out
}

func (f *fakeClient) GetOffset(topic string, partitionID int32, time int64) (int64, error) {
	if f.getOffsetErr != nil {
		return 0, f.getOffsetErr
	}
	if partMap, ok := f.offsets[topic]; ok {
		if timeMap, ok := partMap[partitionID]; ok {
			if v, ok := timeMap[time]; ok {
				return v, nil
			}
		}
	}
	return 0, nil
}

func (f *fakeClient) Close() error  { return nil }
func (f *fakeClient) Closed() bool  { return false }

// Stubs for unused Client methods.
func (f *fakeClient) Config() *sarama.Config                                      { return sarama.NewConfig() }
func (f *fakeClient) Controller() (*sarama.Broker, error)                         { return nil, nil }
func (f *fakeClient) RefreshController() (*sarama.Broker, error)                  { return nil, nil }
func (f *fakeClient) Broker(_ int32) (*sarama.Broker, error)                      { return nil, nil }
func (f *fakeClient) Topics() ([]string, error)                                    { return nil, nil }
func (f *fakeClient) Partitions(_ string) ([]int32, error)                        { return nil, nil }
func (f *fakeClient) WritablePartitions(_ string) ([]int32, error)                { return nil, nil }
func (f *fakeClient) Leader(_ string, _ int32) (*sarama.Broker, error)            { return nil, nil }
func (f *fakeClient) LeaderAndEpoch(_ string, _ int32) (*sarama.Broker, int32, error) {
	return nil, 0, nil
}
func (f *fakeClient) Replicas(_ string, _ int32) ([]int32, error)        { return nil, nil }
func (f *fakeClient) InSyncReplicas(_ string, _ int32) ([]int32, error)  { return nil, nil }
func (f *fakeClient) OfflineReplicas(_ string, _ int32) ([]int32, error) { return nil, nil }
func (f *fakeClient) RefreshBrokers(_ []string) error                     { return nil }
func (f *fakeClient) RefreshMetadata(_ ...string) error                   { return nil }
func (f *fakeClient) Coordinator(_ string) (*sarama.Broker, error)       { return nil, nil }
func (f *fakeClient) RefreshCoordinator(_ string) error                  { return nil }
func (f *fakeClient) TransactionCoordinator(_ string) (*sarama.Broker, error) { return nil, nil }
func (f *fakeClient) RefreshTransactionCoordinator(_ string) error       { return nil }
func (f *fakeClient) InitProducerID() (*sarama.InitProducerIDResponse, error) {
	return nil, nil
}
func (f *fakeClient) LeastLoadedBroker() *sarama.Broker      { return nil }
func (f *fakeClient) PartitionNotReadable(_ string, _ int32) bool { return false }

// ---- helpers -------------------------------------------------------------

func makeLogger(t *testing.T) *logger.Logger {
	t.Helper()
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

// pointsBy returns datapoints filtered by metric name.
func pointsBy(pts []data_store.DataPoint, name string) []data_store.DataPoint {
	var out []data_store.DataPoint
	for _, p := range pts {
		if p.Name == name {
			out = append(out, p)
		}
	}
	return out
}

func tagVal(dp datapoint.DataPoint, key string) string {
	for _, t := range dp.Tags {
		if t.Key == key {
			return t.Value
		}
	}
	return ""
}

// ---- tests ---------------------------------------------------------------

func TestCollect_Up(t *testing.T) {
	adm := &fakeAdmin{
		topics: map[string]sarama.TopicDetail{},
		groups: map[string]string{},
	}
	cli := &fakeClient{brokers: []string{"broker1:9092", "broker2:9092"}}

	p := newTestProbe(t, adm, cli, probeConfig{
		Brokers:         []string{"localhost:9092"},
		ProtocolVersion: "2.0.0",
		Interval:        60 * time.Second,
		Timeout:         10 * time.Second,
	})

	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() returned unexpected error: %v", err)
	}

	up := pointsBy(pts, "senhub.kafka.up")
	if len(up) != 1 {
		t.Fatalf("expected 1 senhub.kafka.up point, got %d", len(up))
	}
	if up[0].Value != 1 {
		t.Errorf("senhub.kafka.up = %v, want 1", up[0].Value)
	}

	brokerPts := pointsBy(pts, "kafka.brokers")
	if len(brokerPts) != 1 {
		t.Fatalf("expected 1 kafka.brokers point, got %d", len(brokerPts))
	}
	if brokerPts[0].Value != 2 {
		t.Errorf("kafka.brokers = %v, want 2", brokerPts[0].Value)
	}
}

func TestCollect_UpZeroOnAdminError(t *testing.T) {
	p := &kafkaProbe{
		BaseProbe:    newBase(t),
		cfg:          probeConfig{Brokers: []string{"dead:9092"}, ProtocolVersion: "2.0.0"},
		moduleLogger: logger.NewModuleLogger(makeLogger(t), "probe.kafka"),
		entitySrc:    newKafkaEntitySource("dead:9092"),
		newAdmin: func(_ []string, _ *sarama.Config) (sarama.ClusterAdmin, error) {
			kerr := sarama.ErrUnknown
			return nil, kerr
		},
		newClient: func(_ []string, _ *sarama.Config) (sarama.Client, error) {
			t.Fatal("newClient should not be called when admin fails")
			return nil, nil
		},
	}
	p.SetProbeType(ProbeType)
	p.SetName("test")

	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() returned unexpected error: %v", err)
	}

	up := pointsBy(pts, "senhub.kafka.up")
	if len(up) != 1 || up[0].Value != 0 {
		t.Errorf("expected senhub.kafka.up=0 on admin error, got %+v", up)
	}
}

func TestCollect_TopicAndPartitionMetrics(t *testing.T) {
	adm := &fakeAdmin{
		topics: map[string]sarama.TopicDetail{
			"my-topic": {
				NumPartitions:     2,
				ReplicationFactor: 2,
				ReplicaAssignment: map[int32][]int32{
					0: {1, 2},
					1: {2, 1},
				},
			},
			"__consumer_offsets": { // must be filtered out
				NumPartitions:     1,
				ReplicaAssignment: map[int32][]int32{0: {1}},
			},
		},
		groups: map[string]string{},
	}
	cli := &fakeClient{
		brokers: []string{"b1:9092"},
		offsets: map[string]map[int32]map[int64]int64{
			"my-topic": {
				0: {sarama.OffsetNewest: 100, sarama.OffsetOldest: 10},
				1: {sarama.OffsetNewest: 200, sarama.OffsetOldest: 20},
			},
		},
	}

	p := newTestProbe(t, adm, cli, probeConfig{
		Brokers:         []string{"localhost:9092"},
		ProtocolVersion: "2.0.0",
		Interval:        60 * time.Second,
		Timeout:         10 * time.Second,
	})

	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// topic.partitions for my-topic
	topicPts := pointsBy(pts, "kafka.topic.partitions")
	if len(topicPts) != 1 {
		t.Fatalf("expected 1 kafka.topic.partitions, got %d", len(topicPts))
	}
	if topicPts[0].Value != 2 {
		t.Errorf("kafka.topic.partitions = %v, want 2", topicPts[0].Value)
	}
	if tagVal(topicPts[0], "topic") != "my-topic" {
		t.Errorf("topic tag = %q, want my-topic", tagVal(topicPts[0], "topic"))
	}

	// Internal topic must be absent
	for _, pt := range pts {
		if tagVal(pt, "topic") == "__consumer_offsets" {
			t.Errorf("internal topic __consumer_offsets leaked into output: %+v", pt)
		}
	}

	// current_offset for both partitions
	newestPts := pointsBy(pts, "kafka.partition.current_offset")
	if len(newestPts) != 2 {
		t.Errorf("expected 2 kafka.partition.current_offset, got %d", len(newestPts))
	}

	// replicas
	replicaPts := pointsBy(pts, "kafka.partition.replicas")
	if len(replicaPts) != 2 {
		t.Errorf("expected 2 kafka.partition.replicas, got %d", len(replicaPts))
	}
	for _, rp := range replicaPts {
		if rp.Value != 2 {
			t.Errorf("kafka.partition.replicas = %v, want 2", rp.Value)
		}
	}
}

func TestCollect_ConsumerGroupLag(t *testing.T) {
	adm := &fakeAdmin{
		topics: map[string]sarama.TopicDetail{
			"orders": {
				NumPartitions:     1,
				ReplicaAssignment: map[int32][]int32{0: {1}},
			},
		},
		groups: map[string]string{"my-consumer": "consumer"},
		descs: []*sarama.GroupDescription{
			{
				GroupId: "my-consumer",
				Members: map[string]*sarama.GroupMemberDescription{
					"member-1": {},
					"member-2": {},
				},
			},
		},
		groupOffsets: map[string]map[string]map[int32]int64{
			"my-consumer": {
				"orders": {0: 80}, // committed at 80
			},
		},
	}
	cli := &fakeClient{
		brokers: []string{"b1:9092"},
		offsets: map[string]map[int32]map[int64]int64{
			"orders": {
				0: {sarama.OffsetNewest: 100, sarama.OffsetOldest: 0},
			},
		},
	}

	p := newTestProbe(t, adm, cli, probeConfig{
		Brokers:         []string{"localhost:9092"},
		ProtocolVersion: "2.0.0",
		Interval:        60 * time.Second,
		Timeout:         10 * time.Second,
	})

	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// members
	memberPts := pointsBy(pts, "kafka.consumer_group.members")
	if len(memberPts) != 1 {
		t.Fatalf("expected 1 kafka.consumer_group.members, got %d", len(memberPts))
	}
	if memberPts[0].Value != 2 {
		t.Errorf("kafka.consumer_group.members = %v, want 2", memberPts[0].Value)
	}

	// offset
	offsetPts := pointsBy(pts, "kafka.consumer_group.offset")
	if len(offsetPts) != 1 {
		t.Fatalf("expected 1 kafka.consumer_group.offset, got %d", len(offsetPts))
	}
	if offsetPts[0].Value != 80 {
		t.Errorf("kafka.consumer_group.offset = %v, want 80", offsetPts[0].Value)
	}

	// lag = 100 - 80 = 20
	lagPts := pointsBy(pts, "kafka.consumer_group.lag")
	if len(lagPts) != 1 {
		t.Fatalf("expected 1 kafka.consumer_group.lag, got %d", len(lagPts))
	}
	if lagPts[0].Value != 20 {
		t.Errorf("kafka.consumer_group.lag = %v, want 20", lagPts[0].Value)
	}

	// lag_sum = 20 (single partition)
	lagSumPts := pointsBy(pts, "kafka.consumer_group.lag_sum")
	if len(lagSumPts) != 1 {
		t.Fatalf("expected 1 kafka.consumer_group.lag_sum, got %d", len(lagSumPts))
	}
	if lagSumPts[0].Value != 20 {
		t.Errorf("kafka.consumer_group.lag_sum = %v, want 20", lagSumPts[0].Value)
	}
}

func TestCollect_TopicFilter(t *testing.T) {
	adm := &fakeAdmin{
		topics: map[string]sarama.TopicDetail{
			"prod-orders": {NumPartitions: 1, ReplicaAssignment: map[int32][]int32{0: {1}}},
			"dev-orders":  {NumPartitions: 1, ReplicaAssignment: map[int32][]int32{0: {1}}},
		},
		groups: map[string]string{},
	}
	cli := &fakeClient{brokers: []string{"b1:9092"}}

	p := newTestProbe(t, adm, cli, probeConfig{
		Brokers:         []string{"localhost:9092"},
		ProtocolVersion: "2.0.0",
		Interval:        60 * time.Second,
		Timeout:         10 * time.Second,
		TopicFilter:     []string{"prod-*"},
	})

	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, pt := range pts {
		if tagVal(pt, "topic") == "dev-orders" {
			t.Errorf("dev-orders should be filtered out, got: %+v", pt)
		}
	}
	topicPts := pointsBy(pts, "kafka.topic.partitions")
	if len(topicPts) != 1 {
		t.Errorf("expected 1 kafka.topic.partitions (prod only), got %d", len(topicPts))
	}
}

func TestCollect_LagFloorAtZero(t *testing.T) {
	// Scenario: group offset is ahead of newest (e.g. after retention shrink);
	// lag must not go negative.
	adm := &fakeAdmin{
		topics: map[string]sarama.TopicDetail{
			"t1": {NumPartitions: 1, ReplicaAssignment: map[int32][]int32{0: {1}}},
		},
		groups: map[string]string{"g1": "consumer"},
		groupOffsets: map[string]map[string]map[int32]int64{
			"g1": {"t1": {0: 500}}, // committed ahead of newest
		},
	}
	cli := &fakeClient{
		brokers: []string{"b1:9092"},
		offsets: map[string]map[int32]map[int64]int64{
			"t1": {0: {sarama.OffsetNewest: 100, sarama.OffsetOldest: 0}},
		},
	}

	p := newTestProbe(t, adm, cli, probeConfig{
		Brokers:         []string{"localhost:9092"},
		ProtocolVersion: "2.0.0",
		Interval:        60 * time.Second,
		Timeout:         10 * time.Second,
	})

	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, pt := range pointsBy(pts, "kafka.consumer_group.lag") {
		if pt.Value < 0 {
			t.Errorf("lag is negative: %v", pt.Value)
		}
	}
}

// ---- builder helpers -----------------------------------------------------

func newBase(t *testing.T) *probeTypes.BaseProbe {
	t.Helper()
	b := &probeTypes.BaseProbe{}
	b.SetName("test")
	return b
}

func newTestProbe(t *testing.T, adm *fakeAdmin, cli *fakeClient, cfg probeConfig) *kafkaProbe {
	t.Helper()
	primaryBroker := "localhost:9092"
	if len(cfg.Brokers) > 0 {
		primaryBroker = cfg.Brokers[0]
	}
	p := &kafkaProbe{
		BaseProbe:    newBase(t),
		cfg:          cfg,
		moduleLogger: logger.NewModuleLogger(makeLogger(t), "probe.kafka"),
		entitySrc:    newKafkaEntitySource(primaryBroker),
		newAdmin: func(_ []string, _ *sarama.Config) (sarama.ClusterAdmin, error) {
			return adm, nil
		},
		newClient: func(_ []string, _ *sarama.Config) (sarama.Client, error) {
			return cli, nil
		},
	}
	p.SetProbeType(ProbeType)
	return p
}
