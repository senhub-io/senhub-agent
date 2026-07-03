# Smoke test for the Windows service install/start/stop/uninstall
# lifecycle. Invoked from .github/workflows/go-test-windows.yml after
# `make build` has populated dist/.
#
# 0.2.0+ build layout: dist/windows-amd64/senhub-agent.exe. The
# pre-0.2.0 path (dist/senhub-agent_windows_amd64.exe) is kept as a
# fallback so a contributor running this script against an older
# checkout still finds the binary.

$ErrorActionPreference = "Stop"

$candidates = @(
    (Join-Path $PSScriptRoot "..\dist\windows-amd64\senhub-agent.exe"),
    (Join-Path $PSScriptRoot "..\dist\senhub-agent_windows_amd64.exe"),
    ".\dist\windows-amd64\senhub-agent.exe",
    ".\dist\senhub-agent_windows_amd64.exe"
)
if ($env:GITHUB_WORKSPACE) {
    $candidates += (Join-Path $env:GITHUB_WORKSPACE "dist\windows-amd64\senhub-agent.exe")
    $candidates += (Join-Path $env:GITHUB_WORKSPACE "dist\senhub-agent_windows_amd64.exe")
}

$serviceExecutable = $null
foreach ($candidate in $candidates) {
    if (Test-Path -LiteralPath $candidate) {
        $serviceExecutable = (Resolve-Path -LiteralPath $candidate).ProviderPath
        Write-Host "Using executable: $serviceExecutable"
        break
    }
}

if (-not $serviceExecutable) {
    Write-Host "Executable not found in any expected location. Listing dist tree:"
    $distPath = Join-Path $PSScriptRoot "..\dist"
    if (Test-Path $distPath) {
        Get-ChildItem -Path $distPath -Force -Recurse | Select-Object FullName, Length | Format-Table -AutoSize
    } else {
        Write-Host "dist directory not found at: $distPath"
        Write-Host "Current directory: $PWD"
        Get-ChildItem -Force
    }
    Write-Error "Unable to find the senhub-agent executable"
    exit 1
}

function Check-ServiceInstalled {
    param ([string]$ServiceName)
    $service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
    return $null -ne $service
}

$serviceName = "senhub-agent"

# Step 1: Install. Pre-0.2.0 this required `--authentication-key` or
# `--offline`; in 0.2.0+ the install path generates a UUID agent key
# locally and the install is one bare invocation.
Write-Host "Installing the service..."
& $serviceExecutable install
if (-not (Check-ServiceInstalled -ServiceName $serviceName)) {
    Write-Error "Service installation failed!"
    exit 1
}
Write-Host "Service installed successfully."

# Step 2: Start
Write-Host "Starting the service..."
& $serviceExecutable start
Start-Sleep -Seconds 2
$service = Get-Service -Name $serviceName
Write-Host "Service status: $($service.Status)"

# Step 3: Stop
Write-Host "Stopping the service..."
& $serviceExecutable stop
Start-Sleep -Seconds 2

# Step 4: Uninstall
# --yes bypasses the interactive confirmation so this unattended smoke
# test does not hang / abort on the [y/N] prompt.
Write-Host "Uninstalling the service..."
& $serviceExecutable uninstall --yes
if (Check-ServiceInstalled -ServiceName $serviceName) {
    Write-Error "Service uninstallation failed!"
    exit 1
}
Write-Host "Service uninstalled successfully."
