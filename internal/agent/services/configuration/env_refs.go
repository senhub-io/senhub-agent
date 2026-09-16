package configuration

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// EnvReference is a ${env:NAME} reference without a default, found in
// one configuration file.
type EnvReference struct {
	Name string
	File string
}

// bareEnvReference matches ${env:NAME} with no ":-default" part: the
// only form that silently resolves to "" when NAME is not set.
var bareEnvReference = regexp.MustCompile(`\$\{env:([^}:]+)\}`)

// UnsetEnvReferences lists the ${env:NAME} references of a
// configuration whose variable is not set in this process, so a
// diagnostic can name the variable rather than the file. It reads the
// top-level file and the probes.d and strategies.d fragments beside it;
// a file it cannot read is skipped, since the loader reports that on
// its own. The result is sorted by name then file, without duplicates.
func UnsetEnvReferences(configPath string) []EnvReference {
	files := []string{configPath}
	baseDir := filepath.Dir(configPath)
	for _, sub := range []string{"probes.d", "strategies.d"} {
		if more, err := listYAMLFiles(filepath.Join(baseDir, sub)); err == nil {
			files = append(files, more...)
		}
	}

	seen := map[EnvReference]struct{}{}
	var refs []EnvReference
	for _, file := range files {
		raw, err := os.ReadFile(file) // #nosec G304 - the configuration files the loader itself reads
		if err != nil {
			continue
		}
		for _, m := range bareEnvReference.FindAllStringSubmatch(string(raw), -1) {
			if _, set := os.LookupEnv(m[1]); set {
				continue
			}
			ref := EnvReference{Name: m[1], File: file}
			if _, dup := seen[ref]; dup {
				continue
			}
			seen[ref] = struct{}{}
			refs = append(refs, ref)
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Name != refs[j].Name {
			return refs[i].Name < refs[j].Name
		}
		return refs[i].File < refs[j].File
	})
	return refs
}
