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
| `LICENSEDIR` | Folder holding the licence file; what the wizard browses to. `license.jwt` or the single `*.jwt` inside is installed; a missing or empty folder means Free tier, several files are refused. Local or UNC path (the seeding step runs as SYSTEM and cannot see mapped drives) |
| `LICENSE_FILE` | Path to the licence file itself, for scripted installs |
| `LICENSE_KEY` | The licence token itself, for scripted installs (see the note on log exposure below) |
| `TAGS` | Comma-separated `k=v` list applied as host `global_tags` (e.g. `site=paris,env=prod`) |
| `OTLP_ENDPOINT` | Optional collector `host:port` — writes an OTLP push strategy (`strategies.d\10-otlp.yaml`) |
| `HTTP_PORT` | Port of the local HTTP endpoints, PRTG / Web UI / Nagios (default `8080`) |
| `DESKTOP_SHORTCUT` | `1` (default) places a "SenHub Agent Console" shortcut on the desktop; `DESKTOP_SHORTCUT=0` skips it |
| `INSTALLFOLDER` | Override the install directory (default `%ProgramFiles%\SenHub Agent\`) |
| `PURGE_DATA` | Uninstall only — `PURGE_DATA=1` on `msiexec /x` deletes `%ProgramData%\SenHub\` in full (see [Uninstall and data purge](#uninstall-and-data-purge)) |

Install-time properties are consumed only on first install; they do not
overwrite an existing `agent.yaml`.

The seeding step checks that the HTTP port is free before writing the
configuration. A port already in use, or an invalid property value, makes
the seeding fail and the install roll back; the reason is in the install
log (`SeedConfig`). Rerun with `HTTP_PORT=<free port>`. Without this check
the install used to exit 0 with a running service that answered on
nothing, its only trace being one `binding HTTP server` line in
`%ProgramData%\SenHub\logs`.

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

## Guided install

Double-clicking the MSI runs the standard wizard with one extra page,
after the install directory:

- **Licence folder**: browse to the folder holding the `.jwt` file
  received from Sensor Factory (the installer has a folder picker, not a
  file picker). Leave the default for the Free tier; a licence can be
  added later from the web console. A customer licence is the same file
  for every agent of that customer.
- **Port**: the port of the web console and of the PRTG / Nagios
  endpoints, `8080` by default. The field takes integers only and the
  wizard refuses a value outside 1 to 65535 before anything is
  installed. A port already in use is refused when the configuration is
  seeded, and the install fails with the reason.
- **Desktop shortcut**: checked by default, creates "SenHub Agent
  Console" on the desktop with the agent icon.
- **Open the web console when setup completes**: checked by default;
  the browser opens on the console when the wizard closes.

The console address ends with the agent key, generated on the machine
and kept sealed, so it is not printed by the wizard: the shortcut, the
end of the wizard and `senhub-agent console` open it, and
`senhub-agent console --print` (as administrator) prints it.

The shortcut runs `senhub-agent.exe console`. It carries no key: the
agent reads the sealed key, asks for elevation once if the user is not
already elevated, then opens the default browser with the user's own
rights. It is removed with the product.

## Silent install

```bat
msiexec /i senhub-agent-<version>-amd64.msi /qn ^
  LICENSE_KEY=eyJhbGciOi... ^
  TAGS=site=paris,env=prod ^
  HTTP_PORT=9080 DESKTOP_SHORTCUT=0 ^
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

Uninstalling removes what the MSI installed (the binary, the
`senhub-agent` service and the registry marker) **and the whole
`%ProgramData%\SenHub\` tree** — configuration (`agent.yaml`,
`probes.d\`, `strategies.d\`), the sealed secret store, the license and
the transient `logs\` and `update\` subfolders. Removing the product
leaves the machine clean, and a later fresh install starts from the
installer's inputs (licence, port) rather than a stale kept
configuration.

This holds for an interactive uninstall from **Apps & features** as well
as `msiexec /x`. It is not recoverable, so a host that will be
reinstalled and must keep its licence should be **upgraded in place**
(install the newer MSI over the older one) rather than uninstalled and
reinstalled.

An in-place major upgrade preserves everything under
`%ProgramData%\SenHub\`: a newer MSI replacing an older one never
touches the data tree, so configuration, licence, secret store and any
staged auto-update survive the upgrade.

`PURGE_DATA` is accepted for backward compatibility but no longer changes
anything: a genuine uninstall already removes the full tree.

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

### Sign the release EXEs (#622)

The bare `senhub-agent.exe` inside the two Windows ZIPs (full + oss) is
signed with the same certificate through the same local session. The
signing round is a deliberate two-half flow so the minisign release key
never leaves CI and the Authenticode credential never enters it:

```bash
# 1. after the release workflow has published the ZIPs, sign both exes
#    locally and re-upload the ZIPs (SimplySign session open):
packaging/windows/sign-windows-release-exe.sh <version>

# 2. verify + re-minisign the ZIPs and rebuild the MSI so it embeds the
#    SIGNED exe (unsigned MSI kept as a build artifact):
gh workflow run publish-signed-exe.yml \
  --repo senhub-io/senhub-agent-enterprise -f tag=<version>

# 3. download the MSI artifact, sign it, upload it, publish:
packaging/windows/sign-release-msi.sh senhub-agent-<version>-amd64.msi --in-place
gh release upload <version> senhub-agent-<version>-amd64.msi --repo senhub-io/senhub-agent
gh workflow run publish-signed-msi.yml \
  --repo senhub-io/senhub-agent-enterprise -f tag=<version>
```

Between step 1 and step 2 the ZIPs' detached `.minisig` assets are stale;
auto-updating agents fail safe (they refuse the archive and retry on the
next check), so run the two steps back to back. One SimplySign session
(2-hour window) comfortably covers the whole sequence.

### Verify a signed MSI or EXE

```powershell
Get-AuthenticodeSignature .\senhub-agent-<version>-amd64.msi | Format-List
Get-AuthenticodeSignature .\senhub-agent.exe | Format-List
```

Status `Valid` with the `SENSOR FACTORY SAS` publisher confirms the signature.

## Known limitations / follow-ups

- The in-wizard fields for `LICENSE_KEY` / `TAGS` / `OTLP_ENDPOINT` (a
  custom dialog) are a follow-up pending interactive validation; today the
  guided install lands the offline default and those are set via properties
  (silent install / MST).
- Beta releases ship with unsigned exes by default (the signing round is a
  local, per-release step); run `sign-windows-release-exe.sh <X.Y.Z-beta>` +
  `publish-signed-exe.yml` on a beta tag when a signed beta matters.
- 32-bit / ARM64 Windows are out of scope (amd64 only, per the
  distributed-binaries matrix).
