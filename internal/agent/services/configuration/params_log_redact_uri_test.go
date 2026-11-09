package configuration

import (
	"fmt"
	"strings"
	"testing"
)

// The log redaction judged a value by its key only, so a connection URI
// under a key like "uri" or "url" went to the log with its password.
func TestSanitizeParamsForLog_MasksCredentialsEmbeddedInStrings(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"mongodb", "mongodb://app:S3cret@db1:27017/prod", "mongodb://***@db1:27017/prod"},
		{"mongodb+srv", "mongodb+srv://app:S3cret@cluster.example.net/prod?retryWrites=true", "mongodb+srv://***@cluster.example.net/prod?retryWrites=***"},
		{"postgres", "postgres://app:S3cret@db:5432/prod?sslmode=require", "postgres://***@db:5432/prod?sslmode=***"},
		{"postgresql", "postgresql://app:S3cret@db/prod", "postgresql://***@db/prod"},
		{"amqp", "amqp://guest:S3cret@mq:5672/vhost", "amqp://***@mq:5672/vhost"},
		{"amqps", "amqps://guest:S3cret@mq:5671/", "amqps://***@mq:5671/"},
		{"redis", "redis://default:S3cret@cache:6379/0", "redis://***@cache:6379/0"},
		{"https", "https://svc:S3cret@api.example.com/v1", "https://***@api.example.com/v1"},
		{"http", "http://svc:S3cret@10.0.0.5:8080", "http://***@10.0.0.5:8080"},
		{"empty user", "redis://:S3cret@cache:6379", "redis://***@cache:6379"},
		{"at sign in password", "mongodb://app:p@ss@db1/prod", "mongodb://***@db1/prod"},
		{"several hosts", "mongodb://app:S3cret@h1:27017,h2:27017/prod", "mongodb://***@h1:27017,h2:27017/prod"},
		{"libpq dsn", "host=db user=app password=S3cret dbname=prod", "host=db user=app password=*** dbname=prod"},
		{"libpq quoted", "host=db password='S 3cret' dbname=prod", "host=db password=*** dbname=prod"},
		{"odbc dsn", "Server=db;Database=prod;User Id=app;Pwd=S3cret;Encrypt=true", "Server=db;Database=prod;User Id=app;Pwd=***;Encrypt=true"},
		{"mysql dsn password key", "Password = S3cret;Port=3306", "Password = ***;Port=3306"},
		{"query string", "https://host/path?password=S3cret&x=1", "https://host/path?password=***&x=***"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeParamsForLog(map[string]interface{}{"target": tc.in})["target"]
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if strings.Contains(got.(string), "S3cret") || strings.Contains(got.(string), "p@ss") {
				t.Errorf("password still present: %q", got)
			}
		})
	}
}

func TestSanitizeParamsForLog_MasksEmbeddedCredentialsInNestedValues(t *testing.T) {
	in := map[string]interface{}{
		"nodes": []interface{}{
			map[string]interface{}{"endpoint": "redis://default:S3cret@a:6379"},
			"amqp://guest:S3cret@mq",
		},
		"targets": []string{"postgres://u:S3cret@h/db"},
		"tls": map[interface{}]interface{}{
			"ca_source": "https://u:S3cret@pki.example.com/ca.pem",
		},
	}
	out := SanitizeParamsForLog(in)
	if strings.Contains(fmt.Sprint(out), "S3cret") {
		t.Errorf("a nested value kept its password: %v", out)
	}
	if got := in["targets"].([]string)[0]; !strings.Contains(got, "S3cret") {
		t.Errorf("the caller's slice was mutated: %q", got)
	}
}

