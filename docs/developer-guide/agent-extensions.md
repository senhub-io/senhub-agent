# Agent extensions

A build can add a service that runs for the agent's lifetime without the
core knowing what it is. The extension lives in its own Go module,
imports only `senhub-agent.go/probesdk/...`, and registers itself from an
`init` function; the build's `main` package pulls it in with a blank
import, the way a probe package is pulled in.

## Registering

```go
package example

import (
	"context"

	"senhub-agent.go/probesdk/extension"
)

func init() {
	extension.Register(extension.Extension{
		Name:        "example",
		ConfigBlock: "example",
		New:         func(h extension.Host) (extension.Service, error) { return &service{host: h}, nil },
		Check:       check,
		Doctor:      doctor,
	})
}

type service struct{ host extension.Host }

func (s *service) Start(ctx context.Context) error    { return nil }
func (s *service) Shutdown(ctx context.Context) error { return nil }
```

| Field | Meaning |
|---|---|
| `Name` | Unique. Names the extension in logs, in the status channel and in the default doctor ID (`config.<name>`). |
| `ConfigBlock` | The top-level `agent.yaml` key the extension owns; empty when it reads none. Unique among extensions. |
| `New` | Builds the service, once per agent start. An error leaves the extension out with an error line; the agent runs. |
| `StopBudget` | The time `Shutdown` may take. Zero is the supervisor default (3 s). |
| `Check` | Lines added to `config check` for the block. |
| `Doctor` | The line added to `doctor`. |

An invalid registration (no name, no constructor, a name or block
already taken) is left out and named in a warning at start.

## Lifecycle

The agent builds every extension after its own services and appends them
to the service set: an extension starts after the outputs, the probes,
the status channel and entity detection, and stops before them.

`Start` must not block and must not fail on a configuration problem: an
error from `Start` stops the whole agent. An extension that finds its
block invalid logs it and stays idle.

## What the Host gives

| Method | Returns |
|---|---|
| `Logger()` | The agent's logger; derive a module logger from it. |
| `ConfigBlock(name)` | A copy of the claimed block as a map, `${env:}`, `${file:}` and `${secret:}` references resolved. A key no extension claims is not loaded. |
| `OnConfigChange(fn)` | Calls `fn` after a reload that changed something, a change confined to an extension block included. |
| `Identity()` | `service.instance.id`, version, edition (`enterprise` when the build carries a paid probe, `oss` otherwise), licence tier (`free` without a valid licence), and the `jti` and expiry of the licence in force. |
| `HostAttributes()` | The host resource attributes the OTLP output attaches to its resource: same function, same `host.id`. |
| `Authorized(feature)` | Whether the licence grants the feature: true when its `authorized_probes` lists the name or the wildcard `*`. The check is live, so a licence change applies without a restart. |
| `Health()` | Root `Healthy`, the agent start time, each configured probe (enabled, running, health, last error, start time, last cycle) and each output (failure reason and detail, failing exports, last export error), from the state `status` and `doctor` read. |
| `EffectiveConfig()` | The configuration file by file (`agent.yaml`, each `probes.d/` and `strategies.d/` fragment, the single file of a monolithic layout, and `environment` for the probes `SENHUB_PROBE_*` declares), each redacted as `config show --redact` redacts it. |
| `PublishStatus(map)` | Replaces what the status channel carries for this extension. It is cleared when the extension stops. |
| `ReloadConfig()` | Reads the configuration again, overlays included, and applies it as a change on disk would. On error the configuration in force is kept. Observers are notified asynchronously, so a probe has not necessarily restarted when it returns. |
| `SetLogLevel(level)` | One level, `debug`, `info`, `warn` or `error`, for every module logger. An empty level puts back the levels in force before. |

`extension.RedactSecrets(s)` masks the credentials a free-form text can
carry (a URI password, a DSN password) before the text leaves the
process.

## Configuration

The claimed block sits at the top level of `agent.yaml` (or of the single
file of a monolithic layout):

```yaml
example:
  enabled: true
  token: ${secret:example-token}
```

It is shown by `config show` and by the per-file view with the usual
redaction: a key whose name looks like a secret, or a value that came
from `${file:}` or `${secret:}`, is masked.

## Overlay

`Overlay(target)` runs each time the running agent loads its
configuration, after the files are read and before the result is
validated and published. It may turn a probe on or off, replace its
`params.interval`, and set the entity detection switches; nothing else is
reachable, and the files are never rewritten. A loaded configuration that
fails validation is not published, so the one in force stays.

A probe whose name and parameters are unchanged keeps running; changing
its interval restarts that probe alone, and it collects at once. The
CLI (`config show`, `config check`) reads the files only: an overlay
lives in the running agent.

To change what the overlay applies, update the extension's own state and
call `Host.ReloadConfig()`. Calling it from `New` applies a stored overlay
before the first probe starts.

## config check and doctor

`Check(block, present)` returns lines of level `CheckOK`, `CheckOff`,
`CheckWarn` or `CheckError`; an error counts in the check's error total,
a warning in its warnings.

`doctor` runs in its own process. `Doctor(in)` receives the block, whether
the running agent answered, and what the extension published there with
`PublishStatus`, and returns one line of level `DoctorOK`, `DoctorWarn`,
`DoctorFail` or `DoctorSkip`.

## Self-metrics

An extension adds records to the agent's own metrics with
`exporthook.RegisterSelfRecordProvider` (`senhub-agent.go/probesdk/exporthook`).
The provider runs at each export of the agent's self-metrics; it returns
no record while the extension is off, so a disabled extension carries no
series.
