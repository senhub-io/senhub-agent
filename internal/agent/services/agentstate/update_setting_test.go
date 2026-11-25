package agentstate

import "testing"

func TestUpdateSetting(t *testing.T) {
	ResetUpdateSettingForTest()
	t.Cleanup(ResetUpdateSettingForTest)

	if _, ok := GetUpdateSetting(); ok {
		t.Fatal("GetUpdateSetting reported ok before anything was published")
	}

	want := UpdateSetting{
		Enabled:     true,
		IncludeBeta: true,
		Target:      "0.7.2",
		Source:      UpdateSourceServer,
		Paused:      true,
	}
	SetUpdateSetting(want)
	got, ok := GetUpdateSetting()
	if !ok {
		t.Fatal("GetUpdateSetting reported ok after SetUpdateSetting")
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	// The returned copy must not alias the stored one: mutating the local
	// copy then reading again must still show the original.
	got.Target = "mutated"
	again, _ := GetUpdateSetting()
	if again.Target != want.Target {
		t.Fatalf("returned copy shares state: target changed to %q", again.Target)
	}
}
