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
- Clean major-upgrade and clean uninstall. Data under
  `%ProgramData%\SenHub\` is intentionally preserved on uninstall unless
  you opt into a purge with `PURGE_DATA=1` (see
  [Uninstall and data purge](#uninstall-and-data-purge)).

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
| `PURGE_DATA` | Uninstall only — `PURGE_DATA=1` on `msiexec /x` deletes `%ProgramData%\SenHub\` in full (see [Uninstall and data purge](#uninstall-and-data-purge)) |

Install-time properties are consumed only on first install; they do not
overwrite an existing `agent.yaml`.

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

## Uninstall and data purge

Silent uninstall:

```bat
msiexec /x senhub-agent-<version>-amd64.msi /qn
```

By default, uninstalling removes what the MSI installed (the binary, the
`senhub-agent` service and the registry marker) plus the two transient
subfolders `%ProgramData%\SenHub\logs\` and `%ProgramData%\SenHub\update\`
(rotated logs and staged auto-update packages — both regenerated on the
next run), so a plain uninstall leaves a clean tree. Operator state under
`%ProgramData%\SenHub\` — configuration (`agent.yaml`, `probes.d\`,
`strategies.d\`), the sealed secret store and the license — is **kept**.
This is deliberate: a later reinstall or an upgrade picks the existing
configuration back up with no data loss. An in-place major upgrade never
removes the transient folders either, so a staged auto-update in progress
survives the upgrade.

To remove the machine's agent data as well, opt in with `PURGE_DATA=1`:

```bat
msiexec /x senhub-agent-<version>-amd64.msi /qn PURGE_DATA=1
```

This deletes the entire `%ProgramData%\SenHub\` tree, including the
sealed secret store and the license. It is not recoverable; use it when
decommissioning a host for good. The purge acts only on a real uninstall
— a major upgrade (a newer MSI replacing an older one) never touches the
data tree, with or without the property. An interactive uninstall from
**Apps & features** always keeps the data (there is no way to pass the
property there); run the `msiexec /x` command above instead.

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

The MSI is signed with [`jsign`](https://ebourg.github.io/jsign/) using a
**European CA — Certum** code-signing certificate (OV, eIDAS), whose private
key lives in Certum's cloud HSM (SimplySign) and is reached through the
SimplySign PKCS#11 module. Authorisation is the **open SimplySign Desktop
session** (login + OTP, 2-hour window) — this Code Signing token has **no
card PIN**, so jsign runs without `--storepass`.

### Why signing is a local step, not CI

The SimplySign session cannot exist on an ephemeral GitHub-hosted runner, and
we deliberately do **not** put the signing credential into CI secrets nor run
workflow code on the signing machine. Instead the release pipeline builds the
**unsigned** MSI and publishes the other artifacts; a maintainer signs the MSI
locally, where the SimplySign session lives, and uploads the signed MSI plus
its `.msi.minisig`. No signing secret ever touches CI, and there is no
self-hosted runner to maintain.

### Sign a release MSI

Prerequisites on the signing machine (macOS): **SimplySign Desktop** installed
with an **open session**, **SimplySign Mobile** as the OTP source (runs on
Apple-Silicon Macs, so no phone is required), **Temurin 17** (jsign + PKCS#11
needs Java ≤ 17 here), and `opensc` + `osslsigncode`
(`brew install opensc osslsigncode`) for read-only token inspection and local
verification.

```bash
# open the SimplySign Desktop session first, then:
packaging/windows/sign-release-msi.sh dist/senhub-agent-<version>-amd64.msi
```

The script is defensive: it pins the jsign jar by SHA-256, confirms the
session token is reachable **read-only** (never consuming a PIN attempt),
auto-discovers the certificate alias, signs with an RFC 3161 timestamp
(`http://time.certum.pl`), and verifies the embedded digest locally. It writes
a `-signed.msi` copy by default (pass `--in-place` to sign the file directly).

> On macOS `osslsigncode verify` may print `unable to get local issuer
> certificate`: that is only the Certum root missing from the OS trust store,
> **not** a signing defect. The authoritative check is on Windows (below).

### Verify a signed MSI

```powershell
Get-AuthenticodeSignature .\senhub-agent-<version>-amd64.msi | Format-List
```

Status `Valid` with the `SENSOR FACTORY SAS` publisher confirms the signature.

## Known limitations / follow-ups

- The in-wizard fields for `LICENSE_KEY` / `TAGS` / `OTLP_ENDPOINT` (a
  custom dialog) are a follow-up pending interactive validation; today the
  guided install lands the offline default and those are set via properties
  (silent install / MST).
- The release pipeline still publishes the **unsigned** MSI; wiring it to
  build unsigned + accept the locally-signed MSI (and its `.msi.minisig`) for
  auto-update is the remaining step (issue #608).
- 32-bit / ARM64 Windows are out of scope (amd64 only, per the
  distributed-binaries matrix).
