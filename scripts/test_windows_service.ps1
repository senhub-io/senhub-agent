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

# Service name
$serviceName = "senhub-agent"

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
Start-Sleep -Seconds 2

# Check service status
$service = Get-Service -Name $serviceName
Write-Host "Service status: $($service.Status)"

# Step 3: Stop the service
Write-Host "Stopping the service..."
& $serviceExecutable stop
Start-Sleep -Seconds 2

# Step 4: Uninstall the service
Write-Host "Uninstalling the service..."
& $serviceExecutable uninstall
if (Check-ServiceInstalled -ServiceName $serviceName) {
    Write-Error "Service uninstallation failed!"
    exit 1
}
Write-Host "Service uninstalled successfully."
