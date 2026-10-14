package zookeeper

import (
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

func TestCollect_IdentityKeyAndCheckResult_Success(t *testing.T) {
	p, err := NewZookeeperProbe(map[string]interface{}{"instance_name": "zk-node-1"}, testLogger())
	if err != nil {
		t.Fatalf("NewZookeeperProbe: %v", err)
	}
	zp := p.(*ZookeeperProbe)
	zp.dial = stubDial(mntrLeader)

	pts, err := zp.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(pts) == 0 {
		t.Fatal("no datapoints returned")
	}

	var check *data_store.DataPoint
	for i := range pts {
		pt := &pts[i]
		if pt.Name == dbcommon.CheckResultMetric {
			check = pt
		}

		foundID := false
		for _, tag := range pt.Tags {
			if tag.Key == "service.instance.id" {
				foundID = true
				if tag.Value != "zk-node-1" {
					t.Errorf("point %s service.instance.id: want zk-node-1 got %q", pt.Name, tag.Value)
				}
			}
		}
		if !foundID {
			t.Errorf("point %s missing service.instance.id tag", pt.Name)
		}
	}

	if check == nil {
		t.Fatalf("check result point %q not found", dbcommon.CheckResultMetric)
	}
	if check.Value != float64(dbcommon.CheckOK) {
		t.Errorf("check result: want %v got %v", float64(dbcommon.CheckOK), check.Value)
	}
	foundStatus := false
	for _, tag := range check.Tags {
		if tag.Key == "metric_type" && tag.Value == "status" {
			foundStatus = true
		}
	}
	if !foundStatus {
		t.Error("check result missing metric_type=status")
	}
}

func TestCollect_IdentityKeyFallbackOnFailedConnection(t *testing.T) {
	old := getHostIdentity
	getHostIdentity = func() string { return "test-host-id" }
	defer func() { getHostIdentity = old }()

	p, err := NewZookeeperProbe(map[string]interface{}{}, testLogger())
	if err != nil {
		t.Fatalf("NewZookeeperProbe: %v", err)
	}
	zp := p.(*ZookeeperProbe)
	zp.dial = stubDial(mntrLeader)
	if _, err := zp.Collect(); err != nil {
		t.Fatalf("initial Collect: %v", err)
	}

	zp.dial = errDial
	pts, err := zp.Collect()
	if err != nil {
		t.Fatalf("Collect after failure: %v", err)
	}

	var upPoint *data_store.DataPoint
	var checkPoint *data_store.DataPoint
	for i := range pts {
		pt := &pts[i]
		if pt.Name == "senhub.zookeeper.up" {
			upPoint = pt
		}
		if pt.Name == dbcommon.CheckResultMetric {
			checkPoint = pt
		}

		foundID := false
		for _, tag := range pt.Tags {
			if tag.Key == "service.instance.id" {
				foundID = true
				if tag.Value != "zookeeper@test-host-id" {
					t.Errorf("point %s service.instance.id: want zookeeper@test-host-id got %q", pt.Name, tag.Value)
				}
			}
		}
		if !foundID {
			t.Errorf("point %s missing service.instance.id tag", pt.Name)
		}
	}

	if upPoint == nil {
		t.Fatal("up point not found")
	}
	if upPoint.Value != 0 {
		t.Errorf("up: want 0 got %v", upPoint.Value)
	}

	if checkPoint == nil {
		t.Fatalf("check result point %q not found", dbcommon.CheckResultMetric)
	}
	wantClass := float64(dbcommon.CheckTargetDown)
	if checkPoint.Value != wantClass {
		t.Errorf("check result: want %v got %v", wantClass, checkPoint.Value)
	}
	foundStatus := false
	for _, tag := range checkPoint.Tags {
		if tag.Key == "metric_type" && tag.Value == "status" {
			foundStatus = true
		}
	}
	if !foundStatus {
		t.Error("check result missing metric_type=status")
	}
}
