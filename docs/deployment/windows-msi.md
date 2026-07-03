# Windows MSI deployment (GPO / SCCM / Intune)

This guide covers deployment of the SenHub Agent on Windows workstations,
servers and Citrix VDA hosts using the MSI installer — interactively
(guided wizard) or unattended (silent, GPO, SCCM, Intune).

> The agent is **offline by default**: it exposes local scrape endpoints
> (PRTG / Nagios / Prometheus) and does not push anywhere. There is no
> cloud backend or enrollment token to provide. The only provisionable
> fields are the **license** (unlocks paid probe tiers — Free needs none)
> and host **tags**; optionally an **OTLP endpoint** to push to a
> collector.

## What the MSI does

- Installs `senhub-agent.exe` into `%ProgramFiles%\SenHub Agent\`.
- Registers and starts the `senhub-agent` Windows service
  (display name **SenHub Agent**, `LocalSystem`), with the same restart-on-failure
  recovery policy as `senhub-agent install`.
- On first install, runs `senhub-agent config init` to write the default
  configuration under `%ProgramData%\SenHub\` (multi-file layout:
  `agent.yaml` + `probes.d\` + `strategies.d\`), applying any provided
  license / tags / OTLP endpoint. **Idempotent** — an upgrade or reinstall
  never overwrites an existing config.
- Interactive install shows a **guided wizard** (Welcome → license →
  install directory → ready → progress → finish). A silent install (`/qn`)
  skips the UI and drives the same properties from the command line.
- Clean major-upgrade and clean uninstall (operator config under
  `%ProgramData%\SenHub\` is intentionally preserved on uninstall).

## MSI properties (parametric configuration)

Public properties can be set on the `msiexec` command line (or via an MST
for GPO) for unattended deployment. All are optional; with none set the
agent installs in the offline Free-tier default.

| Property | Purpose |
|---|---|
| `LICENSE_KEY` | JWT license token — unlocks Pro/Enterprise probes (see the note on log exposure below) |
| `TAGS` | Comma-separated `k=v` list applied as host `global_tags` (e.g. `site=paris,env=prod`) |
| `OTLP_ENDPOINT` | Optional collector `host:port` — writes an OTLP push strategy (`strategies.d\10-otlp.yaml`) |
| `INSTALLFOLDER` | Override the install directory (default `%ProgramFiles%\SenHub Agent\`) |

Properties are consumed only on first install; they do not overwrite an
existing `agent.yaml`.

> **`LICENSE_KEY` and install logs.** The license token is a secret. A
> verbose install log (`/l*v`) records public property values and custom
> action command lines, so a `LICENSE_KEY` passed on the `msiexec` line
> can appear in that log. When you provision a license silently:
>
> - Prefer a non-verbose log level, or omit `/l*v` entirely, for the
>   install that carries `LICENSE_KEY`.
> - If you must capture a verbose log (troubleshooting), treat it as
>   sensitive and delete it once the install is confirmed — it may also
>   contain the token on the command line passed to the provisioning
>   custom action.
> - The token equally appears in the shell history / job output of the
>   deployment tool that invokes `msiexec`; scrub those the same way.

## Silent install

```bat
msiexec /i senhub-agent-<version>-amd64.msi /qn ^
  LICENSE_KEY=eyJhbGciOi... ^
  TAGS=site=paris,env=prod ^
  /l* %TEMP%\senhub-agent-install.log
