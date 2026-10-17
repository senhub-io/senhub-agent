package kafka

import (
	"net"
	"syscall"
	"testing"

	"github.com/IBM/sarama"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

type keysTestAdmin struct {
	sarama.ClusterAdmin
	listTopics map[string]sarama.TopicDetail
	groups     map[string]string
}

func (f *keysTestAdmin) Close() error { return nil }
func (f *keysTestAdmin) ListTopics() (map[string]sarama.TopicDetail, error) {
	return f.listTopics, nil
}
func (f *keysTestAdmin) ListConsumerGroups() (map[string]string, error) {
	return f.groups, nil
}

type keysTestClient struct {
	sarama.Client
}

func (f *keysTestClient) Close() error              { return nil }
func (f *keysTestClient) Brokers() []*sarama.Broker { return nil }

func keysTestTagValue(point data_store.DataPoint, key string) (string, bool) {
	for _, tag := range point.Tags {
		if tag.Key == key {
			return tag.Value, true
		}
	}
	return "", false
}

func keysTestFindPoint(points []data_store.DataPoint, name string) *data_store.DataPoint {
	for i := range points {
		if points[i].Name == name {
			return &points[i]
		}
	}
	return nil
}

func TestKafkaDatapointsCarryIdentityAndCheckResultOnSuccess(t *testing.T) {
	probe, err := NewKafkaProbe(map[string]interface{}{
		"brokers":       []interface{}{"127.0.0.1:9092"},
		"instance_name": "test-kafka-cluster",
	}, makeLogger(t))
	if err != nil {
		t.Fatalf("NewKafkaProbe: %v", err)
	}
	p, ok := probe.(*kafkaProbe)
	if !ok {
		t.Fatalf("NewKafkaProbe returned %T, want *kafkaProbe", probe)
	}

	p.newAdmin = func(brokers []string, cfg *sarama.Config) (sarama.ClusterAdmin, error) {
		return &keysTestAdmin{listTopics: map[string]sarama.TopicDetail{}, groups: map[string]string{}}, nil
	}
	p.newClient = func(brokers []string, cfg *sarama.Config) (sarama.Client, error) {
		return &keysTestClient{}, nil
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect emitted no datapoints")
	}

	for _, point := range points {
		if got, ok := keysTestTagValue(point, "service.instance.id"); !ok || got != "test-kafka-cluster" {
			t.Errorf("point %s service.instance.id = %q ok=%v, want test-kafka-cluster on every point", point.Name, got, ok)
		}
	}

	check := keysTestFindPoint(points, dbcommon.CheckResultMetric)
	if check == nil {
		t.Fatalf("check result point %q not emitted", dbcommon.CheckResultMetric)
	}
	if check.Value != 0 {
		t.Errorf("check result value = %v, want 0", check.Value)
	}
	if got, ok := keysTestTagValue(*check, "metric_type"); !ok || got != "status" {
		t.Errorf("check result metric_type = %q ok=%v, want status", got, ok)
	}
}

func TestKafkaFailedConnectionKeepsUpAndAddsClassifiedCheckResult(t *testing.T) {
	probe, err := NewKafkaProbe(map[string]interface{}{
		"brokers": []interface{}{"127.0.0.1:9092"},
	}, makeLogger(t))
	if err != nil {
		t.Fatalf("NewKafkaProbe: %v", err)
	}
	p, ok := probe.(*kafkaProbe)
	if !ok {
		t.Fatalf("NewKafkaProbe returned %T, want *kafkaProbe", probe)
	}

	p.entitySrc = newKafkaEntitySource("127.0.0.1:9092", "", nil, func() string { return "host-42" })
	p.SetEntitySource(p.entitySrc)

	targetErr := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	p.newAdmin = func(brokers []string, cfg *sarama.Config) (sarama.ClusterAdmin, error) {
		return nil, targetErr
	}
	p.newClient = func(brokers []string, cfg *sarama.Config) (sarama.Client, error) {
		return &keysTestClient{}, nil
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	up := keysTestFindPoint(points, "senhub.kafka.up")
	if up == nil {
		t.Fatalf("senhub.kafka.up not emitted")
	}
	if up.Value != 0 {
		t.Errorf("senhub.kafka.up value = %v, want 0", up.Value)
	}

	check := keysTestFindPoint(points, dbcommon.CheckResultMetric)
	if check == nil {
		t.Fatalf("check result point %q not emitted", dbcommon.CheckResultMetric)
	}
	wantClass := float64(dbcommon.ClassifyError(targetErr))
	if check.Value != wantClass {
		t.Errorf("check result value = %v, want %v", check.Value, wantClass)
	}
	if got, ok := keysTestTagValue(*check, "metric_type"); !ok || got != "status" {
		t.Errorf("check result metric_type = %q ok=%v, want status", got, ok)
	}

	for _, point := range points {
		if got, ok := keysTestTagValue(point, "service.instance.id"); !ok {
			t.Errorf("point %s missing service.instance.id", point.Name)
		} else if got != "kafka@host-42" {
			t.Errorf("point %s service.instance.id = %q, want kafka@host-42", point.Name, got)
		}
	}
}
