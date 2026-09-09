package agentstate

import "testing"

func TestExportActivityTransitionsBecomeEvents(t *testing.T) {
	ResetExportActivityForTest()
	ResetEventsForTest()
	defer ResetExportActivityForTest()
	defer ResetEventsForTest()

	RecordExportSuccess("otlp")
	RecordExportSuccess("otlp")
	if n := len(GetEvents()); n != 0 {
		t.Fatalf("plain successes are not events, got %d", n)
	}
	RecordExportFailure("otlp", "transport")
	RecordExportFailure("otlp", "transport")
	if ev := GetEvents(); len(ev) != 1 || ev[0].Level != EventError {
		t.Fatalf("the first failure is one error event, got %+v", ev)
	}
	RecordExportSuccess("otlp")
	if ev := GetEvents(); len(ev) != 2 || ev[0].Message != "export recovered" {
		t.Fatalf("the first success after a failure is a recovery event, got %+v", ev)
	}
	a := GetExportActivity("otlp")
	if a.Successes != 3 || a.Failures != 2 || a.LastError != "transport" || a.LastSuccess.Before(a.LastFailure) {
		t.Errorf("snapshot wrong: %+v", a)
	}
	if got := GetExportActivity("never"); !got.LastSuccess.IsZero() {
		t.Error("an unknown strategy has a zero snapshot")
	}
}

func TestExportFailureReasonIsPrintable(t *testing.T) {
	ResetExportActivityForTest()
	defer ResetExportActivityForTest()
	RecordExportFailure("otlp", "401 Unauthorized (body: \x08\x10\x12>provided authorization\ndoes not match)")
	if got := GetExportActivity("otlp").LastError; got != "401 Unauthorized (body: >provided authorization does not match)" {
		t.Errorf("control characters must be dropped, got %q", got)
	}
}

func TestPruneExportActivityForgetsDeletedOutputs(t *testing.T) {
	ResetExportActivityForTest()
	defer ResetExportActivityForTest()

	RecordExportFailure("otlp/logs", "collector refused the batch")
	RecordExportSuccess("prtg")
	if got := FailingExports(); len(got) != 1 || got[0] != "otlp" {
		t.Fatalf("the failing signal must make its strategy failing, got %v", got)
	}
	// The operator deletes the otlp output; the header must stop saying
	// an output is failing without waiting for a restart.
	PruneExportActivity([]string{"prtg"})
	if got := FailingExports(); len(got) != 0 {
		t.Errorf("a deleted output must not stay failing, got %v", got)
	}
	if act := GetExportActivity("prtg"); act.LastSuccess.IsZero() {
		t.Error("a configured output keeps its activity")
	}
}