func TestSanitizeParamsForLog_MasksURLForms(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"query value masked", "https://example.com/path?x=1", "https://example.com/path?x=***"},
		{"userinfo masked", "https://token@example.com/repo", "https://***@example.com/repo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeParamsForLog(map[string]interface{}{"target": tc.in})["target"]
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSanitizeParamsForLog_LeavesAHarmlessURLIntact(t *testing.T) {
	for _, v := range []string{
		"http://10.0.0.5:8080/api/key",
		"mongodb://db1:27017/prod",
		"password policy: rotate often",
		"host=db port=5432",
		"Is the service up? yes",
		"plain text #section",
		"author: Jane Smith",
		"authority=paris",
		"Basic authentication required",
		"bearer of the news",
	} {
		got := SanitizeParamsForLog(map[string]interface{}{"target": v})["target"]
		if got != v {
			t.Errorf("%q was altered to %q", v, got)
		}
	}
}

func TestRedactSecretsInString_MasksCredentialShapes(t *testing.T) {
	jwt := "eyJhbGciOiJub25lIn0.eyJzdWIiOiJ0ZXN0In0.c2ln"
	cases := []struct {
		name    string
		in      string
		want    string
		secrets []string
	}{
		{
			name:    "query with token and api_key",
			in:      "https://h.example/api?token=fake-token-123&api_key=fake-key-456",
			want:    "https://h.example/api?token=***&api_key=***",
			secrets: []string{"fake-token-123", "fake-key-456"},
		},
		{
			name:    "query with several params and a fragment",
			in:      "https://h.example/api?page=2&token=fake-token-123#frag",
			want:    "https://h.example/api?page=***&token=***#***",
			secrets: []string{"fake-token-123"},
		},
		{
			name:    "userinfo user password",
			in:      "https://user:fake-pass-123@example.com/path",
			want:    "https://***@example.com/path",
			secrets: []string{"fake-pass-123"},
		},
		{
			name:    "userinfo user only",
			in:      "https://fake-user@example.com/repo",
			want:    "https://***@example.com/repo",
			secrets: []string{"fake-user"},
		},
		{
			name:    "jwt inside a URL path segment",
			in:      "https://h.example/" + jwt + "/resource",
			want:    "https://h.example/***/resource",
			secrets: []string{jwt},
		},
		{
			name:    "jwt after bearer",
			in:      "Bearer " + jwt,
			want:    "Bearer ***",
			secrets: []string{jwt},
		},
		{
			name:    "access key id inside a sentence",
			in:      "key AKIAFAKEFAKEFAKE0000 here",
			want:    "key *** here",
			secrets: []string{"AKIAFAKEFAKEFAKE0000"},
		},
		{
			name:    "dsn password still masked",
			in:      "host=db password=fake-pass-123",
			want:    "host=db password=***",
			secrets: []string{"fake-pass-123"},
		},
		{
			name:    "authorization bearer header",
			in:      "Authorization: Bearer fake-opaque-token",
			want:    "Authorization: Bearer ***",
			secrets: []string{"fake-opaque-token"},
		},
		{
			name:    "authorization digest header",
			in:      "Authorization: Digest fake-digest-1",
			want:    "Authorization: Digest ***",
			secrets: []string{"fake-digest-1"},
		},
		{
			name:    "authorization raw credential header",
			in:      "Authorization: fake-raw-2",
			want:    "Authorization: ***",
			secrets: []string{"fake-raw-2"},
		},
		{
			name:    "authorization digest params keep trailing text",
			in:      `Authorization: Digest username="u", response="fake1" -- retrying`,
			want:    "Authorization: Digest *** -- retrying",
			secrets: []string{"fake1"},
		},
		{
			name:    "authorization basic token keeps trailing text",
			in:      "Authorization: Basic fake2 request failed",
			want:    "Authorization: Basic *** request failed",
			secrets: []string{"fake2"},
		},
		{
			name: "authorization scheme without credential unchanged",
			in:   "Authorization: Basic",
			want: "Authorization: Basic",
		},
		{
			name:    "standalone bearer credential",
			in:      "Bearer fake.tok3n",
			want:    "Bearer ***",
			secrets: []string{"fake.tok3n"},
		},
		{
			name:    "dashed api key flag",
			in:      "--api-key=fake-key-1 --verbose",
			want:    "--api-key=*** --verbose",
			secrets: []string{"fake-key-1"},
		},
		{
			name:    "client secret colon pair",
			in:      "client_secret: fake-secret-2",
			want:    "client_secret: ***",
			secrets: []string{"fake-secret-2"},
		},
		{
			name:    "quoted token value",
			in:      `token="fake token 3"`,
			want:    `token=***`,
			secrets: []string{"fake token 3"},
		},
		{
			name:    "oauth pair",
			in:      "oauth=fake",
			want:    "oauth=***",
			secrets: []string{"fake"},
		},
		{
			name:    "authtoken pair",
			in:      "authtoken=fake",
			want:    "authtoken=***",
			secrets: []string{"fake"},
		},
		{
			name:    "accessToken pair",
			in:      "accessToken=fake",
			want:    "accessToken=***",
			secrets: []string{"fake"},
		},
		{
			name:    "proxyauth pair",
			in:      "proxyauth=fake",
			want:    "proxyauth=***",
			secrets: []string{"fake"},
		},
		{
			name: "url with no query unchanged",
			in:   "https://example.com/path",
			want: "https://example.com/path",
		},
		{
			name: "url fragment masked",
			in:   "https://example.com/path#section",
			want: "https://example.com/path#***",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactSecretsInString(tc.in)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			for _, secret := range tc.secrets {
				if strings.Contains(got, secret) {
					t.Errorf("secret %q still present: %q", secret, got)
				}
			}
		})
	}
}

