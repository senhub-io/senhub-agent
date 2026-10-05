package http

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/configuration/secret"
	"senhub-agent.go/internal/agent/services/license"
	"senhub-agent.go/internal/agent/services/logger"
)

const (
	bindingAgentKey = "50327df5-35b0-4d3c-8288-a99ffbca544f"
	bindingOtherKey = "6f1b2c3d-1111-4222-8333-944455556666"
)

type bindingConsole struct {
	router     *mux.Router
	configPath string
	sign       func(subject string) string
}

func newBindingConsole(t *testing.T) *bindingConsole {
	t.Helper()
	if _, ok := spec.For("veeam"); !ok {
		spec.Register(spec.Probe{Type: "veeam", DisplayName: "Veeam"})
	}
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pub := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	validator, err := license.NewJWTValidator(pub, 7)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	main := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(main, []byte("config_version: 3\nagent:\n  key: \""+bindingAgentKey+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "strategies.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "strategies.d", "00-http.yaml"), []byte("http:\n  port: 8080\n  endpoints: [\"web\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret.SetConfigDir(dir)
	secret.SetProvider(secret.NewMemoryProvider())

	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	cfg := pathedConfig{AgentConfiguration: configuration.NewAgentConfiguration(bindingAgentKey, "", baseLogger), path: main}
	strategy, ok := NewHTTPSyncStrategy(cfg, map[string]interface{}{
		"endpoints": []interface{}{"web"},
		"admin_key": testAdminKey,
	}, baseLogger).(*HTTPSyncStrategy)
	if !ok {
		t.Fatal("strategy cast")
	}
	strategy.licenseValidatorFn = func() (*license.JWTValidator, error) { return validator, nil }

	return &bindingConsole{
		router:     NewHTTPHandlers(strategy).SetupRoutes(),
		configPath: main,
		sign: func(subject string) string {
			claims := jwt.MapClaims{
				"tier":              "pro",
				"authorized_probes": []string{"veeam"},
				"exp":               time.Now().Add(24 * time.Hour).Unix(),
				"iat":               time.Now().Unix(),
				"iss":               "SenHub",
			}
			if subject != "" {
				claims["sub"] = subject
			}
			s, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(priv)
			if err != nil {
				t.Fatal(err)
			}
			return s
		},
	}
}

func (c *bindingConsole) install(t *testing.T, subject string) {
	t.Helper()
	if err := configuration.WriteLicenseSidecar(c.configPath, c.sign(subject)); err != nil {
		t.Fatal(err)
	}
}

func (c *bindingConsole) veeamVerdict(t *testing.T, key string) (authorized, bound bool) {
	t.Helper()
	code, resp := doJSON(t, c.router, "GET", "/api/"+key+"/catalog/probes", nil)
	if code != 200 {
		t.Fatalf("catalog: %d %v", code, resp)
	}
	view, _ := resp["license"].(map[string]interface{})
	bound, _ = view["bound"].(bool)
	for _, e := range resp["probes"].([]interface{}) {
		m := e.(map[string]interface{})
		if m["type"] == "veeam" {
			authorized, _ = m["authorized"].(bool)
			return authorized, bound
		}
	}
	t.Fatal("veeam is not in the catalogue")
	return false, false
}

// The console is opened with the administration key while a licence
// binds to the agent's own key: every check must use the latter.
func TestConsoleLicenceBindingUsesTheAgentKey(t *testing.T) {
	for _, tc := range []struct {
		name    string
		subject string
		want    bool
	}{
		{"licence for this agent", bindingAgentKey, true},
		{"licence for another agent", bindingOtherKey, false},
		{"customer licence", "client1", true},
		{"unbound licence", "", true},
	} {
		for _, opener := range []string{testAdminKey} {
			t.Run(tc.name+" opened with "+opener[:5], func(t *testing.T) {
				c := newBindingConsole(t)
				c.install(t, tc.subject)

				authorized, bound := c.veeamVerdict(t, opener)
				if authorized != tc.want || bound != tc.want {
					t.Errorf("catalog: authorized=%v bound=%v, want %v", authorized, bound, tc.want)
				}

				if opener != testAdminKey {
					return
				}
				code, resp := doJSON(t, c.router, "POST", "/api/"+opener+"/config/probes", map[string]interface{}{
					"name": "veeam1", "type": "veeam", "params": map[string]interface{}{},
				})
				if (code == 201) != tc.want {
					t.Errorf("probe add: %d %v, want accepted=%v", code, resp, tc.want)
				}
				code, resp = doJSON(t, c.router, "GET", "/api/"+opener+"/config/probes", nil)
				if code != 200 {
					t.Fatalf("configured probes: %d %v", code, resp)
				}
			})
		}
	}
}

func TestConsoleLicenceUploadUsesTheAgentKey(t *testing.T) {
	for _, tc := range []struct {
		subject string
		want    int
	}{
		{bindingAgentKey, 200},
		{bindingOtherKey, 400},
		{"client1", 200},
		{"", 200},
	} {
		c := newBindingConsole(t)
		code, resp := doJSON(t, c.router, "POST", "/api/"+testAdminKey+"/config/settings", map[string]interface{}{
			"license": c.sign(tc.subject),
		})
		if code != tc.want {
			t.Errorf("upload with subject %q: %d %v, want %d", tc.subject, code, resp, tc.want)
		}
	}
}
