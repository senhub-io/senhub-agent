// Package dbcommon is the public mirror of the shared database-probe
// helpers (senhub-agent.go/internal/agent/probes/dbcommon) used by the
// mysql and postgresql probes.
package dbcommon

import idbcommon "senhub-agent.go/internal/agent/probes/dbcommon"

type (
	Role        = idbcommon.Role
	MetricType  = idbcommon.MetricType
	Environment = idbcommon.Environment
)

const (
	RolePrimary    = idbcommon.RolePrimary
	RoleReplica    = idbcommon.RoleReplica
	RoleStandalone = idbcommon.RoleStandalone
)

const (
	MetricTypeOverview       = idbcommon.MetricTypeOverview
	MetricTypeConnections    = idbcommon.MetricTypeConnections
	MetricTypeThroughput     = idbcommon.MetricTypeThroughput
	MetricTypeCache          = idbcommon.MetricTypeCache
	MetricTypeIO             = idbcommon.MetricTypeIO
	MetricTypeLocks          = idbcommon.MetricTypeLocks
	MetricTypeReplication    = idbcommon.MetricTypeReplication
	MetricTypeStorage        = idbcommon.MetricTypeStorage
	MetricTypePerDatabase    = idbcommon.MetricTypePerDatabase
	MetricTypePerTable       = idbcommon.MetricTypePerTable
	MetricTypeStatStatements = idbcommon.MetricTypeStatStatements
	MetricTypeBloat          = idbcommon.MetricTypeBloat
	MetricTypeArchiver       = idbcommon.MetricTypeArchiver
)

// DetectEnvironment classifies a server version string into an
// Environment.
func DetectEnvironment(versionString string) Environment {
	return idbcommon.DetectEnvironment(versionString)
}

// IsSystemDatabase reports whether a database name is an engine-internal
// system database that should be excluded from per-database metrics.
func IsSystemDatabase(name string) bool {
	return idbcommon.IsSystemDatabase(name)
}
