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
		{"mongodb", "mongodb://app:S3cret@db1:27017/prod", "mongodb://app:***@db1:27017/prod"},
		{"mongodb+srv", "mongodb+srv://app:S3cret@cluster.example.net/prod?retryWrites=true", "mongodb+srv://app:***@cluster.example.net/prod?retryWrites=true"},
		{"postgres", "postgres://app:S3cret@db:5432/prod?sslmode=require", "postgres://app:***@db:5432/prod?sslmode=require"},
		{"postgresql", "postgresql://app:S3cret@db/prod", "postgresql://app:***@db/prod"},
		{"amqp", "amqp://guest:S3cret@mq:5672/vhost", "amqp://guest:***@mq:5672/vhost"},
		{"amqps", "amqps://guest:S3cret@mq:5671/", "amqps://guest:***@mq:5671/"},
		{"redis", "redis://default:S3cret@cache:6379/0", "redis://default:***@cache:6379/0"},
		{"https", "https://svc:S3cret@api.example.com/v1", "https://svc:***@api.example.com/v1"},
		{"http", "http://svc:S3cret@10.0.0.5:8080", "http://svc:***@10.0.0.5:8080"},
		{"empty user", "redis://:S3cret@cache:6379", "redis://:***@cache:6379"},
		{"at sign in password", "mongodb://app:p@ss@db1/prod", "mongodb://app:***@db1/prod"},
		{"several hosts", "mongodb://app:S3cret@h1:27017,h2:27017/prod", "mongodb://app:***@h1:27017,h2:27017/prod"},
		{"libpq dsn", "host=db user=app password=S3cret dbname=prod", "host=db user=app password=*** dbname=prod"},
		{"libpq quoted", "host=db password='S 3cret' dbname=prod", "host=db password=*** dbname=prod"},
		{"odbc dsn", "Server=db;Database=prod;User Id=app;Pwd=S3cret;Encrypt=true", "Server=db;Database=prod;User Id=app;Pwd=***;Encrypt=true"},
		{"mysql dsn password key", "Password = S3cret;Port=3306", "Password = ***;Port=3306"},
		{"query string", "https://host/path?password=S3cret&x=1", "https://host/path?password=***&x=1"},
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

func TestSanitizeParamsForLog_LeavesAHarmlessURLIntact(t *testing.T) {
	for _, v := range []string{
		"https://example.com/path?x=1",
		"http://10.0.0.5:8080/api/key",
		"https://token@example.com/repo",
		"mongodb://db1:27017/prod",
		"password policy: rotate often",
		"host=db port=5432",
	} {
		got := SanitizeParamsForLog(map[string]interface{}{"target": v})["target"]
		if got != v {
			t.Errorf("%q was altered to %q", v, got)
		}
	}
}
