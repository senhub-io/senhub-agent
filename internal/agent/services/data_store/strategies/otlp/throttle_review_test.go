package otlp

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// An export that began before the hint was noted must not lift the wait,
// or live exports resume and overtake the backlog.
func TestMarkSuccess_ExportStartedBeforeTheHintDoesNotLiftIt(t *testing.T) {
	var buf lockedBuffer
	h, clock := newTestHealth(t, &buf)
	started := *clock
	*clock = clock.Add(time.Second)
	h.noteThrottle(&throttledError{err: context.DeadlineExceeded, wait: time.Minute})
	h.markFailure()

	h.markSuccessSince(started)
	if h.throttleRemaining() <= 0 {
		t.Fatal("a success that began before the hint lifted the wait")
	}
	if h.healthy.Load() {
		t.Error("a stale success marked the backend healthy during the wait")
	}
	h.markSuccessSince(*clock)
	if h.throttleRemaining() != 0 || !h.healthy.Load() {
		t.Error("a success that began after the hint must end the throttle")
	}
}

// A RetryInfo on a status that is not an overload is not a wait.
func TestRetryInfo_IgnoredOnCodesThatAreNotOverload(t *testing.T) {
	mk := func(c codes.Code) error {
		st, err := status.New(c, "x").WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		return st.Err()
	}
	for _, c := range []codes.Code{codes.ResourceExhausted, codes.Unavailable} {
		if _, ok := retryInfoDelay(mk(c)); !ok {
			t.Errorf("%v with RetryInfo gave no hint", c)
		}
	}
	if _, ok := retryInfoDelay(mk(codes.InvalidArgument)); ok {
		t.Error("InvalidArgument with RetryInfo gave a hint")
	}
	sink := &hintSink{}
	sink.observeError(mk(codes.InvalidArgument))
	if sink.remaining(time.Now()) != 0 {
		t.Error("the sink kept a hint from a non-overload code")
	}
}

// A throttled request that cannot be written is counted as lost.
func TestThrottle_UnwritableQueueCountsTheLoss(t *testing.T) {
	col := newFakeCollector(t, "grpc")
	rig := newWireRig(t, col, t.TempDir(), "metrics", 0)
	col.throttleFor(time.Now().Add(time.Minute), "", time.Second)
	_ = os.RemoveAll(rig.q.dir)
	if err := os.WriteFile(rig.q.dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := droppedBy("queue_write_failed")
	if err := rig.pushMetric(1, 1); !errors.Is(err, errWireBackendDown) {
		t.Fatalf("push = %v, want errWireBackendDown", err)
	}
	if got := droppedBy("queue_write_failed") - before; got != 1 {
		t.Errorf("queue_write_failed moved by %d, want 1", got)
	}
}

// The relayed batch that cannot be kept is counted as lost, too.
func TestLogsRelay_UnwritableQueueCountsTheLoss(t *testing.T) {
	col := newFakeCollector(t, "grpc")
	rig := newLogsRelayRig(t, col, t.TempDir())
	col.setDown(true)
	_ = os.RemoveAll(rig.q.dir)
	if err := os.WriteFile(rig.q.dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := agentstate.GetOTLPDroppedByReason()["queue_write_failed"]
	rig.relay.export(relayLogBatch("app", "lost"), 1)
	if got := agentstate.GetOTLPDroppedByReason()["queue_write_failed"] - before; got != 1 {
		t.Errorf("queue_write_failed moved by %d, want 1", got)
	}
}
