package kuberneteslogs

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/logparse"
	"senhub-agent.go/internal/agent/probes/types"
)

const (
	DefaultRoot         = "/var/log/pods"
	DefaultScanInterval = 5 * time.Second
	MinScanInterval     = 1 * time.Second
	DefaultMaxLineBytes = 256 << 10
)

type Config struct {
	Root              string
	IncludeNamespaces []string
	ExcludeNamespaces []string
	IncludePods       []string
	ExcludePods       []string
	ScanInterval      time.Duration
	FromBeginning     bool
	MaxLineBytes      int
	BookmarkPath      string
	Parser            logparse.ParserConfig
	Multiline         logparse.MultilineConfig
}

func parseConfig(config map[string]interface{}) (Config, error) {
	parsed := Config{
		Root:         DefaultRoot,
		ScanInterval: DefaultScanInterval,
		MaxLineBytes: DefaultMaxLineBytes,
		Parser:       logparse.ParserConfig{Type: logparse.ParserRaw},
	}

	if s, ok := config["root"].(string); ok && s != "" {
		parsed.Root = s
	}

	parsed.IncludeNamespaces, _ = types.StringSliceParam(config, "include_namespaces")
	parsed.ExcludeNamespaces, _ = types.StringSliceParam(config, "exclude_namespaces")
	parsed.IncludePods, _ = types.StringSliceParam(config, "include_pods")
	parsed.ExcludePods, _ = types.StringSliceParam(config, "exclude_pods")

	if v, ok := config["from_beginning"].(bool); ok {
		parsed.FromBeginning = v
	}

	if n, ok := types.IntParam(config, "max_line_bytes"); ok {
		if n <= 0 {
			return Config{}, fmt.Errorf("kubernetes_logs: max_line_bytes must be > 0, got %d", n)
		}
		parsed.MaxLineBytes = n
	}

	if s, ok := config["bookmark_path"].(string); ok {
		parsed.BookmarkPath = s
	}

	scanInterval, err := durationParam(config, "scan_interval", DefaultScanInterval)
	if err != nil {
		return Config{}, err
	}
	if scanInterval < MinScanInterval {
		return Config{}, fmt.Errorf("kubernetes_logs: scan_interval must be at least %s", MinScanInterval)
	}
	parsed.ScanInterval = scanInterval

	if err := logparse.ParseParser(config, &parsed.Parser); err != nil {
		return Config{}, fmt.Errorf("kubernetes_logs: %w", err)
	}
	if err := logparse.ParseMultiline(config, &parsed.Multiline); err != nil {
		return Config{}, fmt.Errorf("kubernetes_logs: %w", err)
	}

	return parsed, nil
}

func durationParam(config map[string]interface{}, key string, def time.Duration) (time.Duration, error) {
	raw, ok := config[key]
	if !ok {
		return def, nil
	}

	switch v := raw.(type) {
	case string:
		if v == "" {
			return def, nil
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			return 0, fmt.Errorf("kubernetes_logs: %s must be a valid duration: %w", key, err)
		}
		return d, nil
	case int:
		return time.Duration(v) * time.Second, nil
	case int64:
		return time.Duration(v) * time.Second, nil
	case float64:
		return time.Duration(v * float64(time.Second)), nil
	default:
		return 0, fmt.Errorf("kubernetes_logs: %s must be a duration string or integer seconds", key)
	}
}
