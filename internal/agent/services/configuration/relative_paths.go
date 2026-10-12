package configuration

import (
	"path/filepath"
	"strings"
)

// resolveRelativePaths anchors the file paths a probe or a strategy names
// to the configuration directory. A relative path used to mean the working
// directory of whoever read it: the service's directory at runtime, the
// operator's shell for `config check`, so the same file could be found by
// one and refused (or silently not found) by the other.
//
// A parameter is a file path when its key is bookmark_path or ends in
// _file (ca_file, cert_file, key_file, password_file...). Other keys that
// end in _path are left alone: they are often URL paths. Values that are
// URLs, unresolved references or already absolute stay as written.
func resolveRelativePaths(data *LocalConfigurationData, configPath string) {
	base, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		return
	}
	for i := range data.Probes {
		anchorPaths(data.Probes[i].Params, base)
	}
	for i := range data.Storage {
		anchorPaths(data.Storage[i].Params, base)
	}
}

func anchorPaths(m map[string]interface{}, base string) {
	for k, v := range m {
		switch t := v.(type) {
		case string:
			if isFilePathKey(k) && isRelativeFilePath(t) {
				m[k] = filepath.Join(base, t)
			}
		case map[string]interface{}:
			anchorPaths(t, base)
		case []interface{}:
			for _, it := range t {
				if sub, ok := it.(map[string]interface{}); ok {
					anchorPaths(sub, base)
				}
			}
		}
	}
}

func isFilePathKey(k string) bool {
	k = strings.ToLower(k)
	return k == "bookmark_path" || strings.HasSuffix(k, "_file")
}

func isRelativeFilePath(v string) bool {
	if v == "" || strings.Contains(v, "://") || strings.HasPrefix(v, "$") || strings.HasPrefix(v, "unix:") {
		return false
	}
	if filepath.IsAbs(v) || strings.HasPrefix(v, "/") || strings.HasPrefix(v, `\`) {
		return false
	}
	return true
}
