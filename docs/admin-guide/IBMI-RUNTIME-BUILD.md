# Building the IBM i Native Runner

The `ibmi` probe needs either a JRE on the host OR a self-contained native binary (`jt400runner`). This page describes the native-binary build process — operators don't normally run it; it's automated via the `Native Runner Build` GitHub Actions workflow.

## What the workflow produces

`Native Runner Build` (`.github/workflows/native-runner-build.yml`) cross-compiles the Java `Jt400Runner` entry point against `jt400.jar` using GraalVM native-image, one job per platform:

| Target | Runner | Artifact name |
|---|---|---|
| Linux amd64 | `ubuntu-latest` | `jt400runner-linux-amd64` |
| Linux arm64 | `ubuntu-24.04-arm` | `jt400runner-linux-arm64` |
| macOS amd64 | `macos-13` | `jt400runner-darwin-amd64` |
| macOS arm64 | `macos-14` | `jt400runner-darwin-arm64` |
| Windows amd64 | `windows-latest` | `jt400runner-windows-amd64.exe` |

Each binary is a ~40–50 MB self-contained executable that speaks the same line-oriented JSON protocol over stdin/stdout as the JVM-launched runner — no class path, no JRE required at deploy time.

## Triggering the build

### Manual run

From the GitHub UI: **Actions → Native Runner Build → Run workflow → Branch: dev** (or any branch carrying the latest `Jt400Runner.java` + `jt400.jar`). Each job's artifact is downloadable from the run summary for 14 days.

### CLI

```bash
gh workflow run native-runner-build.yml --ref dev
```

Track:

```bash
gh run watch
gh run download <run-id>
```

## Consuming an artifact on a deployed agent

1. Download the matching artifact for the target host's OS/arch.
2. Place it next to the `senhub-agent` binary:

   | OS | Path |
   |---|---|
   | Linux | `<dir-of-senhub-agent>/bridge/jt400runner` |
   | macOS | same |
   | Windows | `<dir-of-senhub-agent>\bridge\jt400runner.exe` |

3. Mark it executable (Linux/macOS): `chmod +x bridge/jt400runner`.
4. Confirm the agent finds it: `senhub-agent ibmi check`.

The resolver picks the sibling binary automatically when no `bridge.native_runner` is set in the probe YAML — no config change needed. To override, set `native_runner: /custom/path` in `probes.d/<probe>.yaml`.

## Build assumptions and risk

- **GraalVM JDK 21 community edition** is provisioned by `graalvm/setup-graalvm@v1`.
- `--no-fallback` forbids any runtime JVM dependency. If JT400 hits a reflection path the build configuration didn't anticipate, the resulting binary fails at runtime with `ClassNotFoundException`; if static analysis catches it, the build fails. Either way, the smoke check step in the workflow (`./jt400runner </dev/null`) flags missing-runtime crashes.
- A failed build on one platform does not cancel the others (`fail-fast: false`) — the workflow surface every failure to keep iteration cycles short.

If a build starts failing after a JT400 upgrade, add reflection hints under `META-INF/native-image/<group>/<artifact>/reflect-config.json` (in the classpath) or run the GraalVM tracing agent against a representative query to generate one. We deliberately ship no pre-baked reflect-config today — JT400's JDBC entry point is reflection-light and `--no-fallback` has been observed to succeed without one.

## Release integration

`dev-beta-release.yml` and `master-release.yml` invoke `native-runner-build.yml` as a reusable workflow on every tag push. The release job waits on the native build (`needs: build_native_runners`) and attaches the resulting binaries to the GitHub Release alongside the agent binary:

| Asset | Source |
|---|---|
| `jt400runner-linux-amd64` | linux-amd64 native build job |
| `jt400runner-linux-arm64` | linux-arm64 native build job |
| `jt400runner-windows-amd64.exe` | windows-amd64 native build job |

The reusable workflow is also still triggerable manually (`workflow_dispatch`) for iteration between releases. Manual runs upload artifacts on the workflow run; release runs upload them on the Release itself.

If one of the matrix jobs fails (e.g. a JT400 upgrade introduces a new reflection path), the release proceeds without that platform — `continue-on-error: true` on the download step prevents a missing artifact from blocking the entire release. The release notes commit step should reference the gap so operators know to redeploy when the build is fixed.

### Reflection / resource-bundle inclusion

The build is **not** vanilla `native-image -cp jt400.jar:. Jt400Runner`. JT400 uses Java reflection (Class.forName for impl class loading) and ListResourceBundle for i18n; native-image strips those by default. The workflow handles both:

- `IncludeResourceBundles=` enumerates every MRI*-named class under `com/ibm/as400/*` (excluding `vaccess/*` UI bundles).
- A generated `reflect-config.json` lists every `.class` under `com/ibm/as400/*` with `allDeclaredConstructors|Methods|Fields|Classes` so JDBC's dynamic impl lookups succeed at runtime.

These flags inflate the binary from ~6 MB (raw build, fails at runtime) to ~17 MB (functional). Slimmer builds are possible — generate a precise `reflect-config.json` from a GraalVM tracing-agent run against the real IBM i — but adds CI complexity. The current size is acceptable for the use case.
