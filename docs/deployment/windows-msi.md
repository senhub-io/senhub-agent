# Windows MSI deployment (GPO / SCCM / Intune)

This guide covers mass deployment of the SenHub Agent on Windows
workstations and Citrix VDA hosts using the signed MSI installer.

> Status: the MSI packaging chain is scaffolded (WiX v4 source +
> CI workflow). The installer has **not yet been validated on real
> Windows hosts** and the production code-signing certificate is not
> yet provisioned. See [Known limitations](#known-limitations).

## What the MSI does

- Installs `senhub-agent.exe` into `%ProgramFiles%\SenHub Agent\`.
- Registers and starts the `senhub-agent` Windows service
  (display name **SenHub Agent**), matching the service created by
  `senhub-agent install`.
- Seeds `%ProgramData%\SenHub\agent.yaml` from MSI properties on first
  install only — an upgrade or reinstall preserves an existing config.
- Supports silent install (`/qn`), clean upgrade, and clean uninstall.

## Build

The MSI is built from the existing Windows binary by the
[`windows-msi.yml`](../../.github/workflows/windows-msi.yml) workflow
(WiX Toolset v4 via the `wix` dotnet tool). It runs on a Linux runner
and signs cross-platform with `osslsigncode`.

To build locally (requires the `wix` dotnet tool and a staged binary):

```bash
# 1. build the Windows binary
make build-windows           # -> dist/windows-amd64/senhub-agent.exe

# 2. install WiX v4 + the Util extension
dotnet tool install --global wix --version '4.*'
wix extension add -g WixToolset.Util.wixext

# 3. build the MSI
wix build packaging/windows/senhub-agent.wxs \
  -d Version=0.2.0 \
  -d BinDir=dist/windows-amd64 \
  -arch x64 \
  -ext WixToolset.Util.wixext \
  -out dist/senhub-agent-0.2.0-amd64.msi
```

## MSI properties (parametric configuration)

Public properties can be set on the `msiexec` command line for silent
deployment:

| Property | Default | Purpose |
|---|---|---|
| `BACKEND_URL` | `https://eu-west-1.intake.senhub.io` | SenHub intake endpoint |
| `AGENT_TOKEN` | _(empty)_ | Enrollment token, written to the seeded config |

Properties are read only on first install; they do not overwrite an
existing `agent.yaml`.

## Silent install

```bat
msiexec /i senhub-agent-0.2.0-amd64.msi /qn ^
  BACKEND_URL=https://eu-west-1.intake.senhub.io ^
  AGENT_TOKEN=xxxxxxxxxxxxxxxx ^
  /l*v %TEMP%\senhub-agent-install.log
```

Silent uninstall:

```bat
msiexec /x senhub-agent-0.2.0-amd64.msi /qn
```

## GPO (Active Directory) deployment

1. Copy the MSI to a UNC share readable by the target computer
   accounts (e.g. `\\dc01\software\senhub-agent-0.2.0-amd64.msi`).
2. In **Group Policy Management**, create or edit a GPO linked to the
   target OU.
3. **Computer Configuration → Policies → Software Settings → Software
   installation → New → Package**. Select the MSI via its UNC path.
4. Choose **Assigned** (installs at next boot, per-machine).
5. To pass `BACKEND_URL` / `AGENT_TOKEN`, create an MST transform (see
   [Transforms](#transforms-mst)) — GPO software installation cannot
   pass `msiexec` properties directly.

GPO installs run as `SYSTEM` at boot, which matches the MSI's
`perMachine` scope and the service's `LocalSystem` account.

## SCCM / Microsoft Endpoint Configuration Manager

Create an **Application** with a **Windows Installer (*.msi)**
deployment type, or a **Package/Program** with:

- Install: `msiexec /i senhub-agent-0.2.0-amd64.msi /qn BACKEND_URL=... AGENT_TOKEN=...`
- Uninstall: `msiexec /x {ProductCode} /qn`
- Detection: MSI product code, or registry
  `HKLM\SOFTWARE\Sensor Factory\SenHub Agent\Version`.

## Microsoft Intune

1. Wrap the MSI as a **Line-of-business app** (native `.msi` support) or
   convert to `.intunewin` with the Win32 Content Prep Tool.
2. Install command:
   `msiexec /i senhub-agent-0.2.0-amd64.msi /qn BACKEND_URL=... AGENT_TOKEN=...`
3. Uninstall command: `msiexec /x {ProductCode} /qn`
4. Detection rule: registry key
   `HKLM\SOFTWARE\Sensor Factory\SenHub Agent` value `Version`.

## Transforms (MST)

For GPO (which cannot pass properties), generate an MST that sets
`BACKEND_URL` / `AGENT_TOKEN`, e.g. with Orca or `WiX` tooling, and
attach it to the GPO package under **Modifications**.

## Code signing

The MSI must be signed with the **Sensor Factory code-signing
certificate** so Windows SmartScreen does not warn on execution.

- CI signs with `osslsigncode` (cross-platform) when the
  `WINDOWS_CODESIGN_PFX_BASE64` / `WINDOWS_CODESIGN_PFX_PASSWORD`
  secrets are present.
- Without those secrets the workflow produces an **unsigned** MSI and
  logs a warning — usable for layout/install testing only.
- For OV/EV certificates stored in an HSM or Azure Key Vault, switch
  the sign step to `jsign`.

## Config seeding

The `SeedConfig` custom action invokes the installed binary to generate
the config from the MSI properties. The exact subcommand
(`config generate ...`) is a **placeholder** in
`packaging/windows/senhub-agent.wxs` and must be confirmed against the
agent CLI on a real Windows host before the first signed release.

## Known limitations

- Not yet tested on Windows 10 22H2 / 11 23H2 / Server 2019 / 2022.
- Production code-signing certificate not yet provisioned.
- The `SeedConfig` custom action subcommand is unverified.
- 32-bit / ARM64 Windows are out of scope (amd64 only, per the
  distributed-binaries matrix).
