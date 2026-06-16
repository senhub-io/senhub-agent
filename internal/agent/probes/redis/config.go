package redis

import (
	"fmt"
	"time"
)

type probeConfig struct {
	Host         string
	Port         int
	Password     string
	TLS          bool
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
