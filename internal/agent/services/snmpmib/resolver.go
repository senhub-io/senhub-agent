// Package snmpmib resolves numeric SNMP OIDs to MIB-defined names using
// operator-supplied LOCAL MIB files loaded from disk at startup.
//
// It NEVER fetches MIBs over the network — that is a deliberate
// anti-pattern for this agent (vendor-neutrality; the abandoned snmptrap
// WIP that downloaded MIBs from an intake URL is the cautionary tale).
// Operators drop their vendor MIB files in a local directory and point a
// probe at it via `mib_paths`.
//
// gosmi keeps a process-global MIB store, so loading is cumulative across
// probe instances (more MIBs loaded = better resolution for everyone) and
// the package serialises access with a mutex. The resolver is shared:
// snmp_trap uses it to name trap and varbind OIDs; snmp_poll can adopt it
// for OID→name enrichment later.
package snmpmib

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/sleepinggenius2/gosmi"
	"github.com/sleepinggenius2/gosmi/types"

	"senhub-agent.go/internal/agent/services/logger"
)

var (
	initOnce sync.Once
	mu       sync.Mutex
	loaded   = map[string]bool{} // absolute file paths already parsed (dedup)
)

// Resolver translates numeric OIDs into MIB names. The zero value (or a
// nil pointer) is a valid, disabled resolver whose Resolve always misses,
// so callers can use it unconditionally and fall back to numeric OIDs.
type Resolver struct {
	enabled bool
}

// Load parses every MIB file under the given paths (files or directories)
// into the process-global store and returns a Resolver. Directories are
// added to the SMI search path first so inter-module imports resolve. A
// file that fails to parse is logged at debug and skipped — one bad
// vendor MIB never sinks the rest. An empty paths list yields a disabled
// resolver.
func Load(paths []string, log *logger.ModuleLogger) *Resolver {
	if len(paths) == 0 {
		return &Resolver{}
	}
	initOnce.Do(gosmi.Init)

	mu.Lock()
	defer mu.Unlock()

	count := 0
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			log.Warn().Err(err).Str("path", p).Msg("SNMP MIB path not accessible; skipping")
			continue
		}
		if info.IsDir() {
			// Add the dir to the SMI search path so gosmi can resolve
			// module imports, then load every file in it by module name.
			gosmi.AppendPath(p)
			entries, _ := os.ReadDir(p)
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				count += loadModule(p, e.Name(), log)
			}
		} else {
			gosmi.AppendPath(filepath.Dir(p))
			count += loadModule(filepath.Dir(p), filepath.Base(p), log)
		}
	}
	log.Info().Int("modules_loaded", count).Strs("paths", paths).Msg("Loaded operator SNMP MIBs")
	return &Resolver{enabled: count > 0}
}

// loadModule loads one MIB file by its module name. gosmi resolves a
// module by the file-name prefix before the first dot across the search
// path (e.g. "IF-MIB" for "IF-MIB.txt"), so we pass that prefix rather
// than a path. The real module name inside the file drives OID
// registration regardless of the file name.
func loadModule(dir, fileName string, log *logger.ModuleLogger) int {
	moduleName := strings.SplitN(fileName, ".", 2)[0]
	if moduleName == "" {
		return 0
	}
	key := filepath.Join(dir, fileName)
	if loaded[key] {
		return 0
	}
	loaded[key] = true
	name, err := gosmi.LoadModule(moduleName)
	if err != nil || name == "" {
		log.Debug().Err(err).Str("file", key).Msg("skipping unparseable/unresolved MIB file")
		return 0
	}
	log.Debug().Str("module", name).Str("file", key).Msg("loaded MIB module")
	return 1
}

// Resolve translates a dotted numeric OID (with or without a leading dot)
// into a readable MIB label. For an instance OID — a scalar's `.0` or a
// table column's row index — it falls back to the nearest registered
// ancestor node and appends the remaining sub-identifiers, so
// `1.3.6.1.2.1.2.2.1.8.3` resolves to `ifOperStatus.3` and the bare
// notification OID `1.3.6.1.6.3.1.1.5.3` to `linkDown`. Returns ok=false
// when nothing matches or no MIB is loaded.
func (r *Resolver) Resolve(oid string) (string, bool) {
	if r == nil || !r.enabled {
		return "", false
	}
	full, err := types.OidFromString(strings.TrimPrefix(oid, "."))
	if err != nil || len(full) == 0 {
		return "", false
	}

	mu.Lock()
	defer mu.Unlock()
	// gosmi resolves to the nearest registered ancestor node, so one call
	// handles both exact nodes and instance OIDs (scalar .0 / table row
	// index). node.Oid is the matched node's own OID — the remaining
	// sub-identifiers form the instance suffix.
	node, err := gosmi.GetNodeByOID(full)
	if err != nil || node.Name == "" {
		return "", false
	}
	// A match no deeper than a structural container of the OID tree (iso,
	// enterprises, mib-2, …) means the OID's defining MIB isn't loaded —
	// report a miss so the caller keeps the numeric OID rather than a
	// useless "enterprises.99999.1" render.
	if genericContainers[node.Name] {
		return "", false
	}
	suffix := full[len(node.Oid):]
	if len(suffix) == 0 {
		return node.Name, true
	}
	return node.Name + "." + joinSubIDs(suffix), true
}

// genericContainers are the structural nodes of the OID tree that are
// always present (the SMI skeleton). A match no deeper than one of these
// means the OID's defining MIB isn't loaded.
var genericContainers = map[string]bool{
	"iso": true, "org": true, "dod": true, "internet": true,
	"directory": true, "mgmt": true, "mib-2": true, "experimental": true,
	"private": true, "enterprises": true, "security": true, "snmpV2": true,
	"transmission": true, "ccitt": true, "joint-iso-ccitt": true,
	"zeroDotZero": true,
}

func joinSubIDs(o types.Oid) string {
	parts := make([]string, len(o))
	for i, s := range o {
		parts[i] = strconv.FormatUint(uint64(s), 10)
	}
	return strings.Join(parts, ".")
}
