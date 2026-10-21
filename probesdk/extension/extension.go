// Package extension is the public face of the agent extension point
// (senhub-agent.go/internal/agent/services/extension), for a build that
// adds an agent-lifetime service from outside the core module.
//
// An extension registers itself from an init function:
//
//	func init() {
//		extension.Register(extension.Extension{
//			Name:        "example",
//			ConfigBlock: "example",
//			New:         newService,
//		})
//	}
//
// The agent calls New once at start with a Host, then the supervisor
// starts the returned Service after the built-in services and stops it
// before them. Self-metrics go through exporthook.RegisterSelfRecordProvider;
// a licence check through Host.Authorized.
package extension

import (
	"senhub-agent.go/internal/agent/services/configuration"
	iextension "senhub-agent.go/internal/agent/services/extension"
)

type (
	Extension    = iextension.Extension
	Service      = iextension.Service
	Host         = iextension.Host
	Identity     = iextension.Identity
	Health       = iextension.Health
	ProbeHealth  = iextension.ProbeHealth
	OutputHealth = iextension.OutputHealth
	ConfigFile   = iextension.ConfigFile
	CheckLine    = iextension.CheckLine
	DoctorInput  = iextension.DoctorInput
	DoctorLine   = iextension.DoctorLine
	// OverlayTarget, OverlayProbe and EntitySwitches are what an
	// Extension.Overlay may read and change.
	OverlayTarget  = iextension.OverlayTarget
	OverlayProbe   = iextension.OverlayProbe
	EntitySwitches = iextension.EntitySwitches
)

// Check line levels for `config check`.
const (
	CheckOK    = iextension.CheckOK
	CheckOff   = iextension.CheckOff
	CheckWarn  = iextension.CheckWarn
	CheckError = iextension.CheckError
)

// Doctor line levels.
const (
	DoctorOK   = iextension.DoctorOK
	DoctorWarn = iextension.DoctorWarn
	DoctorFail = iextension.DoctorFail
	DoctorSkip = iextension.DoctorSkip
)

// Register adds an extension. An invalid or duplicate one is left out and
// named in a warning when the agent starts.
func Register(e Extension) { iextension.Register(e) }

// RedactSecrets masks the credentials a free-form text can carry (the
// password of a URI, DSN password pairs) before it leaves the process.
func RedactSecrets(s string) string { return configuration.RedactSecretsInString(s) }
