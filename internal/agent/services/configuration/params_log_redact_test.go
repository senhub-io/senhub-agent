package configuration

import (
	"testing"
)

func TestSanitizeParamsForLog_MasksSecretKeys(t *testing.T) {
	in := map[string]interface{}{
		"host":             "example.test",
		"port":             22,
		"user":             "admin",
		"password":         "hunter2",
		"api_token":        "tok-abc",
		"client_secret":    "shhh",
		"db_password":      "p4ss",
		"auth_login":       "svc-acct",
		"contact_email":    "ops@example.test",
		"trusted_user_ids": []int{1, 2},
		"interval":         30,
		"runner_dir":       "/opt/runner",
	}
	out := SanitizeParamsForLog(in)
	for _, k := range []string{"user", "password", "api_token", "client_secret", "db_password", "auth_login", "contact_email", "trusted_user_ids"} {
		if out[k] != "***" {
			t.Errorf("key %q should have been masked; got %v", k, out[k])
		}
	}
	for _, k := range []string{"host", "port", "interval", "runner_dir"} {
		if out[k] == "***" {
			t.Errorf("key %q should NOT have been masked", k)
		}
	}
}

func TestSanitizeParamsForLog_NilIsNil(t *testing.T) {
	if got := SanitizeParamsForLog(nil); got != nil {
		t.Errorf("nil input should return nil, got %v", got)
	}
}

func TestSanitizeParamsForLog_DoesNotMutateInput(t *testing.T) {
	in := map[string]interface{}{
		"user":     "admin",
		"password": "hunter2",
	}
	_ = SanitizeParamsForLog(in)
	if in["user"] != "admin" {
		t.Errorf("input was mutated: user = %v", in["user"])
	}
	if in["password"] != "hunter2" {
		t.Errorf("input was mutated: password = %v", in["password"])
	}
}

func TestSanitizeParamsForLog_CaseInsensitiveMatch(t *testing.T) {
	in := map[string]interface{}{
		"USER":        "ALICE",
		"Password":    "shh",
		"AuthLogin":   "svc",
		"X-API-TOKEN": "tok-abc",
	}
	out := SanitizeParamsForLog(in)
	for k := range in {
		if out[k] != "***" {
			t.Errorf("case variation %q not masked: got %v", k, out[k])
		}
	}
}