```

`/l*` logs everything except the verbose (`v`) level; the verbose level
is what records property values, so it is deliberately omitted here while
`LICENSE_KEY` is on the command line. Use `/l*v` only for an install that
carries no secret, and delete the log afterwards (see the note above).

Free tier, no provisioning:

```bat
msiexec /i senhub-agent-<version>-amd64.msi /qn
```

Silent uninstall:

```bat
msiexec /x senhub-agent-<version>-amd64.msi /qn
```

## Existing installations

The MSI handles a machine that already has an agent:

- **Installed by this MSI** — a newer MSI performs a clean major-upgrade
  (config preserved); the same version opens the standard maintenance
  experience (Change / Repair / Remove) interactively, or
  `msiexec /f` (repair) / `/x` (remove) silently.
- **Installed outside the MSI** (a `senhub-agent install`, ZIP or
  auto-update deploy) — the installer **detects the foreign service** and,
  by default, stops with a clear message rather than colliding on service
  creation. To take it over, pass `ADOPT=1`:

  ```bat
  msiexec /i senhub-agent-<version>-amd64.msi /qn ADOPT=1 /l*v %TEMP%\senhub-adopt.log
  ```

  `ADOPT=1` stops and deletes the existing service, then installs the
  MSI-managed one. Configuration under `%ProgramData%\SenHub\` is
  preserved (`config init` is idempotent). Migrating a fleet from a
  script/auto-update install to MSI management is therefore a single
  `ADOPT=1` install.

  > If the old service does not release immediately, Windows may report it
  > as "marked for deletion" and the new service install completes after a
  > reboot. Prefer stopping the old agent before an `ADOPT=1` migration on
  > busy hosts.

## GPO (Active Directory) deployment

1. Copy the MSI to a UNC share readable by the target computer accounts
   (e.g. `\\dc01\software\senhub-agent-<version>-amd64.msi`).
2. In **Group Policy Management**, edit a GPO linked to the target OU.
3. **Computer Configuration → Policies → Software Settings → Software
   installation → New → Package**; select the MSI via its UNC path.
4. Choose **Assigned** (installs at next boot, per-machine).
5. To pass `LICENSE_KEY` / `TAGS`, attach an MST transform (see
   [Transforms](#transforms-mst)) — GPO software installation cannot pass
   `msiexec` properties directly.

GPO installs run as `SYSTEM` at boot, matching the MSI's `perMachine`
scope and the service's `LocalSystem` account.

## SCCM / Microsoft Endpoint Configuration Manager

Create an **Application** with a **Windows Installer (*.msi)** deployment
type, or a Package/Program with:

- Install: `msiexec /i senhub-agent-<version>-amd64.msi /qn LICENSE_KEY=... TAGS=...`
- Uninstall: `msiexec /x {ProductCode} /qn`
- Detection: MSI product code, or registry
  `HKLM\SOFTWARE\Sensor Factory\SenHub Agent\Version`.

## Microsoft Intune

1. Wrap the MSI as a **Line-of-business app** or convert to `.intunewin`
   with the Win32 Content Prep Tool.
2. Install: `msiexec /i senhub-agent-<version>-amd64.msi /qn LICENSE_KEY=... TAGS=...`
3. Uninstall: `msiexec /x {ProductCode} /qn`
4. Detection rule: registry key `HKLM\SOFTWARE\Sensor Factory\SenHub Agent`
   value `Version`.

## Transforms (MST)

For GPO (which cannot pass properties), generate an MST that sets
`LICENSE_KEY` / `TAGS` (e.g. with Orca) and attach it to the GPO package
under **Modifications**.

## Updates

An MSI-managed install does **not** self-replace its binary (that would drift
from Windows Installer tracking — a repair could revert it, and ARP would show
the wrong version). Auto-update stays automatic but **applies a new signed MSI**
instead: when an update is available the agent downloads
`senhub-agent-<version>-amd64.msi`, verifies its signature, and runs
`msiexec /i /qn` (a clean MajorUpgrade that preserves `%ProgramData%\SenHub`).

This is enabled automatically — the agent detects the MSI registry marker and
switches update strategy. A non-MSI install (ZIP / script) keeps the binary
self-replace flow unchanged. Alternatively, disable `auto_update` and push new
MSIs through your management tool (Intune/SCCM/WSUS).

> Requires the release channel to publish the MSI and its `.msi.minisig`
> alongside the ZIP (see issue for the release-pipeline wiring).

## Build

The MSI is built from the Windows binary by the
[`windows-msi.yml`](../../.github/workflows/windows-msi.yml) workflow
(WiX Toolset v4 via the `wix` dotnet tool, on a Linux runner).

Locally (requires the `wix` dotnet tool and a staged binary):

```bash
make build-windows                                  # -> dist/windows-amd64/senhub-agent.exe
dotnet tool install --global wix --version 5.0.2
# Pin extensions to the SAME version as the tool (unpinned pulls a
# newer major the tool cannot load; WiX 4 also mis-validates Directory
# names on Linux, so 5.x is the floor for CI builds).
wix extension add -g WixToolset.Util.wixext/5.0.2
wix extension add -g WixToolset.UI.wixext/5.0.2
make package-windows-msi                            # -> dist/senhub-agent-<version>-amd64.msi
```

## Code signing

Signing uses [`jsign`](https://ebourg.github.io/jsign/) (one tool across
every key store, so it works on the Linux CI runner). The certificate is a
**European CA — Certum** (OV, eIDAS), held on a cloud HSM (SimplySign) and
addressed through jsign's `--storetype`. The agent `.exe` is signed before
it is packaged, then the `.msi`, then any `.ps1`.

Provide these repository **secrets** to enable signing (unset ⇒ the
workflow still builds an **unsigned** MSI + warning, for layout testing):

| Secret | Value (Certum SimplySign) |
|---|---|
| `CODESIGN_STORETYPE` | `PKCS11` |
| `CODESIGN_KEYSTORE` | path to the SimplySign PKCS#11 `.cfg` |
| `CODESIGN_STOREPASS` | session PIN |
| `CODESIGN_ALIAS` | certificate label |

Optional repo variables: `CODESIGN_TSA` (defaults to
`http://time.certum.pl`), `JSIGN_SHA256` (pins the jsign jar digest).

> **CI runner note.** Certum SimplySign opens a 2-hour signing session via
> a mobile TOTP app, which cannot run on an ephemeral GitHub-hosted runner.
> Sign on a **self-hosted runner** with SimplySign Desktop and a session
> opened at release time (semi-attended), which fits a tag-based cadence.

### Verify a signed MSI

```powershell
Get-AuthenticodeSignature .\senhub-agent-<version>-amd64.msi | Format-List
```

Status `Valid` with the Sensor Factory publisher confirms the signature.

## Known limitations / follow-ups

- The in-wizard fields for `LICENSE_KEY` / `TAGS` / `OTLP_ENDPOINT` (a
  custom dialog) are a follow-up pending interactive validation; today the
  guided install lands the offline default and those are set via properties
  (silent install / MST).
- Production Certum certificate not yet provisioned → releases ship an
  unsigned MSI until the signing secrets are set (issue #153).
- 32-bit / ARM64 Windows are out of scope (amd64 only, per the
  distributed-binaries matrix).
