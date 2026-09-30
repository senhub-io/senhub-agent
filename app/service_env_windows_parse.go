package app

import "strings"

// parseServiceEnvironmentBlock reads the Environment value of a Windows
// service key: a REG_MULTI_SZ of KEY=VALUE strings, what `sc` and the
// services console set and what the service process inherits.
func parseServiceEnvironmentBlock(block []string) map[string]string {
	out := map[string]string{}
	for _, entry := range block {
		k, v, ok := strings.Cut(entry, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			continue
		}
		out[k] = v
	}
	return out
}

// imagePathConfig returns the --config-path an ImagePath passes to the
// service, "" when it passes none and the service uses the default path.
func imagePathConfig(imagePath string) string {
	args := unitArgs(imagePath)
	for i, a := range args {
		if a == "--config-path" && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, "--config-path="); ok {
			return v
		}
	}
	return ""
}
