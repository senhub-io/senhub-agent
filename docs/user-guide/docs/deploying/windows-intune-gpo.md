# Windows: Intune, GPO and SCCM

The three tools install the same file, the signed MSI
`senhub-agent-<version>-amd64.msi`, and differ in how they hand it
properties. This page gives the command line once, then what each tool
adds. The full list of properties is in
[Silent / unattended install](../installation.md#silent-unattended-install).

## The command line

```bat
msiexec /i senhub-agent-0.6.1-amd64.msi /qn /norestart ^
  LICENSE_FILE=\\files01\deploy\senhub\license.jwt ^
  TAGS=site=paris,env=prod ^
  OTLP_ENDPOINT=collector.example.com:4317 ^
  HTTP_PORT=8080 DESKTOP_SHORTCUT=0 ^
  /l* %TEMP%\senhub-agent-install.log
```

Every property is optional. With none, the agent installs in the free-tier
default: local endpoints, no push. `ZABBIX_SERVER` and
`ZABBIX_HOST_METADATA` configure the Zabbix output the same way
`OTLP_ENDPOINT` configures OTLP.

The properties are read on the first install only. They never overwrite an
existing `agent.yaml`, so running the same command again on an installed
host changes nothing: that is what makes the line safe as an Intune or
Configuration Manager command, which both run again until the detection
rule is satisfied.

Exit codes are `msiexec`'s: `0` success, `3010` success with a reboot
pending (the agent needs none), `1603` a fatal error. A port already in use
makes the install roll back with a reason in the log (search `SeedConfig`);
rerun with another `HTTP_PORT`.

## Pinning

The file is the pin. Download the MSI of the version you chose and stage
that exact file in your tool; nothing in the MSI fetches a newer one.

```powershell
$version = "0.6.1"
$base = "https://github.com/senhub-io/senhub-agent/releases/download/$version"
Invoke-WebRequest "$base/senhub-agent-$version-amd64.msi" -OutFile ".\senhub-agent-$version-amd64.msi"
Get-AuthenticodeSignature ".\senhub-agent-$version-amd64.msi" | Format-List Status, SignerCertificate
```

`Status` must read `Valid` with `SENSOR FACTORY SAS` as the signer. Do the
check once on the build machine, before the file goes into the tool.

!!! note "Auto-update"
    `auto_update.enabled` is `false` unless set, so the version stays the one
    your tool installed. If you turn it on, an MSI install applies a newer
    signed MSI by itself and your pin no longer holds; see
    [Auto-Update](../configuration.md#auto-update).

## Licence

Give the licence as a **file**, with `LICENSE_FILE`, not as `LICENSE_KEY`.
A token on the command line is copied into the install log, the job output
of the deployment tool and its history; see the warning under
[the silent install](../installation.md#silent-unattended-install).

- **GPO and SCCM**: put `license.jwt` on a share and grant read to the
  computer accounts of the target machines (`Domain Computers`, or a group of
  them). The step that reads the file runs as `SYSTEM`, so it sees the
  network as the computer account and sees no mapped drive: use a UNC path.
- **Intune**: a machine that is only joined to Entra ID often cannot reach a
  file server. Ship the file inside the package instead, see below.

A licence is the same file for every agent of a customer. Without one the
free tier runs, and the licence can be added later (a file placed next to
`agent.yaml`, then a restart of the service).

## Secrets

The MSI takes no secret other than the licence. Credentials of probes (a
database password, an API token) go in a fragment of `probes.d` that refers
to a file or to the sealed store, never in the command line:

```yaml
- name: orders-db
  type: postgresql
  params:
    host: db.example.com
    username: monitor
    password: "${file:C:/ProgramData/SenHub/secrets/pg_password}"
```

Place the secret file with a second step of your tool (a script or a
configuration item), and restrict its ACL to `SYSTEM` and `Administrators`.
Alternatively, seal the value into the agent's own store with
`senhub-agent secret set <name> --from-file <path>` as `SYSTEM`, and refer
to it as `${secret:<name>}`; see the [secret store](../secret-store.md).

## Validation before apply

The MSI refuses a bad property before it installs anything: a port outside
1 to 65535, or already taken, stops the install and rolls it back.

For the configuration you add after the install, validate it on a test
machine with the same MSI first:

```powershell
& "C:\Program Files\SenHub Agent\senhub-agent.exe" config check --json
echo $LASTEXITCODE
```

Exit `0` means clean, `1` warnings only, `2` an error. Make your tool treat
`2` as a failed deployment and stop the rollout there. The check also takes
a path, so a script can validate a staged copy of the configuration before
it replaces the live one:

```powershell
$stage = Join-Path $env:TEMP "senhub-stage"
Copy-Item "C:\ProgramData\SenHub" $stage -Recurse -Force
Copy-Item .\50-orders-db.yaml "$stage\probes.d\"
& "C:\Program Files\SenHub Agent\senhub-agent.exe" config check "$stage\agent.yaml"
if ($LASTEXITCODE -eq 2) { throw "configuration refused" }
Copy-Item "$stage\probes.d\50-orders-db.yaml" "C:\ProgramData\SenHub\probes.d\"
```

## Group Policy

GPO software installation cannot pass properties. It applies a transform.

1. Copy the MSI to a share the computer accounts can read, for instance
   `\\files01\deploy\senhub\senhub-agent-0.6.1-amd64.msi`.
2. Build the transform. With Orca (Windows SDK), open the MSI,
   **Transform > New Transform**, add rows to the `Property` table for
   `LICENSE_FILE`, `TAGS`, `OTLP_ENDPOINT`, `HTTP_PORT`, then
   **Transform > Generate Transform** and save `senhub.mst` next to the MSI.
   A transform is safe to keep in version control: `LICENSE_FILE` holds a
   path, not the licence.
3. In Group Policy Management, edit a GPO linked to the target OU:
   **Computer Configuration > Policies > Software Settings > Software
   installation > New > Package**, choose the MSI by its UNC path, pick
   **Advanced**, then on the **Modifications** tab add `senhub.mst`.
4. Choose **Assigned**. The package installs at the next boot, as `SYSTEM`.

Scope the GPO to a test OU first and run `gpupdate /force` followed by a
restart on one machine.

## Configuration Manager (SCCM)

Create an **Application** with a **Windows Installer (\*.msi)** deployment
type, and replace the generated install command by the one above.

| Field | Value |
|---|---|
| Installation program | `msiexec /i "senhub-agent-0.6.1-amd64.msi" /qn /norestart LICENSE_FILE=\\files01\deploy\senhub\license.jwt TAGS=site=paris,env=prod` |
| Uninstall program | `msiexec /x "{ProductCode}" /qn /norestart` |
| Detection method | Windows Installer product code, or registry value `HKLM\SOFTWARE\Sensor Factory\SenHub Agent`, `Version`, equal to `0.6.1` |
| Install behavior | Install for system |

The product code changes with every version; read it from the MSI
(`Property` table, `ProductCode`) when you build the application.

## Intune

Intune runs the install command from the staged content of a Win32 app. Put
the MSI, the licence and a one-line wrapper in the source folder, and
package it with the
[Win32 Content Prep Tool](https://github.com/microsoft/Microsoft-Win32-Content-Prep-Tool).

`install.cmd`:

```bat
@echo off
msiexec /i "%~dp0senhub-agent-0.6.1-amd64.msi" /qn /norestart ^
  LICENSE_FILE="%~dp0license.jwt" ^
  TAGS=site=paris,env=prod ^
  /l* "%TEMP%\senhub-agent-install.log"
exit /b %ERRORLEVEL%
```

```powershell
IntuneWinAppUtil.exe -c .\source -s install.cmd -o .\out
```

| Field | Value |
|---|---|
| Install command | `cmd /c install.cmd` |
| Uninstall command | `msiexec /x "{ProductCode}" /qn /norestart` |
| Install behavior | System |
| Detection rule | Registry. Key `HKLM\SOFTWARE\Sensor Factory\SenHub Agent`. Value name `Version`. Detection method **Version comparison**, operator **Greater than or equal to**, value `0.6.1` |
| Associated with a 32-bit app on 64-bit clients | No |

The licence file travels inside the package, which Intune stores encrypted
and delivers to the device's `IMECache` folder; the install step reads it as
`SYSTEM` and the agent keeps its own copy as `license.jwt`. Build one
package per customer licence, and keep the source folder out of version
control.

## Upgrade and repair

A newer MSI installed over the older one is a major upgrade: the service
stops, the binary is replaced, the service starts, and everything under
`%ProgramData%\SenHub\` (configuration, licence, secrets) is kept. The
configuration step is idempotent, so the properties of an upgrade command
are ignored once an `agent.yaml` exists. A beta and its final release share
the same `X.Y.Z`, and the final replaces the beta; a lower version is
refused.

- **Intune and SCCM**: publish the new MSI as a new version of the same
  app, with the detection rule set to the new version. Machines on the old
  version no longer match and receive it.
- **GPO**: add the new MSI as a package that **upgrades** the existing one
  (**Upgrades** tab), then remove the old package from the GPO.
- **Repair**: `msiexec /f senhub-agent-0.6.1-amd64.msi /qn` restores the
  binary and the service registration and leaves the configuration alone.
- **A machine that runs an agent installed another way** (ZIP, `install`
  command) stops the installer with a message. `ADOPT=1` takes it over and
  keeps its configuration.

## Removal

```bat
msiexec /x "{ProductCode}" /qn /norestart
```

A removal deletes `%ProgramData%\SenHub\` in full: configuration, sealed
secrets, licence and logs. A host that will be reinstalled and must keep its
licence is upgraded in place, not removed and installed again. In Intune,
a removal is an uninstall assignment on the app; in SCCM, an uninstall
deployment; in a GPO, removing the package with **Immediately uninstall the
software from users and computers**.

## Verify

Run on one machine after the tool reports success.

```powershell
Get-Service senhub-agent | Format-List Status, StartType
(Get-ItemProperty "HKLM:\SOFTWARE\Sensor Factory\SenHub Agent").Version
& "C:\Program Files\SenHub Agent\senhub-agent.exe" status
& "C:\Program Files\SenHub Agent\senhub-agent.exe" config check
& "C:\Program Files\SenHub Agent\senhub-agent.exe" license show
# only present when OTLP_ENDPOINT was given
Get-Content "C:\ProgramData\SenHub\strategies.d\10-otlp.yaml"
Invoke-RestMethod http://localhost:8080/health
```

Expected: the service `Running` and `Automatic`, the version you pinned,
`status` healthy, `config check` exit `0`, the licence tier you provisioned,
the OTLP endpoint of the property (the file `strategies.d\10-otlp.yaml` exists only when `OTLP_ENDPOINT` was given), and `/health` answering.
