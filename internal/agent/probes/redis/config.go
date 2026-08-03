package redis

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"
)

type probeConfig struct {
	Host         string
	Port         int
	Password     string
	TLS          bool
	TLSCertFile  string
	TLSKeyFile   string
	TLSCAFile    string
	Timeout      time.Duration
	Interval     time.Duration
	InstanceName string
}

func parseConfig(raw map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		Host:     "127.0.0.1",
		Port:     6379,
		Timeout:  defaultTimeout,
		Interval: defaultInterval,
	}

	if v, ok := raw["host"].(string); ok && v != "" {
		cfg.Host = v
	}
	if v, ok := raw["port"].(int); ok {
		if v <= 0 || v > 65535 {
			return cfg, fmt.Errorf("redis probe: port %d is out of range (1–65535)", v)
		}
		cfg.Port = v
	}
	if v, ok := raw["password"].(string); ok {
		cfg.Password = v
	}
	if v, ok := raw["tls"].(bool); ok {
		cfg.TLS = v
	}
	if v, ok := raw["tls_cert_file"].(string); ok {
		cfg.TLSCertFile = v
	}
	if v, ok := raw["tls_key_file"].(string); ok {
		cfg.TLSKeyFile = v
	}
	if v, ok := raw["tls_ca_file"].(string); ok {
		cfg.TLSCAFile = v
	}
	if (cfg.TLSCertFile != "") != (cfg.TLSKeyFile != "") {
		return cfg, fmt.Errorf("redis probe: tls_cert_file and tls_key_file must both be set for mTLS (tls_cert_file=%q, tls_key_file=%q)", cfg.TLSCertFile, cfg.TLSKeyFile)
	}
	if v, ok := raw["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := raw["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := raw["instance_name"].(string); ok {
		cfg.InstanceName = v
	}
	return cfg, nil
}

// tlsClientConfig builds the tls.Config used to upgrade the raw TCP
// connection. Client certificate and custom CA material is loaded at
// probe construction so a bad path fails fast instead of at collect time.
func (c probeConfig) tlsClientConfig() (*tls.Config, error) {
	tlsCfg := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}

	if c.TLSCertFile != "" && c.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(c.TLSCertFile, c.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("redis probe: loading client certificate pair (%s, %s): %w", c.TLSCertFile, c.TLSKeyFile, err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	if c.TLSCAFile != "" {
		pemBytes, err := os.ReadFile(c.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("redis probe: reading CA bundle %s: %w", c.TLSCAFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, fmt.Errorf("redis probe: CA bundle %s contains no valid PEM certificate", c.TLSCAFile)
		}
		tlsCfg.RootCAs = pool
	}

	return tlsCfg, nil
}
