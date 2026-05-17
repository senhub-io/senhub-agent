---
title: Go style — formatting, imports, errors, naming, logging
---

## Formatting

- `gofmt` is enforced by a pre-commit hook. Code that isn't `gofmt`'d will not commit.
- Toolchain pin: see `go.mod`'s `go` directive (currently `1.26.3`). Don't bump without confirmation.

## Imports

Three groups separated by a single blank line, in this order:

1. **Standard library**
2. **Third-party** (anything not in `senhub-agent.go/...`)
3. **Internal** (`senhub-agent.go/internal/...`)

Example:

```go
import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
)
```

## Naming

- Exported: `PascalCase` — methods, types, constants, exported variables.
- Unexported: `camelCase`.
- Acronyms keep their case (HTTPHandler, OTLPStrategy, URLParser).
- Probe types live in lowercase package names (`mysql`, `citrix`, `netscaler`).

## Error handling

- Always wrap with context using `%w`:

  ```go
  if err != nil {
      return fmt.Errorf("loading config from %s: %w", path, err)
  }
  ```

- Never swallow errors silently. If an error is intentionally non-fatal, log it at `Warn` with context — never discard it without a trace.
- Use the `sanitize` helpers (`sanitize.CountInt32`, `sanitize.Bytes`, `sanitize.Duration`, `sanitize.EnumValue`) when converting external values to the probe's float32 metric type. They return `(value, ok)` so the caller decides emit vs skip.

## Logging

- Use a **module-specific logger** for every package:

  ```go
  moduleLogger := logger.NewModuleLogger(baseLogger, "probe.mysql")
  moduleLogger.Info().Str("host", cfg.Host).Msg("mysql probe connected")
  ```

- Structured fields only. No `fmt.Sprintf` in log messages.
- Log levels:
  - `Debug` — verbose internals, off by default
  - `Info` — boot, shutdown, significant transitions
  - `Warn` — degraded operation, recoverable failure
  - `Error` — unrecoverable failure for one cycle/operation
  - `Fatal` — agent must stop (rare; almost always prefer return-an-error)

## Comments

- Default: write no comments. Identifier names should carry the meaning.
- Comments only when the WHY is non-obvious — a hidden constraint, an invariant a future reader would miss, a workaround for a specific upstream bug (with a reference).
- Never reference the current task, PR, or caller in comments — that belongs in the commit message and rots with the code.
- Public types and functions get a doc comment when their contract isn't obvious from the signature.

## What NOT to do

- No `_ = err` to silence errors.
- No `panic()` outside of unit tests and `main.go` privilege checks.
- No package-level mutable state that isn't explicitly synchronized.
- No `init()` functions that do work — initialization belongs in constructors.
