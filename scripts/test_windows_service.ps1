# Check if running with administrator privileges
function Test-Administrator {
    $user = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($user)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

# If not running as admin, restart script with admin privileges
if (-not (Test-Administrator)) {
    Write-Host "This script requires administrator privileges. Restarting with elevated permissions..."
    Start-Process powershell -Verb RunAs -ArgumentList ("-NoProfile -ExecutionPolicy Bypass -File `"$PSCommandPath`"")
    exit
}

# Define the service executable path
$serviceExecutable = ".\dist\senhub-agent_windows_amd64.exe"

# Function to check if the service is installed
function Check-ServiceInstalled {
    param (
        [string]$ServiceName
    )
    $service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
    return $null -ne $service
}

# Function to wait for a file to be created
function Wait-ForFile {
    param (
        [string]$FilePath,
        [int]$TimeoutInSeconds = 30
    )
    $startTime = Get-Date
    Write-Host "Waiting for file: $FilePath"
    while (-not (Test-Path $FilePath)) {
        Write-Host "." -NoNewline
        Start-Sleep -Seconds 1
        if ((Get-Date) -gt $startTime.AddSeconds($TimeoutInSeconds)) {
            Write-Host ""
            Write-Error "Timed out waiting for file: $FilePath"
            # Check parent directory permissions
            $logDir = Split-Path $FilePath -Parent
            if (Test-Path $logDir) {
                Write-Host "Log directory exists, checking permissions:"
                $acl = Get-Acl $logDir
                $acl.Access | Format-Table IdentityReference, FileSystemRights -AutoSize
            } else {
                Write-Host "Log directory does not exist: $logDir"
            }
            return $false
        }
    }
    Write-Host ""
    Write-Host "File found: $FilePath"
    return $true
}

# Service name
$serviceName = "senhub-agent"

# Path to the expected log file
$logDir = "C:\ProgramData\SenHub\logs"
Write-Host "Creating log directory: $logDir"
New-Item -ItemType Directory -Force -Path $logDir | Out-Null

# Ensure directory has proper permissions for the service account
$acl = Get-Acl $logDir
$systemSid = New-Object System.Security.Principal.SecurityIdentifier([System.Security.Principal.WellKnownSidType]::LocalSystemSid, $null)
$systemRule = New-Object System.Security.AccessControl.FileSystemAccessRule($systemSid, "FullControl", "ContainerInherit,ObjectInherit", "None", "Allow")
$acl.AddAccessRule($systemRule)
Set-Acl $logDir $acl

$logFilePath = Join-Path $logDir "senhubagent.log"

Write-Host "Running with administrator privileges..."

# Step 1: Install the service
Write-Host "Installing the service..."
& $serviceExecutable install --authentication-key "blah"
if (-not (Check-ServiceInstalled -ServiceName $serviceName)) {
    Write-Error "Service installation failed!"
    exit 1
}
Write-Host "Service installed successfully."

# Step 2: Start the service
Write-Host "Starting the service..."
& $serviceExecutable start
Start-Sleep -Seconds 5

# Check service status after startup
$service = Get-Service -Name $serviceName
Write-Host "Service status after start: $($service.Status)"

# Step 3: Wait for the log file to be created
Write-Host "Waiting for the log file to be created..."
if (-not (Wait-ForFile -FilePath $logFilePath -TimeoutInSeconds 45)) {
    Write-Error "Log file was not created within the timeout period."

    # Display service-related system events
    Write-Host "Checking system event log for service-related events..."
    Get-EventLog -LogName System -Source "Service Control Manager" -Newest 10 |
        Where-Object { $_.Message -like "*$serviceName*" } |
        ForEach-Object { Write-Host $_.TimeGenerated $_.Message }

    # Cleanup before exit
    Write-Host "Cleaning up..."
    & $serviceExecutable stop
    Start-Sleep -Seconds 2
    & $serviceExecutable uninstall
    exit 1
}
Write-Host "Log file detected."

# Step 4: Run status check
Write-Host "Service status..."
& $serviceExecutable status
Start-Sleep -Seconds 2

# Step 5: Stop the service
Write-Host "Stopping the service..."
& $serviceExecutable stop
Start-Sleep -Seconds 2

# Step 6: Uninstall the service
Write-Host "Uninstalling the service..."
& $serviceExecutable uninstall
if (Check-ServiceInstalled -ServiceName $serviceName) {
    Write-Error "Service uninstallation failed!"
    exit 1
}
Write-Host "Service uninstalled successfully."
