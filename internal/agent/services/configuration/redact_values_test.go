package configuration

import (
	"math"
	"strings"
	"testing"
)

func TestValueFromEnv(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"full reference", "${env:API_TOKEN}", true},
		{"embedded reference", "prefix-${env:X}", true},
		{"plain value", "plain", false},
		{"wrong namespace", "${secret:x}", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := valueFromEnv(tt.raw); got != tt.want {
				t.Errorf("valueFromEnv(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestMaskWebhookPath(t *testing.T) {
	slackPath := "/services/" + "T012ABC" + "/" + "B045DEF" + "/" + "XYZxyz123456"
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"slack",
			"https://hooks.slack.com" + slackPath,
			"https://hooks.slack.com/***",
		},
		{
			"teams",
			"https://contoso.webhook.office.com/webhookb2/" + "1234-abcd" + "@tenant/IncomingWebhook/" + "5678",
			"https://contoso.webhook.office.com/***",
		},
		{
			"outlook",
			"https://outlook.office.com/webhook/" + "1234-abcd" + "/IncomingWebhook/" + "5678",
			"https://outlook.office.com/***",
		},
		{
			"discord",
			"https://discord.com/api/webhooks/1/" + "abc123def456",
			"https://discord.com/***",
		},
		{
			"discordapp",
			"https://discordapp.com/api/webhooks/2/" + "xyz987uvw654",
			"https://discordapp.com/***",
		},
		{
			"logic apps",
			"https://prod-01.westeurope.logic.azure.com/workflows/" + "00112233" + "/triggers/manual/paths/invoke?api-version=2016-10-01&sp=%2Ftriggers%2Fmanual%2Frun&sv=1.0&sig=" + "sigsig",
			"https://prod-01.westeurope.logic.azure.com/***",
		},
		{
			"url inside sentence",
			"see https://hooks.slack.com" + slackPath + " for details",
			"see https://hooks.slack.com/*** for details",
		},
		{
			"unrelated host unchanged",
			"https://example.com/a/b",
			"https://example.com/a/b",
		},
		{
			"slack uppercase host",
			"https://HOOKS.SLACK.COM" + slackPath,
			"https://HOOKS.SLACK.COM/***",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maskWebhookPath(tt.input); got != tt.want {
				t.Errorf("maskWebhookPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLooksLikeSecretTrue(t *testing.T) {
	// Deterministic 40-char base64-like string built by concatenation.
	opaque := "Qw7Zx2Vb" + "Nm9Lk4Tp" + "Rc8Hj3Ys" + "Df6Ga1Eu" + "So5Ib0Wz"

	tests := []struct {
		name  string
		value string
	}{
		{"ghp_", "ghp_" + strings.Repeat("a1", 10)},
		{"gho_", "gho_" + strings.Repeat("a1", 10)},
		{"ghs_", "ghs_" + strings.Repeat("a1", 10)},
		{"ghu_", "ghu_" + strings.Repeat("a1", 10)},
		{"github_pat_", "github_pat_" + strings.Repeat("a1", 10)},
		{"glpat-", "glpat-" + strings.Repeat("a1", 10)},
		{"xoxb-", "xox" + "b-" + strings.Repeat("a1", 15)},
		{"xoxp-", "xox" + "p-" + strings.Repeat("a1", 15)},
		{"xoxa-", "xox" + "a-" + strings.Repeat("a1", 15)},
		{"xapp-", "xapp-" + strings.Repeat("a1", 10)},
		{"sk-", "sk-" + strings.Repeat("a1", 10)},
		{"sk_live_", "sk_live_" + strings.Repeat("a1", 10)},
		{"rk_live_", "rk_live_" + strings.Repeat("a1", 10)},
		{"AKIA", "AKIA" + strings.Repeat("b2", 10)},
		{"ASIA", "ASIA" + strings.Repeat("b2", 10)},
		{"AIza", "AIza" + strings.Repeat("b2", 10)},
		{"SG.", "SG." + strings.Repeat("xY9", 6)},
		{"eyJ JWT", "eyJ" + "abc123def456ghi789" + "." + "xyz987uvw" + "." + "sig12345"},
		{"opaque random", opaque},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !looksLikeSecret(tt.value) {
				t.Errorf("looksLikeSecret(%q) = false, want true", tt.value)
			}
		})
	}
}

func TestLooksLikeSecretFalse(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"plain word", "postgres"},
		{"hostname", "db1.example.com"},
		{"ip address", "10.0.0.5"},
		{"uuid", "550e8400-e29b-41d4-a716-446655440000"},
		{"file path", "/var/lib/senhub/agent.db"},
		{"url", "https://example.com/health"},
		{"duration", "60s"},
		{"bool", "true"},
		{"sql", "SELECT 1"},
		{"low entropy", strings.Repeat("a", 30)},
		{"long plain name", "my-very-long-but-plain-name-for-a-probe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if looksLikeSecret(tt.value) {
				t.Errorf("looksLikeSecret(%q) = true, want false", tt.value)
			}
		})
	}
}

func TestShannonEntropy(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  float64
	}{
		{"uniform", "aaaa", 0},
		{"two symbols", "ab", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shannonEntropy(tt.input)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("shannonEntropy(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