func TestSanitizeParamsForLog_MasksAnyQueryValueUnderAHarmlessKey(t *testing.T) {
	got := SanitizeParamsForLog(map[string]interface{}{
		"url": "https://h.example/api?token=fake-token-123",
	})["url"].(string)
	if !strings.Contains(got, "token=***") {
		t.Errorf("expected token=*** in %q", got)
	}
	if strings.Contains(got, "fake-token-123") {
		t.Errorf("token still present: %q", got)
	}
}

func TestRedactSecretsInString_MasksWholeDigestAuthorizationParams(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		secrets []string
	}{
		{
			name:    "quoted digest parameters",
			in:      `Authorization: Digest username="u", realm="r", nonce="n", response="fake-resp-1"`,
			want:    "Authorization: Digest ***",
			secrets: []string{"fake-resp-1", "username", "realm", "nonce"},
		},
		{
			name:    "unquoted digest parameters",
			in:      "Authorization: Digest username=u, response=fake-resp-2",
			want:    "Authorization: Digest ***",
			secrets: []string{"fake-resp-2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactSecretsInString(tc.in)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			for _, secret := range tc.secrets {
				if strings.Contains(got, secret) {
					t.Errorf("secret %q still present: %q", secret, got)
				}
			}
		})
	}
}

func TestRedactSecretsInString_MasksURLFragmentsOnlyInURLs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"fragment after query", "https://h.example/p?x=1#fake-frag-3", "https://h.example/p?x=***#***"},
		{"fragment without query", "https://h.example/p#fake-frag-4", "https://h.example/p#***"},
		{"plain text fragment untouched", "see docs #fake-frag-5", "see docs #fake-frag-5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RedactSecretsInString(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSanitizeParamsForLog_DoesNotTreatAuthorWordsAsCredentials(t *testing.T) {
	got := SanitizeParamsForLog(map[string]interface{}{"neg_author": "author: x"})["neg_author"]
	if got != "author: x" {
		t.Errorf("got %q, want %q", got, "author: x")
	}

	positives := []struct {
		in   string
		want string
	}{
		{"auth: fake", "auth: ***"},
		{"x-auth-token: fake", "x-auth-token: ***"},
		{"--auth=fake", "--auth=***"},
	}
	for _, tc := range positives {
		t.Run(tc.in, func(t *testing.T) {
			if got := RedactSecretsInString(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
