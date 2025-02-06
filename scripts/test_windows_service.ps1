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
    while (-not (Test-Path $FilePath)) {
        Start-Sleep -Seconds 1
        if ((Get-Date) -gt $startTime.AddSeconds($TimeoutInSeconds)) {
            Write-Error "Timed out waiting for file: $FilePath"
            return $false
        }
    }
    return $true
}

# Service name (ensure this matches your service's name)
$serviceName = "senhub-agent"

# Path to the expected log file
$logFilePath = "C:\ProgramData\SenHub\logs\senhubagent.log"

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
Start-Sleep -Seconds 2 # Give some time for the service to start

# Step 3: Wait for the log file to be created
Write-Host "Waiting for the log file to be created..."
if (-not (Wait-ForFile -FilePath $logFilePath -TimeoutInSeconds 30)) {
    Write-Error "Log file was not created within the timeout period."
    exit 1
}
Write-Host "Log file detected."

# Step 4: Run status
Write-Host "Service status..."
& $serviceExecutable status
Start-Sleep -Seconds 2 # Give some time for the service to start

# Step 5: Stop the service
Write-Host "Stopping the service..."
& $serviceExecutable stop
Start-Sleep -Seconds 2 # Allow some time for the service to stop
Write-Host "Service stopped."

# Step 6: Uninstall the service
Write-Host "Uninstalling the service..."
& $serviceExecutable uninstall
if (Check-ServiceInstalled -ServiceName $serviceName) {
    Write-Error "Service uninstallation failed!"
    exit 1
}
Write-Host "Service uninstalled successfully."
