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
